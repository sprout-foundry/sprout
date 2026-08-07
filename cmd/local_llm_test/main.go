//go:build darwin && arm64 && cgo && mlx

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"

	// Register architectures
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen3"
)

func main() {
	modelDir := os.Getenv("HOME") + "/.cache/sprout/models/qwen3-0.6b"

	fmt.Println("Loading model...")
	start := time.Now()
	model, err := llm.NewModel(modelDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer model.Close()
	fmt.Printf("Loaded in %v\n\n", time.Since(start))

	// Simple test with greedy decoding
	prompt := "<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nWhat is 2+2?<|im_end|>\n<|im_start|>assistant\n"

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 30
	cfg.Temperature = 0.0 // greedy
	cfg.RepetitionPenalty = 1.1

	fmt.Println("=== With repetition penalty ===")
	genStart := time.Now()
	text, err := model.GenerateText(context.Background(), prompt, cfg)
	elapsed := time.Since(genStart)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Output: %s\n", text)
	}
	fmt.Printf("Time: %v (%.1f tok/s)\n\n", elapsed, 30/elapsed.Seconds())

	// Test 2: Same prompt, no repetition penalty
	cfg.RepetitionPenalty = 0
	fmt.Println("=== Without repetition penalty ===")
	genStart = time.Now()
	text, err = model.GenerateText(context.Background(), prompt, cfg)
	elapsed = time.Since(genStart)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Output: %s\n", text)
	}
	fmt.Printf("Time: %v (%.1f tok/s)\n", elapsed, 30/elapsed.Seconds())
}
