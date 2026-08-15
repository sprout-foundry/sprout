//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestPipelinedDecodeParityLiveModel checks that PipelinedGreedyArchitecture's
// decode path (opted into via SPROUT_PIPELINE_DECODE=1 — see
// Model.generateLocked's usePipelined branch, off by default) produces
// byte-identical output to the plain per-token path for the same prompt
// under greedy decoding. Pipelining is only supposed to change
// AsyncEval/readback timing, not the computation itself, so any divergence
// here is a bug — and as of this test's introduction, it fails: every
// prompt diverges. Skips when SPROUT_MTP_PARITY_MODEL isn't set (reusing
// that env var — same "point at an installed model" use case).
func TestPipelinedDecodeParityLiveModel(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL not set")
	}

	prompts := []string{
		"Hello",
		"The capital of France is",
		"Write a short poem about the ocean.",
		"diff --git a/foo.go b/foo.go\n+func bar() {}\nReturn ONLY the commit title.",
	}

	runOne := func(t *testing.T, prompt string) string {
		t.Helper()
		model, err := llm.NewModel(dir)
		if err != nil {
			t.Fatalf("NewModel(%q): %v", dir, err)
		}
		defer model.Close()

		cfg := llm.DefaultGenerateConfig()
		cfg.MaxTokens = 24
		cfg.Temperature = 0
		cfg.RepetitionPenalty = 0

		out, err := model.GenerateText(context.Background(), prompt, cfg)
		if err != nil {
			t.Fatalf("GenerateText(%q): %v", prompt, err)
		}
		return out
	}

	for _, prompt := range prompts {
		// Pin compiled decode OFF for both legs: it is default-ON for short
		// greedy prompts now, but this test compares pipelined vs plain
		// EAGER byte-for-byte (their graphs are supposed to be identical —
		// any divergence is a pipelining bug). The compiled path has its own
		// near-parity test with different (determinism) assertions.
		os.Setenv("SPROUT_COMPILED_DECODE", "0")
		plain := runOne(t, prompt)

		t.Setenv("SPROUT_PIPELINE_DECODE", "1")
		pipelined := runOne(t, prompt)
		os.Unsetenv("SPROUT_PIPELINE_DECODE")
		os.Unsetenv("SPROUT_COMPILED_DECODE")

		if pipelined != plain {
			t.Errorf("pipelined decode divergence for %q:\n  pipelined: %q\n  plain:     %q", prompt, pipelined, plain)
		} else {
			t.Logf("parity ok for %q: %q", prompt, plain)
		}
	}
}
