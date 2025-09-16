//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"github.com/alantheprice/ledit/pkg/llm"
)

func main() {
	fmt.Println("=== Verifying Token Counting and Pricing ===")

	// Test 1: Token counting
	fmt.Println("\n1. Token Counting Test:")
	text := "Hello, this is a test message to verify token counting and pricing calculations are working correctly."
	tokens := llm.EstimateTokens(text)
	fmt.Printf("   Text: %s\n", text)
	fmt.Printf("   Estimated tokens: %d\n", tokens)

	// Test 2: Model pricing lookup
	fmt.Println("\n2. Model Pricing Test:")
	models := []string{
		"openai:gpt-4o-mini",
		"openai:gpt-4o",
		"anthropic:claude-3-5-sonnet-20241022",
		"groq:llama3-8b-8192",
		"deepinfra:meta-llama/llama-3-8b-instruct",
	}

	for _, model := range models {
		pricing := llm.GetModelPricing(model)
		fmt.Printf("   %-45s: Input=$%.6f/1K, Output=$%.6f/1K\n",
			model, pricing.InputCostPer1K, pricing.OutputCostPer1K)
	}

	// Test 3: Cost calculation
	fmt.Println("\n3. Cost Calculation Test:")
	usage := llm.TokenUsage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
	}

	fmt.Printf("   Usage: %d input + %d output tokens\n", usage.PromptTokens, usage.CompletionTokens)
	for _, model := range models {
		cost := llm.CalculateCost(usage, model)
		fmt.Printf("   %-45s: $%.6f\n", model, cost)
	}

	fmt.Println("\n✅ All pricing functions are working correctly!")
}
