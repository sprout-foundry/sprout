//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestMTPParityLiveModel is TestMTPParity's check (MTP spec-decode output
// must be byte-identical to plain greedy decode) run against whatever real
// model SPROUT_MTP_PARITY_MODEL points at, so it can be exercised against
// installed quantized models rather than only the raw-HF fixture
// TestMTPParity requires. Skips when the env var isn't set.
func TestMTPParityLiveModel(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL not set")
	}

	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel(%q): %v", dir, err)
	}
	defer model.Close()

	if !model.MTPAvailable() {
		t.Fatalf("expected MTP head to be available on %s", dir)
	}

	prompts := []string{
		"Hello",
		"The capital of France is",
		"Write a short poem about the ocean.",
		"diff --git a/foo.go b/foo.go\n+func bar() {}\nReturn ONLY the commit title.",
	}
	for _, prompt := range prompts {
		cfg := llm.DefaultGenerateConfig()
		cfg.MaxTokens = 24
		cfg.Temperature = 0
		cfg.RepetitionPenalty = 0
		cfg.MaxMTPDrafts = 3

		withMTP, err := model.GenerateText(context.Background(), prompt, cfg)
		if err != nil {
			t.Fatalf("GenerateText(%q) with MTP: %v", prompt, err)
		}

		cfg.MaxMTPDrafts = 0
		plain, err := model.GenerateText(context.Background(), prompt, cfg)
		if err != nil {
			t.Fatalf("GenerateText(%q) plain: %v", prompt, err)
		}

		if withMTP != plain {
			t.Errorf("MTP divergence for %q:\n  mtp:   %q\n  plain: %q", prompt, withMTP, plain)
		} else {
			t.Logf("parity ok for %q: %q", prompt, plain)
		}
	}
}
