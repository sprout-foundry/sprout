//go:build darwin && arm64 && cgo

package gemma4

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestBenchmarkDecode measures tok/s for a longer generation to verify the
// C shim actually improves throughput. Requires a model on disk.
func TestBenchmarkDecode(t *testing.T) {
	modelDir := os.ExpandEnv("$HOME/dev/llm-models/gemma-4-e2b-it-4bit")
	if _, err := os.Stat(modelDir + "/model.safetensors"); err != nil {
		t.Skip("model not found at", modelDir)
	}

	model, err := llm.NewModel(modelDir)
	if err != nil {
		t.Skip("model load failed:", err)
	}
	defer model.Close()

	prompt := model.FormatChat([]llm.ChatMessage{
		{Role: "user", Content: "Write a Python function that checks if a string is a palindrome. Include comments."},
	})

	// Warmup (compile kernels, populate cache)
	_, _ = model.GenerateText(context.Background(), prompt,
		llm.GenerateConfig{MaxTokens: 10, Temperature: 0, RepetitionPenalty: 0},
	)

	// Benchmark: count actual tokens via callback
	cfg := llm.GenerateConfig{
		MaxTokens:         200,
		Temperature:       0,
		RepetitionPenalty: 0,
	}

	var count int
	start := time.Now()
	err = model.Generate(context.Background(), prompt, cfg, func(tokenID int) {
		count++
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal("generate failed:", err)
	}

	toksPerSec := float64(count) / elapsed.Seconds()
	t.Logf("Gemma4 e2b 4bit: %d tokens in %v = %.1f tok/s", count, elapsed, toksPerSec)
}
