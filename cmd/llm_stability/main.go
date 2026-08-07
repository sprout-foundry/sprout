//go:build darwin && arm64 && cgo && mlx

package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen3"
)

func memStats() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return fmt.Sprintf("heap=%.1fMB sys=%.1fMB", float64(m.HeapAlloc)/1e6, float64(m.Sys)/1e6)
}

func main() {
	modelDir := os.Getenv("HOME") + "/.cache/sprout/models/qwen3-0.6b"
	model, err := llm.NewModel(modelDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer model.Close()

	// Bound MLX's pooled-buffer cache: default pools ALL freed buffers, which
	// makes long generations appear to leak RSS (Go heap stays flat).
	const cacheLimit = uint64(256) << 20 // 256MB
	if err := mlx.SetCacheLimit(cacheLimit); err != nil {
		fmt.Fprintf(os.Stderr, "SetCacheLimit: %v\n", err)
		os.Exit(1)
	}
	snap, _ := mlx.Snapshot()
	fmt.Printf("mlx: active=%.0fMB cache=%.0fMB peak=%.0fMB\n",
		float64(snap.Active)/1e6, float64(snap.Cache)/1e6, float64(snap.Peak)/1e6)

	prompt := "<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nWrite a long, detailed story about a robot learning to paint. Make it at least 300 words.<|im_end|>\n<|im_start|>assistant\n"

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 300
	cfg.Temperature = 0.6
	cfg.TopP = 0.95
	cfg.TopK = 20
	cfg.RepetitionPenalty = 1.1

	fmt.Printf("start: %s\n", memStats())
	start := time.Now()
	text, err := model.GenerateText(context.Background(), prompt, cfg)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("done: %s\n", memStats())
	fmt.Printf("elapsed: %v (%.1f tok/s)\n", elapsed, float64(cfg.MaxTokens)/elapsed.Seconds())
	fmt.Printf("output length: %d chars, %d words\n", len(text), len(splitWords(text)))
	fmt.Printf("output preview: %.120s\n", text)

	// Second run to check for leaks/instability
	start = time.Now()
	text2, err := model.GenerateText(context.Background(), prompt, cfg)
	elapsed2 := time.Since(start)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error (2nd run): %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\n2nd run: %s\n", memStats())
	fmt.Printf("2nd elapsed: %v (%.1f tok/s)\n", elapsed2, float64(cfg.MaxTokens)/elapsed2.Seconds())
	fmt.Printf("2nd output length: %d chars\n", len(text2))
}

func splitWords(s string) []string {
	var words []string
	start := -1
	for i, c := range s {
		if c == ' ' || c == '\n' || c == '\t' {
			if start >= 0 {
				words = append(words, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		words = append(words, s[start:])
	}
	return words
}
