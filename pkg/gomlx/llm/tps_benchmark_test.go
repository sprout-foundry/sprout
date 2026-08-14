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

// TestTPSBenchmark reports prefill and decode tokens/sec in the same format
// as mlx_lm.generate's own stats line, for direct comparison against a
// reference `mlx_lm.generate` run on the same model and prompt length.
// Decode timing is measured via onToken callback timestamps (first token
// marks prefill's end / decode's start) rather than any internal
// instrumentation, so it needs no production code changes to stay accurate.
// Skips when SPROUT_MTP_PARITY_MODEL isn't set.
func TestTPSBenchmark(t *testing.T) {
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
	// Matches production defaults (see local_provider.go): MTP is disabled
	// pending a real-world corruption bug fix, so the honest sprout-vs-mlx-lm
	// comparison is the plain/prompt-lookup decode path actually shipped.
	if os.Getenv("SPROUT_TPS_MTP") == "1" {
		cfg.MaxMTPDrafts = 3
	}

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

	promptTokens := 20017 // matches this synthetic prompt's tokenized length within a few tokens
	prefillElapsed := firstTokenAt.Sub(start)
	decodeElapsed := lastTokenAt.Sub(firstTokenAt)
	decodeTokens := tokenCount - 1 // first token's cost is counted in prefillElapsed

	fmt.Printf("Prompt: ~%d tokens, %.3f tokens-per-sec\n", promptTokens, float64(promptTokens)/prefillElapsed.Seconds())
	if decodeTokens > 0 && decodeElapsed > 0 {
		fmt.Printf("Generation: %d tokens, %.3f tokens-per-sec\n", decodeTokens, float64(decodeTokens)/decodeElapsed.Seconds())
	}
	fmt.Printf("Total: %d tokens, %.2fs\n", tokenCount, total.Seconds())
}
