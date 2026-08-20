//go:build darwin && arm64 && cgo

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sprout-foundry/sinter/llm"
	_ "github.com/sprout-foundry/sinter/llm/qwen3"
	_ "github.com/sprout-foundry/sinter/llm/qwen35"
)

func main() {
	modelDir := os.Getenv("HOME") + "/.cache/sprout/models/qwen3-0.6b"

	model, err := llm.NewModel(modelDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer model.Close()

	prompts := []string{
		"<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nWhat is 2+2?<|im_end|>\n<|im_start|>assistant\n",
		"<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nWrite a haiku about coding.<|im_end|>\n<|im_start|>assistant\n",
		"<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nSummarize in one sentence: The Go programming language is statically typed, compiled, and known for its simplicity.<|im_end|>\n<|im_start|>assistant\n",
	}

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 300
	cfg.Temperature = 0.0

	for i, prompt := range prompts {
		fmt.Printf("--- Test %d ---\n", i+1)
		start := time.Now()
		text, err := model.GenerateText(context.Background(), prompt, cfg)
		elapsed := time.Since(start)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Output: %s\n", text)
		}
		fmt.Printf("Time: %v (%.1f tok/s)\n\n", elapsed, float64(cfg.MaxTokens)/elapsed.Seconds())
	}
}
