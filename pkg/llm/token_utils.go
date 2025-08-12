package llm

import (
	"strings"
)

// --- Token Counting ---

// EstimateTokens provides a more accurate token estimation based on OpenAI's tiktoken approach
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}

	// Count words and characters for better estimation
	words := strings.Fields(text)
	charCount := len(text)

	// Count special tokens (newlines, punctuation, etc.)
	specialTokens := 0
	for _, char := range text {
		if char == '\n' || char == '\r' || char == '\t' {
			specialTokens++
		}
	}

	// More sophisticated estimation based on OpenAI's patterns:
	// - English text: ~0.75 tokens per word
	// - Code: ~1.2 tokens per word (more tokens due to syntax)
	// - Special characters: ~0.5 tokens each

	// Detect if this looks like code
	isCode := strings.Contains(text, "func ") || strings.Contains(text, "import ") ||
		strings.Contains(text, "package ") || strings.Contains(text, "if ") ||
		strings.Contains(text, "for ") || strings.Contains(text, "return ") ||
		strings.Contains(text, "var ") || strings.Contains(text, "const ") ||
		strings.Contains(text, "struct ") || strings.Contains(text, "interface ")

	var tokensPerWord float64
	if isCode {
		tokensPerWord = 1.2
	} else {
		tokensPerWord = 0.75
	}

	// Calculate estimated tokens
	wordTokens := float64(len(words)) * tokensPerWord
	charTokens := float64(charCount) * 0.25 // Rough character-to-token ratio
	specialTokenCost := float64(specialTokens) * 0.5

	totalTokens := int(wordTokens + charTokens + specialTokenCost)

	// Ensure minimum token count
	if totalTokens < 1 {
		totalTokens = 1
	}

	return totalTokens
}

// GetMessageTokens estimates tokens for a complete message including role
func GetMessageTokens(role, content string) int {
	// Role typically adds 3-4 tokens
	roleTokens := 4

	// Content tokens
	contentTokens := EstimateTokens(content)

	// Formatting overhead (typically 3-4 tokens)
	formattingTokens := 4

	return roleTokens + contentTokens + formattingTokens
}

// GetConversationTokens estimates total tokens for a conversation
func GetConversationTokens(messages []struct{ Role, Content string }) int {
	total := 0
	for _, msg := range messages {
		total += GetMessageTokens(msg.Role, msg.Content)
	}

	// Add conversation overhead (typically 3-4 tokens per message)
	overhead := len(messages) * 4

	return total + overhead
}

// --- Model Pricing ---

// CalculateCost calculates the actual cost for a given token usage and model using the pricing table
func CalculateCost(usage TokenUsage, model string) float64 {
	pricing := GetModelPricing(model)

	inputCost := float64(usage.PromptTokens) / 1000.0 * pricing.InputCostPer1K
	outputCost := float64(usage.CompletionTokens) / 1000.0 * pricing.OutputCostPer1K

	return inputCost + outputCost
}
