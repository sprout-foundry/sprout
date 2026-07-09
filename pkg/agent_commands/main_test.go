package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/search"
)

// TestMain isolates session-state persistence and the search index updater
// for the agent_commands package's tests. See the matching block in
// cmd/main_test.go for the rationale (leaky autoSaveState writes a session
// JSON to the developer's real ~/.sprout/sessions/ on every Agent constructed
// in a test). Without this hook, tests like TestClearCommand_RotatesSessionID
// that call RotateSession → SaveStateScoped → search.MarkSessionDirty would
// trigger a debounced BuildIndex that walks the entire real sessions corpus
// (~250 MB), building an HNSW index with 30+ GB peak allocation.
func TestMain(m *testing.M) {
	// Disable implicit embedding auto-index for the whole suite. Every agent
	// built by a test calls RestoreEmbeddingIndex(), which otherwise
	// auto-enables the index, lazily downloads a ~240MB ONNX model, and
	// spawns a background build/inference goroutine. Multiplied across tests
	// those goroutines can balloon a single test process to 25–30GB RSS.
	os.Setenv("SPROUT_DISABLE_EMBEDDING_AUTOINDEX", "1")
	if os.Getenv("SPROUT_MODELS_DIR") == "" {
		os.Setenv("SPROUT_MODELS_DIR", filepath.Join(os.TempDir(), "sprout-test-models"))
	}

	tmpDir, err := os.MkdirTemp("", "sprout-agent-commands-test-state-*")
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

	realDir, beforeSnapshot := agent.SnapshotRealStateDir()

	restore := agent.SetTestStateDirHook(sessionsDir)

	// Redirect the search index updater into the temp dir so
	// MarkSessionDirty writes go to a temp dir instead of the
	// developer's real ~/.sprout/sessions/.
	oldUpdater := search.ResetGlobalUpdaterForTest()
	search.GlobalUpdater = search.NewIndexUpdater(
		filepath.Join(sessionsDir, "search-index.json"), sessionsDir)

	code := m.Run()

	search.RestoreGlobalUpdater(oldUpdater)
	restore()

	leakCode := agent.AssertNoStateLeak(realDir, beforeSnapshot)
	_ = os.RemoveAll(tmpDir)

	if code == 0 {
		code = leakCode
	}
	os.Exit(code)
}
