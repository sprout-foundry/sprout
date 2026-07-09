package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/search"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// TestMain makes the agent test binary hermetic and fast.
//
// Three failure modes used to make `go test ./pkg/agent/` unrunnable:
//
//  1. Resource blowup. Every agent created in a test calls
//     RestoreEmbeddingIndex(), which auto-enables the embedding index and
//     spawns a background goroutine that downloads a ~240MB ONNX model and
//     runs inference. Multiplied across the suite's ~1600 tests, those
//     leaked download/inference goroutines pinned the machine (~21 cores,
//     tens of GB RSS) and the suite never finished. We disable the implicit
//     auto-index here via the same env guard production/headless runs use;
//     tests that genuinely need embeddings call EnableEmbeddingIndex()
//     explicitly and already gate on -short / ONNX availability.
//
//  2. Prompt hang. A security-approval gate (e.g. highRiskApprovedForCommand)
//     renders an interactive picker when the shared logger is interactive,
//     then blocks forever on stdin that no test types into. We force the
//     shared logger non-interactive so those gates resolve deterministically
//     (reject) instead of hanging. The per-agent SkipPrompt set by the test
//     factory is the durable guarantee; this is a process-wide backstop for
//     any path that doesn't go through agent config.
//
//  3. Search-index rebuild. The persistence.go init() wires the global
//     search IndexUpdater to the real ~/.sprout/sessions/search-index.json
//     before TestMain can intercept it. Any test calling SaveStateScoped
//     triggers search.MarkSessionDirty, which schedules a debounced
//     rebuildAndSave that walks the entire real sessions corpus (~250 MB),
//     building an HNSW index with 30+ GB peak allocation. We redirect the
//     global updater to a throwaway temp dir so those writes are harmless.
func TestMain(m *testing.M) {
	os.Setenv("SPROUT_DISABLE_EMBEDDING_AUTOINDEX", "1")

	// Share one ONNX model/runtime cache across the whole suite. Each test
	// isolates SPROUT_CONFIG to its own t.TempDir(), and the model dir
	// normally derives from that — so without this every embedding-dependent
	// (non -short) test re-downloaded the ~240MB model into a throwaway dir,
	// turning the full run into a download loop that never finished.
	// SPROUT_MODELS_DIR takes priority over the config-derived path, so a
	// stable shared dir means the model is fetched at most once and reused
	// across tests (and across `go test` invocations).
	if os.Getenv("SPROUT_MODELS_DIR") == "" {
		os.Setenv("SPROUT_MODELS_DIR", filepath.Join(os.TempDir(), "sprout-test-models"))
	}

	tmpDir, err := os.MkdirTemp("", "sprout-agent-test-state-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: create temp state dir: %v\n", err)
		os.Exit(1)
	}
	sessionsDir := filepath.Join(tmpDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: mkdir sessions: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	// Layer-5 detector: snapshot the developer's real state dir before
	// installing the override so we can catch tests that bypass isolation.
	realDir, beforeSnapshot := SnapshotRealStateDir()

	// Re-aim the search index updater at the temp sessions dir so
	// SaveStateScoped → MarkSessionDirty writes go to a throwaway
	// corpus instead of the developer's real ~/.sprout/sessions/.
	oldUpdater := search.ResetGlobalUpdaterForTest()
	search.GlobalUpdater = search.NewIndexUpdater(
		filepath.Join(sessionsDir, "search-index.json"), sessionsDir)

	utils.GetLogger(true) // skipPrompts=true → userInteractionEnabled=false
	code := m.Run()

	search.RestoreGlobalUpdater(oldUpdater)

	leakCode := AssertNoStateLeak(realDir, beforeSnapshot)
	_ = os.RemoveAll(tmpDir)

	if code == 0 {
		code = leakCode
	}
	os.Exit(code)
}
