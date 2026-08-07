//go:build darwin && arm64 && cgo && mlx

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen3"
)

func bench(model *llm.Model, prompt string, maxTokens int, temp float32, repPen float32, label string) {
	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = maxTokens
	cfg.Temperature = temp
	cfg.RepetitionPenalty = repPen

	// warmup
	_, _ = model.GenerateText(context.Background(), prompt, cfg)

	start := time.Now()
	text, err := model.GenerateText(context.Background(), prompt, cfg)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
		return
	}
	fmt.Printf("  %-22s %v  (%.1f tok/s)  out=%.40q\n", label, elapsed, float64(maxTokens)/elapsed.Seconds(), text)
}

func main() {
	modelDir := os.Getenv("HOME") + "/.cache/sprout/models/qwen3-0.6b"

	model, err := llm.NewModel(modelDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer model.Close()

	shortPrompt := "<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nSay the word: apple.<|im_end|>\n<|im_start|>assistant\n"
	longPrompt := "<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nWrite a detailed paragraph about the history of the Roman Empire, covering its founding, expansion, peak, and fall.<|im_end|>\n<|im_start|>assistant\n"

	fmt.Println("GPU argmax greedy (no rep penalty):")
	bench(model, shortPrompt, 40, 0.0, 0.0, "short prompt")
	bench(model, longPrompt, 40, 0.0, 0.0, "long prompt")

	fmt.Println("\nCPU sampling greedy (rep penalty 1.1):")
	bench(model, shortPrompt, 40, 0.0, 1.1, "short prompt")
	bench(model, longPrompt, 40, 0.0, 1.1, "long prompt")
}
