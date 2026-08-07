//go:build darwin && arm64 && cgo && mlx

package main

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen3"
)

func mem() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return fmt.Sprintf("heap=%.0fMB", float64(m.HeapAlloc)/1e6)
}

func main() {
	modelDir := "/Users/alanp/.cache/sprout/models/qwen3-0.6b"
	model, err := llm.NewModel(modelDir)
	if err != nil {
		panic(err)
	}
	defer model.Close()

	prompt := "<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nSay exactly: hello world and nothing else<|im_end|>\n<|im_start|>assistant\n"
	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 20
	cfg.Temperature = 0.0
	cfg.RepetitionPenalty = 0.0

	runtime.GC()
	debug.FreeOSMemory()
	fmt.Println("before:", mem())
	start := time.Now()
	text, err := model.GenerateText(context.Background(), prompt, cfg)
	fmt.Printf("generation: %v for %d tokens, out=%q\n", time.Since(start), cfg.MaxTokens, text[:min(40, len(text))])
	fmt.Println("after:", mem())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
