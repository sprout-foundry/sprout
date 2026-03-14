package api

import (
	"strings"
	"sync"
)

// Token estimation constants
const (
	// DefaultBufferTokens is the safety buffer for estimation errors
	DefaultBufferTokens = 1000
	// MinOutputTokens is the minimum output tokens to reserve
	MinOutputTokens = 512
	// ToolTokenEstimate is the approximate token count per tool definition
	ToolTokenEstimate = 200
	// SystemInstructionBuffer accounts for system prompt overhead
	SystemInstructionBuffer = 500
	// MessageOverheadTokens accounts for role/message wrapper overhead
	MessageOverheadTokens = 4
	// ImageMessageOverheadTokens conservatively accounts for multimodal image parts
	ImageMessageOverheadTokens = 256
)

var (
	tokenCache   = make(map[string]int)
	tokenCacheMu sync.RWMutex
)

// EstimateTokens provides a token estimation based on OpenAI's tiktoken approach.
// This is the centralized implementation that all providers should use for consistency.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}

	// Fast path: cached
	tokenCacheMu.RLock()
	cached, ok := tokenCache[text]
	tokenCacheMu.RUnlock()
	if ok {
		return cached
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
	isCode := detectCode(text)

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

	// Use the higher of word-based or character-based estimation
	var baseTokens float64
	if wordTokens > charTokens {
		baseTokens = wordTokens
	} else {
		baseTokens = charTokens
	}

	totalTokens := int(baseTokens + specialTokenCost)

	// Ensure minimum token count
	if totalTokens < 1 {
		totalTokens = 1
	}

	// Store in cache (limit cache size to prevent memory issues)
	tokenCacheMu.Lock()
	if len(tokenCache) < 10000 {
		tokenCache[text] = totalTokens
	}
	tokenCacheMu.Unlock()

	return totalTokens
}

// detectCode determines if text appears to be code
func detectCode(text string) bool {
	return strings.Contains(text, "func ") ||
		strings.Contains(text, "import ") ||
		strings.Contains(text, "package ") ||
		strings.Contains(text, "if ") ||
		strings.Contains(text, "for ") ||
		strings.Contains(text, "return ") ||
		strings.Contains(text, "var ") ||
		strings.Contains(text, "const ") ||
		strings.Contains(text, "struct ") ||
		strings.Contains(text, "interface ") ||
		strings.Contains(text, "func(") ||
		strings.Contains(text, "{\n") ||
		strings.Contains(text, "}\n") ||
		strings.Contains(text, "();") ||
		strings.Contains(text, "= {") ||
		strings.Contains(text, "=> {")
}

// EstimateInputTokens estimates total input tokens for messages and tools.
// This includes a buffer for system instructions and message formatting overhead.
func EstimateInputTokens(messages []Message, tools []Tool) int {
	inputTokens := 0
	for _, msg := range messages {
		inputTokens += EstimateTokens(msg.Content)
		inputTokens += EstimateTokens(msg.ReasoningContent)
		for _, img := range msg.Images {
			inputTokens += estimateImageTokens(img)
		}
		// Account for message role and formatting overhead
		inputTokens += MessageOverheadTokens
	}
	// Add tool tokens
	inputTokens += len(tools) * ToolTokenEstimate
	// Add buffer for system instructions and formatting
	inputTokens += SystemInstructionBuffer
	return inputTokens
}

// CalculateOutputBudget calculates the safe output token budget given context constraints.
// It returns the maximum tokens that can be requested for completion.
// If the input exceeds the context limit, returns 0 and an error message.
func CalculateOutputBudget(contextLimit int, inputTokens int) (int, bool) {
	if contextLimit <= 0 {
		contextLimit = 32000 // Default fallback
	}

	// Check if input already exceeds context
	if inputTokens >= contextLimit {
		return 0, false
	}

	// Calculate remaining space
	remaining := contextLimit - inputTokens

	// Use a percentage-based buffer with minimum
	// 5% of remaining or at least DefaultBufferTokens
	buffer := (remaining * 5) / 100
	if buffer < DefaultBufferTokens {
		buffer = DefaultBufferTokens
	}

	// Ensure we don't subtract more than available
	if buffer >= remaining {
		if remaining < MinOutputTokens {
			return remaining, true
		}
		return MinOutputTokens, true // Minimum viable output
	}

	maxOutput := remaining - buffer

	// Ensure minimum output tokens
	if maxOutput < MinOutputTokens && remaining >= MinOutputTokens {
		maxOutput = MinOutputTokens
	}
	if maxOutput > remaining {
		maxOutput = remaining
	}

	return maxOutput, true
}

func estimateImageTokens(img ImageData) int {
	tokens := ImageMessageOverheadTokens

	if img.URL != "" {
		tokens += EstimateTokens(img.URL)
	}

	if img.Type != "" {
		tokens += EstimateTokens(img.Type)
	}

	if img.Base64 != "" {
		tokens += EstimateTokens(img.Base64)
	}

	return tokens
}
