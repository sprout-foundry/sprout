//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestTPSShortContext benchmarks decode at a short (chat-like) context —
// 300-token prompt, 200 generated — where per-token CPU dispatch dominates
// rather than attention over a 20K-key cache. This is the regime compiled
// decode targets first (the compiled closure eliminates the per-token graph
// walk entirely).
func TestTPSShortContext(t *testing.T) {
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
	buf = append(buf, "Write a very long, detailed short story about a robot exploring an alien planet, with lots of description. "...)
	for i := 0; i < 250; i++ {
		buf = append(buf, words[r.Intn(len(words))]...)
		buf = append(buf, ' ')
	}
	prompt := string(buf)

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 200
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0

	var firstTokenAt, lastTokenAt time.Time
	tokenCount := 0
	start := time.Now()
	err = model.Generate(context.Background(), prompt, cfg, func(id int) {
		now := time.Now()
		if tokenCount == 0 {
			firstTokenAt = now
		}
		lastTokenAt = now
		tokenCount++
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	total := time.Since(start)

	promptTokens := 305
	prefillElapsed := firstTokenAt.Sub(start)
	decodeElapsed := lastTokenAt.Sub(firstTokenAt)
	decodeTokens := tokenCount - 1
	fmt.Printf("Prompt: ~%d tokens, %.3f tokens-per-sec\n", promptTokens, float64(promptTokens)/prefillElapsed.Seconds())
	if decodeTokens > 0 && decodeElapsed > 0 {
		fmt.Printf("Generation: %d tokens, %.3f tokens-per-sec\n", decodeTokens, float64(decodeTokens)/decodeElapsed.Seconds())
	}
	fmt.Printf("Total: %d tokens, %.2fs\n", tokenCount, total.Seconds())
}
