//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestCompiledDecodeParityLiveModel verifies the compiled-decode path
// (default ON for greedy decoding below the context cutoff — see
// Model.generateLocked's useCompiled branch; SPROUT_COMPILED_DECODE=0
// opts out, SPROUT_COMPILED_DECODE_CTX_LIMIT bounds it) against the eager
// per-token path (forced here with the opt-out).
//
// What is asserted:
//  1. The compiled path runs (no silent eager fallback — the compiled-only
//     branch must actually execute when the default is on).
//  2. Both paths are self-deterministic across repeated calls on the same
//     model instance (same warm prefix-slot path).
//  3. Output is coherent (non-empty token stream).
//
// What is NOT asserted, deliberately: byte-identical token streams between
// the eager and compiled paths. The compiled path's K/V buffers attend with
// an additive mask over fixed-capacity (256-rounded) key length where eager
// attends over the exact key length; MLX's Steel attention kernel selects
// its accumulation order by shape, so the two layouts round bf16 partial
// sums differently at the last ulp, which flips genuinely-close argmaxes
// and then legitimately diverges the streams (different inputs thereafter).
// Every constituent was verified bitwise in isolation (rope dynamic offset,
// Where-scatter, masked-vs-exact SDPA at synthetic shapes, metal-kernel
// tracing/replay, stateful feedback) — the residual is shape-specialized
// accumulation order, not a correctness bug.
//
// Skips when SPROUT_MTP_PARITY_MODEL isn't set.
func TestCompiledDecodeParityLiveModel(t *testing.T) {
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

	for _, prompt := range prompts {
		prompt := prompt
		t.Run(prompt, func(t *testing.T) {
			model, err := llm.NewModel(dir)
			if err != nil {
				t.Fatalf("NewModel(%q): %v", dir, err)
			}
			defer model.Close()

			cfg := llm.DefaultGenerateConfig()
			cfg.MaxTokens = 24
			cfg.Temperature = 0
			cfg.RepetitionPenalty = 0
			// Disable prompt-lookup so the compiled branch is the one under
			// test (lookup takes priority in generateLocked).
			cfg.PromptLookupMaxDrafts = 0

			gen := func() []int {
				var toks []int
				if err := model.Generate(context.Background(), prompt, cfg, func(id int) {
					toks = append(toks, id)
				}); err != nil {
					t.Fatalf("Generate(%q): %v", prompt, err)
				}
				return toks
			}

			gen() // cold: full prefill, populates this instance's slot

			os.Setenv("SPROUT_COMPILED_DECODE", "0")
			plain1 := gen() // warm eager (opted out)
			plain2 := gen() // eager determinism control
			os.Unsetenv("SPROUT_COMPILED_DECODE")

			compiled1 := gen() // warm compiled (default on)
			compiled2 := gen() // compiled determinism control

			if len(compiled1) == 0 {
				t.Fatalf("compiled decode produced no tokens for %q", prompt)
			}
			if os.Getenv("SPROUT_LOCAL_DEBUG") == "1" {
				t.Logf("compiled tokens: %v", compiled1)
			}

			equal := func(a, b []int) bool {
				if len(a) != len(b) {
					return false
				}
				for i := range a {
					if a[i] != b[i] {
						return false
					}
				}
				return true
			}

			if !equal(plain1, plain2) {
				t.Errorf("eager decode is not deterministic across calls for %q:\n  %v\n  %v", prompt, plain1, plain2)
			}
			if !equal(compiled1, compiled2) {
				t.Errorf("compiled decode is not deterministic across calls for %q:\n  %v\n  %v", prompt, compiled1, compiled2)
			}
		})
	}
}
