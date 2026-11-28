package api

import (
	"strings"
	"sync"

	core "github.com/sprout-foundry/seed/core"
)

// Token estimation constants
const (
	// DefaultBufferTokens is the safety buffer for estimation errors
	DefaultBufferTokens = 1000
	// MinOutputTokens and the output-budget constants below are seed's
	// (core/output_budget.go), where their reasons are recorded.
	MinOutputTokens = core.MinOutputTokens
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
	// InlineImageTokenEstimate is the flat token cost for inline image
	// payloads (base64 in the Base64 field or embedded in a data: URL).
	// Vision providers bill images by pixel area, not payload bytes: a
	// typical 1080p screenshot costs ~1-2K tokens (OpenAI high-detail
	// ~1105, Anthropic 1585, Gemini ~516), while running the payload's
	// base64 through the text heuristic charges ~0.25 tokens per base64
	// character — tens of thousands per screenshot, 20-100x the real
	// cost. That inflation pushed the context estimate past the window
	// on image-heavy turns, tripped the compaction trigger on healthy
	// sessions, and starved the output budget. The flat cost sits ~2x
	// above the typical screenshot charge; if it ever undershoots a very
	// large image the provider rejects the request and seed's
	// overflow-recovery compaction fires — the safe direction to err in.
	InlineImageTokenEstimate = 2048
	EstimationErrorPercent   = core.EstimationErrorPercent
	BiasReservePercent       = core.BiasReservePercent
	BaseCushionPercent       = core.BaseCushionPercent
	BaseCushionFloor         = core.BaseCushionFloor
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

// CalculateOutputBudget is seed's output budget (core.CalculateOutputBudget):
// the output tokens a call can ask for in the window, given its estimated
// input.
func CalculateOutputBudget(contextLimit int, inputTokens int) (int, bool) {
	return core.CalculateOutputBudget(contextLimit, inputTokens)
}

// CalculateOutputBudgetAnchored is seed's anchored output budget: the error
// reserve applies to the estimated part of the input only.
func CalculateOutputBudgetAnchored(contextLimit, anchoredInput, heuristicInput int) (int, bool) {
	return core.CalculateOutputBudgetAnchored(contextLimit, anchoredInput, heuristicInput)
}

func estimateImageTokens(img ImageData) int {
	if isInlineImagePayload(img) {
		// Inline base64 payloads are billed by the provider as image
		// tokens (pixel area), never as text: running the payload itself
		// through the text heuristic overstates a 300KB screenshot by
		// ~100K tokens against the ~1-2K the provider actually counts.
		return ImageMessageOverheadTokens + InlineImageTokenEstimate
	}

	tokens := ImageMessageOverheadTokens

	if img.URL != "" {
		tokens += EstimateTokens(img.URL)
	}

	if img.Type != "" {
		tokens += EstimateTokens(img.Type)
	}

	return tokens
}

// isInlineImagePayload reports whether the image carries its pixel data
// inline — base64 in Base64, or a data: URI in URL — as opposed to a
// fetchable http(s) URL.
func isInlineImagePayload(img ImageData) bool {
	if img.Base64 != "" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(img.URL), "data:")
}
