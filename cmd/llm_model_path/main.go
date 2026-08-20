//go:build darwin && arm64 && cgo && mlx

package main

import (
	"context"
	"log"
	"os"

	"github.com/sprout-foundry/sinter/llm"
	_ "github.com/sprout-foundry/sinter/llm/gemma4"
	_ "github.com/sprout-foundry/sinter/llm/qwen3"
	_ "github.com/sprout-foundry/sinter/llm/qwen35"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: %s <model-dir>", os.Args[0])
	}
	m, err := llm.NewModel(os.Args[1])
	if err != nil {
		log.Fatalf("new model: %v", err)
	}
	defer m.Close()

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 40
	cfg.Temperature = 0
	out, err := m.GenerateText(context.Background(), "The capital of France is", cfg)
	if err != nil {
		log.Fatalf("generate: %v", err)
	}
	log.Printf("OUTPUT: %q", out)
}
