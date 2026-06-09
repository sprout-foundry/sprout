package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

// TestMain makes the agent test binary hermetic and fast.
//
// Two failure modes used to make `go test ./pkg/agent/` unrunnable:
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

	utils.GetLogger(true) // skipPrompts=true → userInteractionEnabled=false
	os.Exit(m.Run())
}
