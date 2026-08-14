//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// TestScratchLargeContext measures memory/throughput at a genuinely large,
// agentic-scale context (tens of thousands of tokens) rather than a small
// commit-message-sized prompt. Skips when SPROUT_MTP_PARITY_MODEL isn't set.
func TestScratchLargeContext(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL not set")
	}

	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel(%q): %v", dir, err)
	}
	defer model.Close()

	words := []string{"func", "return", "error", "nil", "string", "int", "struct",
		"interface", "package", "import", "context", "time", "sync", "mutex",
		"append", "len", "make", "the", "and", "of", "to", "in", "a", "is"}
	r := rand.New(rand.NewSource(42))
	var buf []byte
	buf = append(buf, "Summarize the following text in one sentence.\n\n"...)
	for i := 0; i < 20000; i++ {
		buf = append(buf, words[r.Intn(len(words))]...)
		buf = append(buf, ' ')
	}
	buf = append(buf, "\n\nReturn ONLY the summary."...)
	prompt := string(buf)

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 40
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0
	if os.Getenv("SPROUT_SCRATCH_NO_MTP") != "1" {
		cfg.MaxMTPDrafts = 3
	}

	before, _ := mlx.Snapshot()
	fmt.Printf("MLX_MEM before: active=%.1fMB cache=%.1fMB peak=%.1fMB\n",
		float64(before.Active)/1048576, float64(before.Cache)/1048576, float64(before.Peak)/1048576)

	out, err := model.GenerateText(context.Background(), prompt, cfg)
	if err != nil {
		t.Fatalf("GenerateText: %v", err)
	}

	after, _ := mlx.Snapshot()
	fmt.Printf("MLX_MEM after: active=%.1fMB cache=%.1fMB peak=%.1fMB\n",
		float64(after.Active)/1048576, float64(after.Cache)/1048576, float64(after.Peak)/1048576)
	fmt.Printf("output: %q\n", out)
}
