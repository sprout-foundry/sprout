package agent

import (
	"strings"
	"sync"
)

// Token estimation utilities copied from llm package to remove dependency

var (
	tokenCache   = make(map[string]int)
	tokenCacheMu sync.RWMutex
)

// EstimateTokens provides a token estimation based on OpenAI's tiktoken approach
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

	// Store in cache
	tokenCacheMu.Lock()
	tokenCache[text] = totalTokens
	tokenCacheMu.Unlock()
	return totalTokens
}
