package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestAbbreviate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		text  string
		limit int
		want  string
	}{
		{
			name:  "short text no truncation",
			text:  "hello",
			limit: 20,
			want:  "hello",
		},
		{
			name:  "exact limit no truncation",
			text:  "hello",
			limit: 5,
			want:  "hello",
		},
		{
			name:  "truncation with ellipsis",
			text:  "hello world this is a long string",
			limit: 10,
			want:  "hello wor…",
		},
		{
			name:  "empty string",
			text:  "",
			limit: 10,
			want:  "",
		},
		{
			name:  "whitespace only trimmed",
			text:  "   \t\n  ",
			limit: 10,
			want:  "",
		},
		{
			name:  "leading and trailing whitespace trimmed",
			text:  "  hello world  ",
			limit: 8,
			want:  "hello w…",
		},
		{
			name:  "zero limit returns trimmed text",
			text:  "hello world",
			limit: 0,
			want:  "hello world",
		},
		{
			name:  "negative limit returns trimmed text",
			text:  "hello world",
			limit: -5,
			want:  "hello world",
		},
		{
			name:  "limit 1 returns first char no ellipsis",
			text:  "hello",
			limit: 1,
			want:  "h",
		},
		{
			name:  "limit 1 long string",
			text:  "hello world",
			limit: 1,
			want:  "h",
		},
		{
			name:  "limit 2 with truncation",
			text:  "hello",
			limit: 2,
			want:  "h…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := abbreviate(tt.text, tt.limit)
			if got != tt.want {
				t.Errorf("abbreviate(%q, %d) = %q, want %q", tt.text, tt.limit, got, tt.want)
			}
		})
	}
}

func TestShouldLogTurnSummaries(t *testing.T) {
	// nil agent → false
	t.Run("nil agent", func(t *testing.T) {
		t.Parallel()
		ch := &ConversationHandler{}
		if ch.shouldLogTurnSummaries() {
			t.Error("shouldLogTurnSummaries() = true with nil agent, want false")
		}
	})

	// debug=true → true
	t.Run("debug true", func(t *testing.T) {
		t.Parallel()
		a := &Agent{debug: true}
		ch := &ConversationHandler{agent: a}
		if !ch.shouldLogTurnSummaries() {
			t.Error("shouldLogTurnSummaries() = false with debug=true, want true")
		}
	})

	// debug=false, no env → false
	t.Run("debug false no env", func(t *testing.T) {
		t.Parallel()
		a := &Agent{debug: false}
		ch := &ConversationHandler{agent: a}
		if ch.shouldLogTurnSummaries() {
			t.Error("shouldLogTurnSummaries() = true with debug=false and no env, want false")
		}
	})
}

// Separate non-parallel test for env var tests to avoid t.Setenv + t.Parallel conflict
func TestShouldLogTurnSummaries_EnvVars(t *testing.T) {
	// debug=false, LOG_TURNS=1 → true
	t.Run("debug false env LOG_TURNS=1", func(t *testing.T) {
		t.Setenv("SPROUT_LOG_TURNS", "1")
		a := &Agent{debug: false}
		ch := &ConversationHandler{agent: a}
		if !ch.shouldLogTurnSummaries() {
			t.Error("shouldLogTurnSummaries() = false with LOG_TURNS=1, want true")
		}
	})

	// debug=false, LOG_TURNS=0 → false
	t.Run("debug false env LOG_TURNS=0", func(t *testing.T) {
		t.Setenv("SPROUT_LOG_TURNS", "0")
		a := &Agent{debug: false}
		ch := &ConversationHandler{agent: a}
		if ch.shouldLogTurnSummaries() {
			t.Error("shouldLogTurnSummaries() = true with LOG_TURNS=0, want false")
		}
	})
}

func TestLogTurnSummary(t *testing.T) {
	t.Parallel()

	// No-op when shouldLogTurnSummaries returns false
	t.Run("no-op when disabled", func(t *testing.T) {
		t.Parallel()
		a := &Agent{debug: false}
		ch := &ConversationHandler{agent: a}
		turn := TurnEvaluation{
			Iteration: 1,
		}
		// Should not panic
		ch.logTurnSummary(turn)
	})

	// With debug enabled, calls PrintLineAsync (verify no panic)
	t.Run("no panic with debug enabled", func(t *testing.T) {
		a := &Agent{debug: true}
		a.initSubManagers()
		ch := &ConversationHandler{agent: a}
		turn := TurnEvaluation{
			Iteration:         1,
			UserInput:         "test user input",
			AssistantContent:  "test assistant content",
			FinishReason:      "stop",
			ReasoningSnippet:  "some reasoning",
			ToolCalls:         []api.ToolCall{},
			ToolResults:       []api.Message{},
			TokenUsage:        TokenUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
			CompletionReached: true,
			GuardrailTrigger:  "test guardrail",
		}
		// Should not panic
		ch.logTurnSummary(turn)
	})
}
