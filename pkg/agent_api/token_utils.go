package api

import (
	"strings"
	"sync"
)

// Token estimation constants
const (
	// DefaultBufferTokens is the safety buffer for estimation errors
	DefaultBufferTokens = 1000
	// MinOutputTokens is the emergency output floor when the window is
	// nearly full by estimate. Sized for a reasoning-model turn: thinking
	// tokens are billed against max_tokens on OpenAI-compatible stacks, so
	// the floor must cover reasoning + one tool-call batch + prose, not
	// just prose. If the estimate was right and input+floor overflows the
	// window, the provider rejects and seed's context-overflow recovery
	// compaction fires, which is strictly better than silently sawing
	// every response off at a tiny floor when the heuristic merely
	// overestimated the prompt size (observed as finish=output_limit at
	// exactly the old 512 value on 80K–134K-token prompts with plenty of
	// real headroom).
	MinOutputTokens = 16384
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
	// BiasReservePercent caps the estimation-bias reserve at a share of the
	// REMAINING space once the 30% input inflation would eat more than that.
	// Set above EstimationErrorPercent so the cap is inert in the regime the
	// inflation was calibrated for: it binds only when input exceeds ~57% of
	// the window (0.4·remaining < 0.3·estimate). Below that line budgets are
	// bit-identical to the uncapped math — including the historical cold-
	// estimate case (est 116K vs real 156K on a 200K window, fully absorbed).
	// Above it, the uncapped reserve grew to 50K+ tokens and starved
	// ordinary big-prompt turns into finish=output_limit-at-the-floor (the
	// failures this file's history documents); the cap yields instead, and a
	// genuinely wrong cold estimate then fails loudly via the provider's
	// rejection + seed's overflow-recovery compaction rather than silently
	// truncating the response.
	BiasReservePercent = 40
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

// EstimateMessagesTokensWireView estimates message tokens as they appear on
// the wire when reasoning history is NOT replayed (the provider config has
// an empty reasoning_content_field and no structured reasoning_details
// replay). ReasoningContent is excluded because the provider never receives
// it — counting it inflates fallback estimates by the full reasoning mass
// (tens of thousands of tokens on reasoning-heavy agents), which skews both
// the context-usage display and the compaction trigger.
func EstimateMessagesTokensWireView(messages []Message) int {
	tokens := 0
	for _, msg := range messages {
		tokens += EstimateTokens(msg.Content)
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
		tokens += MessageOverheadTokens
	}
	return tokens
}

// EstimateInputTokensWireView is EstimateInputTokens over
// EstimateMessagesTokensWireView.
func EstimateInputTokensWireView(messages []Message, tools []Tool) int {
	return EstimateMessagesTokensWireView(messages) +
		len(tools)*ToolTokenEstimate +
		SystemInstructionBuffer
}

// CalculateOutputBudget calculates the safe output token budget given context constraints.
// It returns the maximum tokens that can be requested for completion.
// If the input exceeds the context limit, returns 0 and an error message.
//
// The !ok return is NOT "no budget": it means the estimate says input already
// fills the window, but the heuristic has been observed to overestimate by
// 80K+ tokens against the provider's true prompt count on real conversations
// (finish=output_limit at exactly the floor value, prompt far below the
// limit). The 512-token emergency floor then silently decapitated every
// response. ok=false now reports the full remaining window as the budget —
// the provider still owns the hard ceiling, and seed's context-overflow
// recovery can actually fire when the estimate was right.
func CalculateOutputBudget(contextLimit int, inputTokens int) (int, bool) {
	if contextLimit <= 0 {
		contextLimit = 32000 // Default fallback
	}

	// Check if input already exceeds context
	if inputTokens >= contextLimit {
		// The estimate says the prompt fills the window, but this estimate
		// can overestimate badly (heuristic vs real tokenizer). Pinning the
		// output to the emergency floor here caused every serious truncation
		// (finish=output_limit at exactly MinOutputTokens with prompts far
		// below the provider's true limit). Report the full remaining window
		// as the budget: the provider clamps the real overflow, and callers
		// with overflow recovery can still fire when the estimate was right.
		return max(contextLimit-inputTokens, 0), false
	}

	// Calculate remaining space
	remaining := contextLimit - inputTokens

	// Reserve for estimation bias, capped at a share of the remaining
	// space. EstimateTokens is a heuristic (not a real BPE tokenizer): it
	// has been observed to underestimate the true token count by 25-34% on
	// tool-heavy prompts and to run HOT on prose/reasoning-heavy history.
	// Inflating the estimate by EstimationErrorPercent captures the
	// underestimate regime, but uncapped the reserve grows to 50K+ tokens
	// on a 200K window and starved ordinary big-prompt turns (the
	// finish=output_limit-at-the-floor failures this file's history
	// documents). The cap bounds the posture: while remaining is large the
	// full 30% inflation applies; as it shrinks the reserve yields rather
	// than eating the response.
	biasReserve := min(
		(inputTokens*EstimationErrorPercent)/100,
		(remaining*BiasReservePercent)/100,
	)

	// Small fixed cushion for output-side rounding/formatting slop, separate
	// from the estimation-error margin above. Scales gently with window size
	// but stays modest — the bias reserve already carries most of the safety
	// margin.
	cushion := max((contextLimit*BaseCushionPercent)/100, BaseCushionFloor)

	maxOutput := remaining - biasReserve - cushion

	// Hard cap: max_tokens must never cause input + output to exceed
	// the context limit. This is the last line of defense against
	// estimation errors that slip past the margins above.
	maxOutput = min(maxOutput, remaining)

	// Below the minimum viable output, fall back to the floor — but only
	// when the real remaining space cannot cover it either. A reasoning
	// turn needs thinking + one tool batch + prose; if the estimate was
	// right and even the floor overflows, the provider rejects and seed's
	// overflow-recovery compaction fires.
	if maxOutput < MinOutputTokens {
		return min(MinOutputTokens, remaining), true
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
		// Same overestimate defense as CalculateOutputBudget: report the
		// remaining window (0) instead of a sentinel floor.
		return max(contextLimit-totalInput, 0), false
	}

	remaining := contextLimit - totalInput

	// Bias reserve on the heuristic portion only — the anchored portion is
	// a real measurement with no estimation error. Same cap as
	// CalculateOutputBudget so the two variants agree in the taper zone.
	biasReserve := min(
		(heuristicInput*EstimationErrorPercent)/100,
		(remaining*BiasReservePercent)/100,
	)

	cushion := max((contextLimit*BaseCushionPercent)/100, BaseCushionFloor)

	maxOutput := remaining - biasReserve - cushion
	maxOutput = min(maxOutput, remaining)

	// Same floor semantics as CalculateOutputBudget.
	if maxOutput < MinOutputTokens {
		return min(MinOutputTokens, remaining), true
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
