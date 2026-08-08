//go:build darwin && arm64 && cgo && mlx

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen3"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen35"
)

func bench(model *llm.Model, prompt string, maxTokens int, temp float32, repPen float32, mtpDrafts int, label string) {
	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = maxTokens
	cfg.Temperature = temp
	cfg.RepetitionPenalty = repPen
	cfg.MaxMTPDrafts = mtpDrafts

	// warmup
	_, _ = model.GenerateText(context.Background(), prompt, cfg)

	start := time.Now()
	text, err := model.GenerateText(context.Background(), prompt, cfg)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
		return
	}
	tps := float64(maxTokens) / elapsed.Seconds()
	mtpTag := ""
	if mtpDrafts > 0 {
		mtpTag = fmt.Sprintf(" [mtp=%d]", mtpDrafts)
	}
	fmt.Printf("  %-26s %v  (%.1f tok/s)%s  out=%.40q\n", label, elapsed, tps, mtpTag, text)
}

func benchNoWarmup(model *llm.Model, prompt string, maxTokens int, temp float32, repPen float32, mtpDrafts int, label string) {
	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = maxTokens
	cfg.Temperature = temp
	cfg.RepetitionPenalty = repPen
	cfg.MaxMTPDrafts = mtpDrafts

	start := time.Now()
	text, err := model.GenerateText(context.Background(), prompt, cfg)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
		return
	}
	tps := float64(maxTokens) / elapsed.Seconds()
	mtpTag := ""
	if mtpDrafts > 0 {
		mtpTag = fmt.Sprintf(" [mtp=%d]", mtpDrafts)
	}
	fmt.Printf("  %-26s %v  (%.1f tok/s)%s  out=%.40q\n", label, elapsed, tps, mtpTag, text)
}

func main() {
	modelDir := flag.String("model", os.Getenv("HOME")+"/.cache/sprout/models/qwen3-0.6b", "model directory")
	maxTokens := flag.Int("tokens", 64, "max tokens to generate")
	flag.Parse()

	model, err := llm.NewModel(*modelDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer model.Close()

	mtpAvail := model.MTPAvailable()
	fmt.Printf("Model: %s\n", *modelDir)
	fmt.Printf("MTP available: %v\n\n", mtpAvail)

	longPrompt := "<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nWrite a detailed paragraph about the history of the Roman Empire, covering its founding, expansion, peak, and fall.<|im_end|>\n<|im_start|>assistant\n"

	// Baseline: no MTP
	fmt.Println("=== No MTP (baseline) ===")
	bench(model, longPrompt, *maxTokens, 0.0, 0.0, 0, "long prompt")
	bench(model, longPrompt, *maxTokens, 0.0, 0.0, 0, "long prompt")

	// MTP on
	if mtpAvail {
		fmt.Println("\n=== MTP spec-decode (k=4) ===")
		benchNoWarmup(model, longPrompt, *maxTokens, 0.0, 0.0, 4, "long prompt")
	}
}
