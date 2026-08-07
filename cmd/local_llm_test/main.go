//go:build darwin && arm64 && cgo && mlx

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sprout-foundry/sprout/pkg/local_llm"
)

func main() {
	modelPath := os.Getenv("HOME") + "/.cache/sprout/models/qwen3-0.6b/model.safetensors"
	tokPath := os.Getenv("HOME") + "/.cache/sprout/models/qwen3-0.6b/tokenizer.json"

	fmt.Println("Loading model...")
	start := time.Now()
	model, err := local_llm.NewModel(modelPath, tokPath)
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

	cfg := local_llm.DefaultGenerateConfig()
	cfg.MaxTokens = 80
	cfg.Temperature = 0.0

	for i, prompt := range prompts {
		fmt.Printf("--- Test %d ---\n", i+1)
		fmt.Printf("Prompt: %s\n", prompt[len("<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\n"):])

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
