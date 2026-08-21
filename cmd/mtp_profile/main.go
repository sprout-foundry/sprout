//go:build darwin && arm64 && cgo && mlx

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sprout-foundry/sinter/llm"
	_ "github.com/sprout-foundry/sinter/llm/qwen35"
)

func main() {
	dir := flag.String("model", os.Getenv("HOME")+"/.cache/sprout/models/qwen3.5-4b-raw", "model")
	flag.Parse()

	model, err := llm.NewModel(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer model.Close()

	prompt := "<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nWrite about Rome.<|im_end|>\n<|im_start|>assistant\n"

	// warmup
	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 4
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0 // MTP requires the greedy GPU-argmax path
	cfg.MaxMTPDrafts = 0
	model.GenerateText(context.Background(), prompt, cfg)

	tokens := 64

	// Baseline
	cfg.MaxMTPDrafts = 0
	cfg.MaxTokens = tokens
	t0 := time.Now()
	model.GenerateText(context.Background(), prompt, cfg)
	baseElapsed := time.Since(t0)
	fmt.Printf("Baseline (no MTP): %v  (%.1f tok/s)\n", baseElapsed, float64(tokens)/baseElapsed.Seconds())

	// MTP k=1,2,4
	for _, k := range []int{1, 2, 4} {
		cfg.MaxMTPDrafts = k
		// warmup for this k
		cfg.MaxTokens = 4
		model.GenerateText(context.Background(), prompt, cfg)
		cfg.MaxTokens = tokens
		t0 = time.Now()
		model.GenerateText(context.Background(), prompt, cfg)
		elapsed := time.Since(t0)
		speedup := baseElapsed.Seconds() / elapsed.Seconds()
		fmt.Printf("MTP k=%d:           %v  (%.1f tok/s)  %.2fx baseline\n", k, elapsed, float64(tokens)/elapsed.Seconds(), speedup)
	}
}
