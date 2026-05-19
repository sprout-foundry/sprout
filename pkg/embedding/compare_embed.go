//go:build ignore

package main

import (
	"fmt"
	"log"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

func main() {
	p, err := embedding.NewStaticProvider()
	if err != nil {
		log.Fatal(err)
	}
	defer p.Close()

	texts := []string{
		"compute cosine similarity between two vectors",
		"func cosineSim(a, b []float32) float32",
	}

	for _, text := range texts {
		tokens, ids := p.DebugTokenize(text)
		fmt.Printf("Go tokens: %v\n", tokens)
		fmt.Printf("Go IDs:    %v\n\n", ids)
	}
}
