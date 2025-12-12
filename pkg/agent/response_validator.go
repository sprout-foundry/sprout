package agent

import (
	"strings"
	"unicode"
)

// ResponseValidator validates LLM responses for completeness and quality
type ResponseValidator struct {
	agent *Agent
}

// NewResponseValidator creates a new response validator
func NewResponseValidator(agent *Agent) *ResponseValidator {
	return &ResponseValidator{
		agent: agent,
	}
}



// IsIncomplete checks if a response appears to be incomplete
func (rv *ResponseValidator) IsIncomplete(content string) bool {
	// Skip validation if streaming or if content is empty
	if rv.agent.streamingEnabled || len(content) == 0 {
		return false
	}

	// Check various indicators of incomplete responses
	return rv.hasIncompletePatterns(content) ||
		rv.hasAbruptEnding(content) ||
		rv.isUnusuallyShort(content) ||
		rv.hasIncompleteCodeBlock(content)
}

// hasIncompletePatterns checks for patterns indicating incomplete response
func (rv *ResponseValidator) hasIncompletePatterns(content string) bool {
	// Check for obvious incomplete endings
	trimmed := strings.TrimSpace(content)

	// Check for ellipsis at the end
	if strings.HasSuffix(trimmed, "...") {
		return true
	}

	return false
}

// hasAbruptEnding checks if the response ends abruptly
func (rv *ResponseValidator) hasAbruptEnding(content string) bool {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) == 0 {
		return false
	}

	lastChar := trimmed[len(trimmed)-1]

	// Check if it ends without proper punctuation
	if !unicode.IsPunct(rune(lastChar)) || lastChar == ',' || lastChar == '-' {
		// Exception for code blocks
		if !strings.Contains(content, "```") {
			return true
		}
	}

	return false
}

// isUnusuallyShort checks if response is too short for the context
func (rv *ResponseValidator) isUnusuallyShort(content string) bool {
	// Very short responses might be incomplete
	wordCount := len(strings.Fields(content))

	// If the response is very short and doesn't look like a complete answer
	if wordCount < 10 && !rv.isCompleteShortAnswer(content) {
		return true
	}

	return false
}

// isCompleteShortAnswer checks if a short response is actually complete
func (rv *ResponseValidator) isCompleteShortAnswer(content string) bool {
	// Some responses are naturally short but complete
	shortCompletePatterns := []string{
		"done",
		"completed",
		"finished",
		"yes",
		"no",
		"error:",
		"success",
		"failed",
	}

	contentLower := strings.ToLower(strings.TrimSpace(content))
	for _, pattern := range shortCompletePatterns {
		if strings.Contains(contentLower, pattern) {
			return true
		}
	}

	return false
}

// hasIncompleteCodeBlock checks for unclosed code blocks
func (rv *ResponseValidator) hasIncompleteCodeBlock(content string) bool {
	// Count code block markers
	codeBlockCount := strings.Count(content, "```")

	// Odd number means unclosed code block
	return codeBlockCount%2 != 0
}

// ValidateToolCalls checks if tool calls in content are valid
func (rv *ResponseValidator) ValidateToolCalls(content string) bool {
	// Check for malformed tool call attempts
	if rv.containsAttemptedToolCalls(content) {
		rv.agent.debugLog("⚠️ Detected attempted tool calls in message content\n")
		return false
	}

	return true
}

// containsAttemptedToolCalls checks if content has attempted but malformed tool calls
func (rv *ResponseValidator) containsAttemptedToolCalls(content string) bool {
	if content == "" {
		return false
	}

	// Patterns that suggest attempted tool calls
	attemptedPatterns := []string{
		`"tool_calls"`,
		`"function"`,
		`"arguments"`,
		`"tool_use"`,
		`"function_calls"`,
		`<tool_calls>`,
		`<function_calls>`,
		`[TOOL_CALL]`,
		`[FUNCTION]`,
		`{"name":`,
		`{"tool":`,
		`{"function":`,
		`<function=`,
		`</function>`,
		"I'll use the",
		"I'll call the",
		"Using the",
		"Calling the",
		"Let me use the",
		"I need to use",
	}

	for _, pattern := range attemptedPatterns {
		if strings.Contains(content, pattern) {
			// But only if there are no actual tool calls
			return true
		}
	}

	return false
}
