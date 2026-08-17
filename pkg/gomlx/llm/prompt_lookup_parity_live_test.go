//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/gemma4"
)

// TestPromptLookupParityLiveModel asserts prompt-lookup speculative decoding
// produces byte-identical greedy output to the plain per-token path — the
// same contract TestMTPParityLiveModel holds MTP to. The historical bug
// this guards against: on partial/rejected drafts the rollback either left
// a hole in the KV cache (accepted==0 re-run skipped) or desynced pos from
// cache length by one per partial round (pos += accepted instead of
// accepted+1) — novel prose (constant mid-batch rejections) degenerated
// into garbage within seconds, while echo-heavy workloads (full accepts)
// masked it. Skips when SPROUT_MTP_PARITY_MODEL isn't set.
func TestPromptLookupParityLiveModel(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL not set")
	}

	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel(%q): %v", dir, err)
	}
	defer model.Close()

	prompts := []string{
		"Hello",
		"The capital of France is",
		"Write a short poem about the ocean.",
		"diff --git a/foo.go b/foo.go\n+func bar() {}\nReturn ONLY the commit title.",
	}
	for _, prompt := range prompts {
		prompt := prompt
		t.Run(prompt, func(t *testing.T) {
			cfg := llm.DefaultGenerateConfig()
			cfg.MaxTokens = 48
			cfg.Temperature = 0
			cfg.RepetitionPenalty = 0
			cfg.PromptLookupMaxDrafts = 6 // production default

			withLookup, err := model.GenerateText(context.Background(), prompt, cfg)
			if err != nil {
				t.Fatalf("GenerateText(%q) with lookup: %v", prompt, err)
			}

			cfg.PromptLookupMaxDrafts = 0
			plain, err := model.GenerateText(context.Background(), prompt, cfg)
			if err != nil {
				t.Fatalf("GenerateText(%q) plain: %v", prompt, err)
			}

			if strings.TrimSpace(withLookup) != strings.TrimSpace(plain) {
				t.Errorf("prompt-lookup divergence for %q:\n  lookup: %q\n  plain:  %q", prompt, withLookup, plain)
			} else {
				t.Logf("parity ok for %q: %q", prompt, plain)
			}
		})
	}
}
