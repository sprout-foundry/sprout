//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestScratchBf16Sanity prints actual generations after the scaleRMSNorm
// bf16 fix, to eyeball coherence vs the fp32-promoted pipeline.
func TestScratchBf16Sanity(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL not set")
	}
	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()

	for _, prompt := range []string{
		"The capital of France is",
		"Write a haiku about rain.",
		"1 + 1 =",
	} {
		cfg := llm.DefaultGenerateConfig()
		cfg.MaxTokens = 24
		cfg.Temperature = 0
		cfg.RepetitionPenalty = 0
		out, err := model.GenerateText(context.Background(), prompt, cfg)
		if err != nil {
			t.Fatalf("GenerateText(%q): %v", prompt, err)
		}
		fmt.Printf("PROMPT %q -> %q\n", prompt, out)
	}
}
