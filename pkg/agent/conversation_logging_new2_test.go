package agent

import (
	"testing"
	"time"
)

// TestAbbreviate2 tests abbreviate function
func TestAbbreviate2(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		limit    int
		expected string
	}{
		{"empty", "", 10, ""},
		{"shorter than limit", "hello", 10, "hello"},
		{"over limit", "hello world", 8, "hello w…"},
		{"zero limit", "hello", 0, "hello"},
		{"negative limit", "hello", -5, "hello"},
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

// TestShouldLogTurnSummaries2 tests logging conditions
func TestShouldLogTurnSummaries2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.debug = true

	ch := &ConversationHandler{
		agent: a,
	}

	if !ch.shouldLogTurnSummaries() {
		t.Error("expected true when debug is true")
	}
}

// TestLogTurnSummary2 tests logging doesn't panic
func TestLogTurnSummary2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.debug = true

	ch := &ConversationHandler{
		agent: a,
	}

	now := time.Now()
	turn := TurnEvaluation{
		Iteration: 1,
		Timestamp: now,
		TokenUsage: TokenUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	// Should not panic
	ch.logTurnSummary(turn)
}
