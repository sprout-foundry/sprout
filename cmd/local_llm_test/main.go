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

	fmt.Println("Loading model from", modelDir)
	start := time.Now()
	model, err := llm.NewModel(modelDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer model.Close()
	fmt.Printf("Model loaded in %v\n\n", time.Since(start))

	prompts := []string{
		"<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nWhat is 2+2?<|im_end|>\n<|im_start|>assistant\n",
		"<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nWrite a haiku about coding.<|im_end|>\n<|im_start|>assistant\n",
		"<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nSummarize in one sentence: The Go programming language is statically typed, compiled, and known for its simplicity.<|im_end|>\n<|im_start|>assistant\n",
	}

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 80
	cfg.Temperature = 0.0

	for i, prompt := range prompts {
		fmt.Printf("--- Test %d ---\n", i+1)
		userPart := prompt[len("<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\n"):]
		fmt.Printf("Prompt: %s\n", userPart)

		genStart := time.Now()
		text, err := model.GenerateText(context.Background(), prompt, cfg)
		elapsed := time.Since(genStart)

		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Output: %s\n", text)
		}
		fmt.Printf("Time: %v\n\n", elapsed)
	}
}
