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
	// ToolCallOverheadTokens accounts for assistant tool_call wrapper overhead
	ToolCallOverheadTokens = 12
	// ToolCallIDOverheadTokens accounts for tool response tool_call_id overhead
	ToolCallIDOverheadTokens = 8
	// ImageMessageOverheadTokens conservatively accounts for multimodal image parts
	ImageMessageOverheadTokens = 256
	// EstimationErrorPercent is how much EstimateTokens can underestimate the
	// true token count on tool-heavy prompts (observed 25-34% in practice).
	// CalculateOutputBudget inflates the input estimate by this percent to
	// get a worst-case figure to budget output against.
	EstimationErrorPercent = 30
	// BaseCushionPercent is a small fixed cushion (percent of context limit)
	// for output-side rounding/formatting slop, on top of the estimation
	// error margin above.
	BaseCushionPercent = 5
	// BaseCushionFloor ensures small contexts still get a meaningful cushion.
	BaseCushionFloor = 2000
)

var (
	tokenCache = make(map[string]int)
	cacheMu    sync.RWMutex
)

// EstimateTokens provides a token estimation based on OpenAI's tiktoken approach.
// This is the centralized implementation that all providers should use for consistency.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}

	// Fast path: cached
	cacheMu.RLock()
	cached, ok := tokenCache[text]
	cacheMu.RUnlock()
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
	totalTokens = max(totalTokens, 1)

	// Store in cache (limit cache size to prevent memory issues)
	cacheMu.Lock()
	if len(tokenCache) < 10000 {
		tokenCache[text] = totalTokens
	}
	cacheMu.Unlock()

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

// EstimateMessagesTokens estimates tokens for a slice of messages only —
// no tool catalog or system-instruction buffer. Factored out of
// EstimateInputTokens so callers that already know the tool/system-prompt
// contribution from a real measurement (see sproutProvider's token anchor
// in pkg/agent/seed_provider_token_anchor.go) can estimate just a delta of
// newly appended messages without double-counting the fixed overhead.
func EstimateMessagesTokens(messages []Message) int {
	tokens := 0
	for _, msg := range messages {
		tokens += EstimateTokens(msg.Content)
		tokens += EstimateTokens(msg.ReasoningContent)
		for _, img := range msg.Images {
			tokens += estimateImageTokens(img)
		}
		for _, toolCall := range msg.ToolCalls {
			tokens += EstimateTokens(toolCall.ID)
			tokens += EstimateTokens(toolCall.Type)
			tokens += EstimateTokens(toolCall.Function.Name)
			tokens += EstimateTokens(toolCall.Function.Arguments)
			tokens += ToolCallOverheadTokens
		}
		if msg.ToolCallID != "" {
			tokens += EstimateTokens(msg.ToolCallID)
			tokens += ToolCallIDOverheadTokens
		}
		// Account for message role and formatting overhead
		tokens += MessageOverheadTokens
	}
	return tokens
}

// EstimateInputTokens estimates total input tokens for messages and tools.
// This includes a buffer for system instructions and message formatting overhead.
func EstimateInputTokens(messages []Message, tools []Tool) int {
	inputTokens := EstimateMessagesTokens(messages)
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

	// EstimateTokens is a heuristic (not a real BPE tokenizer): on tool-heavy
	// prompts it has been observed to underestimate the true token count by
	// 25-34%. Rather than reserving a buffer *on top of* the already-remaining
	// space (which double-counts the same risk and, being additive, collapses
	// to nothing once input crosses ~64% of a 200K window — see the
	// "no premature collapse" regression test), inflate the input estimate
	// itself to a worst-case figure and budget output against that.
	worstCaseInput := inputTokens + (inputTokens*EstimationErrorPercent)/100

	// Small fixed cushion for output-side rounding/formatting slop, separate
	// from the estimation-error margin above. Scales gently with window size
	// but stays modest — the worst-case input inflation already carries most
	// of the safety margin.
	cushion := max((contextLimit*BaseCushionPercent)/100, BaseCushionFloor)

	maxOutput := contextLimit - worstCaseInput - cushion

	// Hard cap: max_tokens must never cause input + output to exceed
	// the context limit. This is the last line of defense against
	// estimation errors that slip past the margins above.
	maxOutput = min(maxOutput, remaining)

	// Below the minimum viable output, fall back to a small fixed floor —
	// but only once the real (non-worst-case) remaining space also can't
	// comfortably cover it. This should only bite in the final stretch
	// before the actual ceiling, not at moderate context usage.
	if maxOutput < MinOutputTokens {
		if remaining < MinOutputTokens {
			return remaining, true
		}
		return MinOutputTokens, true // Minimum viable output
	}

	return maxOutput, true
}

// CalculateOutputBudgetAnchored computes the output budget when part of the
// input estimate came from a real measurement (Usage.PromptTokens) and only
// the heuristic portion is subject to estimation error. This prevents
// double-counting the estimation margin on the anchored portion.
//
// anchoredInput is the portion measured from a real API response (no error).
// heuristicInput is the portion estimated by the heuristic (subject to
// EstimationErrorPercent underestimation).
// The total input is anchoredInput + heuristicInput.
func CalculateOutputBudgetAnchored(contextLimit, anchoredInput, heuristicInput int) (int, bool) {
	if contextLimit <= 0 {
		contextLimit = 32000
	}

	totalInput := anchoredInput + heuristicInput
	if totalInput >= contextLimit {
		return 0, false
	}

	remaining := contextLimit - totalInput

	// Only inflate the heuristic portion — the anchored portion is already
	// a real measurement with no estimation error.
	worstCaseHeuristic := heuristicInput + (heuristicInput*EstimationErrorPercent)/100
	worstCaseInput := anchoredInput + worstCaseHeuristic

	cushion := max((contextLimit*BaseCushionPercent)/100, BaseCushionFloor)

	maxOutput := contextLimit - worstCaseInput - cushion
	maxOutput = min(maxOutput, remaining)

	if maxOutput < MinOutputTokens {
		if remaining < MinOutputTokens {
			return remaining, true
		}
		return MinOutputTokens, true
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
