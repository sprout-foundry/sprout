//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

func TestDecodeProfile(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL not set")
	}
	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	defer model.Close()

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 150
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0

	err = model.Generate(context.Background(), "Write a very long, detailed short story about a robot exploring an alien planet, with lots of description.", cfg, func(id int) {})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
}
