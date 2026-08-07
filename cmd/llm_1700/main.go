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

func main() {
	model, err := llm.NewModel(os.Getenv("HOME") + "/.cache/sprout/models/qwen3-1.7b")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer model.Close()

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 40
	cfg.Temperature = 0.0
	prompts := []struct {
		name string
		p    string
	}{
		{"short", "<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nSay the word: apple.<|im_end|>\n<|im_start|>assistant\n"},
		{"long", "<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nWrite a detailed paragraph about the history of the Roman Empire, covering its founding, expansion, peak, and fall.<|im_end|>\n<|im_start|>assistant\n"},
	}
	for _, tc := range prompts {
		_, _ = model.GenerateText(context.Background(), tc.p, cfg) // warmup
		start := time.Now()
		text, err := model.GenerateText(context.Background(), tc.p, cfg)
		elapsed := time.Since(start)
		if err != nil {
			fmt.Printf("%s ERROR: %v\n", tc.name, err)
			continue
		}
		fmt.Printf("%-6s %v (%.1f tok/s) out=%.40q\n", tc.name, elapsed, float64(cfg.MaxTokens)/elapsed.Seconds(), text)
	}
}
