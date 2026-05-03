package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func makeTestConversationHandler() *ConversationHandler {
	return &ConversationHandler{
		agent: &Agent{
			debug: false,
			state: NewAgentStateManager(false),
		},
	}
}

func TestIsBlankIterationWithToolCalls(t *testing.T) {
	ch := makeTestConversationHandler()

	toolCalls := []api.ToolCall{{ID: "tc1", Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: "read_file", Arguments: `{"path":"x"}`}}}

	// With tool calls, should not be blank regardless of content
	if got := ch.isBlankIteration("some content", toolCalls); got {
		t.Errorf("isBlankIteration(content, toolCalls) = %v, want false", got)
	}
	// Even empty content with tool calls is not blank
	if got := ch.isBlankIteration("", toolCalls); got {
		t.Errorf("isBlankIteration(\"\", toolCalls) = %v, want false", got)
	}
}

func TestIsBlankIterationEmptyAndWhitespace(t *testing.T) {
	ch := makeTestConversationHandler()

	// Empty content
	if got := ch.isBlankIteration("", nil); !got {
		t.Errorf("isBlankIteration(\"\", nil) = %v, want true", got)
	}

	// Only whitespace
	if got := ch.isBlankIteration("   \t\n  ", nil); !got {
		t.Errorf("isBlankIteration(whitespace, nil) = %v, want true", got)
	}
}

func TestIsBlankIterationSingleChar(t *testing.T) {
	ch := makeTestConversationHandler()

	// Single character — len 1 <= 1 → true
	if got := ch.isBlankIteration("a", nil); !got {
		t.Errorf("isBlankIteration(\"a\", nil) = %v, want true", got)
	}
	if got := ch.isBlankIteration("!", nil); !got {
		t.Errorf("isBlankIteration(\"!\", nil) = %v, want true", got)
	}
}

func TestIsBlankIterationTwoChars(t *testing.T) {
	ch := makeTestConversationHandler()

	// Two chars — len 2 > 1 (not blank by length check)
	// Then len 2 <= 3, so enters punct check. "ab" has non-punct chars → false
	if got := ch.isBlankIteration("ab", nil); got {
		t.Errorf("isBlankIteration(\"ab\", nil) = %v, want false", got)
	}
}

func TestIsBlankIterationPunctuationOnly(t *testing.T) {
	ch := makeTestConversationHandler()

	// Three punctuation chars
	if got := ch.isBlankIteration("...", nil); !got {
		t.Errorf("isBlankIteration(\"...\", nil) = %v, want true", got)
	}
	if got := ch.isBlankIteration("!!!", nil); !got {
		t.Errorf("isBlankIteration(\"!!!\", nil) = %v, want true", got)
	}
	if got := ch.isBlankIteration("--", nil); !got {
		t.Errorf("isBlankIteration(\"--\", nil) = %v, want true", got)
	}
}

func TestIsBlankIterationMixedChars(t *testing.T) {
	ch := makeTestConversationHandler()

	// Mixed punctuation and letters — contains non-punct char → false
	if got := ch.isBlankIteration("a.b", nil); got {
		t.Errorf("isBlankIteration(\"a.b\", nil) = %v, want false", got)
	}
}

func TestIsBlankIterationFourOrMoreChars(t *testing.T) {
	ch := makeTestConversationHandler()

	// 4+ chars → always false (doesn't enter punct check)
	if got := ch.isBlankIteration("abcd", nil); got {
		t.Errorf("isBlankIteration(\"abcd\", nil) = %v, want false", got)
	}
	if got := ch.isBlankIteration("this is real content", nil); got {
		t.Errorf("isBlankIteration(\"this is real content\", nil) = %v, want false", got)
	}
}

func TestIsRepetitiveContentPatternMatch(t *testing.T) {
	ch := makeTestConversationHandler()

	// Known repetitive pattern
	if got := ch.isRepetitiveContent("let me check for any simple improvements"); !got {
		t.Errorf("isRepetitiveContent(pattern) = %v, want true", got)
	}

	// Case insensitive
	if got := ch.isRepetitiveContent("Let me check for any simple improvements"); !got {
		t.Errorf("isRepetitiveContent(upper-case pattern) = %v, want true", got)
	}

	// Embedded in larger content
	if got := ch.isRepetitiveContent("Here is my analysis. let me check for any simple improvements and see what I can do."); !got {
		t.Errorf("isRepetitiveContent(embedded pattern) = %v, want true", got)
	}
}

func TestIsRepetitiveContentDuplicateOfPrevious(t *testing.T) {
	ch := makeTestConversationHandler()

	content := "I have finished the task."

	// Set up messages: user, assistant (the one to duplicate), then another user message
	// isRepetitiveContent iterates from len-2 backwards, breaks on first assistant
	ch.agent.state.SetMessages([]api.Message{
		{Role: "user", Content: "Do something"},
		{Role: "assistant", Content: content},
		{Role: "user", Content: "Continue"},
	})

	// Same content as previous assistant message → true
	if got := ch.isRepetitiveContent(content); !got {
		t.Errorf("isRepetitiveContent(duplicate) = %v, want true", got)
	}

	// Different content → false
	if got := ch.isRepetitiveContent("I have done something different."); got {
		t.Errorf("isRepetitiveContent(different) = %v, want false", got)
	}
}

func TestIsRepetitiveContentWordFrequency(t *testing.T) {
	ch := makeTestConversationHandler()

	// Word "testing" repeated 12 times — 12/12 = 100% > 30%, len > 3, and > 10 words
	if got := ch.isRepetitiveContent("testing testing testing testing testing testing testing testing testing testing testing testing"); !got {
		t.Errorf("isRepetitiveContent(repeated word) = %v, want true", got)
	}

	// Normal content should not trigger
	if got := ch.isRepetitiveContent("The quick brown fox jumps over the lazy dog. This is normal content that should not trigger repetition detection."); got {
		t.Errorf("isRepetitiveContent(normal) = %v, want false", got)
	}
}

func TestIsRepetitiveContentShortContent(t *testing.T) {
	ch := makeTestConversationHandler()

	// Short content (< 10 words) skips word frequency check
	if got := ch.isRepetitiveContent("testing testing testing"); got {
		t.Errorf("isRepetitiveContent(short repeated) = %v, want false (under 10 words)", got)
	}
}

func TestIsRepetitiveContentNormalContent(t *testing.T) {
	ch := makeTestConversationHandler()

	tests := []string{
		"I'll start by reading the file to understand its current structure.",
		"The function handles input validation and returns an error if the input is invalid.",
		"Based on my analysis, the code looks correct. I don't see any issues that need to be fixed.",
	}

	for _, content := range tests {
		if got := ch.isRepetitiveContent(content); got {
			t.Errorf("isRepetitiveContent(%q) = %v, want false", content[:min(50, len(content))]+"...", got)
		}
	}
}
