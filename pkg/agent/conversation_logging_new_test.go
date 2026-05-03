package agent

import (
	"testing"
)

func TestAbbreviateV2(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		limit    int
		expected string
	}{
		// No truncation needed
		{"short text", "hello", 20, "hello"},
		{"exact limit", "hello", 5, "hello"},
		{"empty string", "", 10, ""},
		{"whitespace trimmed", "  hello  ", 20, "hello"},

		// Truncation (note: abbreviate uses byte-based len(), not rune count)
		{"truncate long text", "this is a very long string that needs truncation", 20, "this is a very long…"},
		{"truncate with limit 10", "abcdefghij", 10, "abcdefghij"},
		{"truncate with limit 9", "abcdefghij", 9, "abcdefgh…"},
		{"truncate with limit 5", "hello world", 5, "hell…"},
		{"truncate exact boundary", "123456789012345", 15, "123456789012345"},
		{"truncate one over", "1234567890123456", 15, "12345678901234…"},

		// Edge cases
		{"zero limit", "hello", 0, "hello"},
		{"negative limit", "hello", -5, "hello"},
		{"limit 1", "hello", 1, "h"},
		{"limit 2", "hello", 2, "h…"},

		// Edge cases
		{"leading whitespace", "  hello world  ", 10, "hello wor…"},
		{"all whitespace", "   ", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := abbreviate(tt.text, tt.limit)
			if result != tt.expected {
				t.Errorf("abbreviate(%q, %d) = %q; want %q", tt.text, tt.limit, result, tt.expected)
			}
		})
	}
}

func TestShouldLogTurnSummariesV2(t *testing.T) {
	// When agent is nil, should return false
	ch := &ConversationHandler{}
	if ch.shouldLogTurnSummaries() {
		t.Error("shouldLogTurnSummaries() should return false when agent is nil")
	}

	// When agent is not nil but debug is false, and LOG_TURNS is not set, should return false
	a := newTestAgent(t)
	ch2 := &ConversationHandler{agent: a}
	// newTestAgent creates a non-debug agent by default
	if ch2.shouldLogTurnSummaries() {
		t.Error("shouldLogTurnSummaries() should return false when debug is off and LOG_TURNS is not set")
	}
}
