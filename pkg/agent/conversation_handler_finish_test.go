package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestHandleFinishReason_Empty(t *testing.T) {
	t.Parallel()
	ch := &ConversationHandler{
		agent: &Agent{},
	}
	shouldStop, reason := ch.handleFinishReason("", "some content")
	if shouldStop {
		t.Error("expected false for empty finish reason")
	}
	if reason != "" {
		t.Errorf("expected empty reason, got %q", reason)
	}
}

func TestHandleFinishReason_ToolCalls(t *testing.T) {
	t.Parallel()
	ch := &ConversationHandler{
		agent: &Agent{},
	}
	shouldStop, reason := ch.handleFinishReason("tool_calls", "let me check that")
	if shouldStop {
		t.Error("expected false for tool_calls finish reason")
	}
	if reason != "model tool_calls finish" {
		t.Errorf("expected reason 'model tool_calls finish', got %q", reason)
	}
}

func TestHandleFinishReason_Length(t *testing.T) {
	t.Parallel()
	ch := &ConversationHandler{
		agent: &Agent{},
	}
	shouldStop, reason := ch.handleFinishReason("length", "content")
	if shouldStop {
		t.Error("expected false for length finish reason")
	}
	if reason != "model length limit" {
		t.Errorf("expected reason 'model length limit', got %q", reason)
	}
	// Verify continuation message was queued
	if len(ch.transientMessages) != 1 {
		t.Fatalf("expected 1 transient message, got %d", len(ch.transientMessages))
	}
	if ch.transientMessages[0].Role != "user" {
		t.Errorf("expected transient message role 'user', got %q", ch.transientMessages[0].Role)
	}
}

func TestHandleFinishReason_ContentFilter(t *testing.T) {
	t.Parallel()
	ch := &ConversationHandler{
		agent: &Agent{},
	}
	shouldStop, reason := ch.handleFinishReason("content_filter", "filtered content")
	if shouldStop {
		t.Error("expected false for content_filter finish reason")
	}
	if reason != "content filtered" {
		t.Errorf("expected reason 'content filtered', got %q", reason)
	}
}

func TestHandleFinishReason_Unknown(t *testing.T) {
	t.Parallel()
	ch := &ConversationHandler{
		agent: &Agent{},
	}
	shouldStop, reason := ch.handleFinishReason("custom_reason", "content")
	if shouldStop {
		t.Error("expected false for unknown finish reason")
	}
	if reason != "unknown finish reason: custom_reason" {
		t.Errorf("expected 'unknown finish reason: custom_reason', got %q", reason)
	}
}

func TestHandleFinishReason_StopWithContentCompletes(t *testing.T) {
	t.Parallel()
	ch := &ConversationHandler{
		agent: &Agent{},
	}
	shouldStop, reason := ch.handleFinishReason("stop", "Final answer here")
	if !shouldStop {
		t.Error("expected true for stop with non-empty content")
	}
	if reason != "completion" {
		t.Errorf("expected reason 'completion', got %q", reason)
	}
}

func TestFollowsRecentToolResults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		messages []api.Message
		expected bool
	}{
		{
			name:     "no messages",
			messages: []api.Message{},
			expected: false,
		},
		{
			name:     "nil handler",
			messages: []api.Message{{Role: "tool", Content: "result"}},
			expected: false, // tested via nil ch
		},
		{
			name: "tool results followed by assistant",
			messages: []api.Message{
				{Role: "user", Content: "query"},
				{Role: "assistant", Content: "checking", ToolCalls: []api.ToolCall{{ID: "c1"}}},
				{Role: "tool", ToolCallID: "c1", Content: "result"},
				{Role: "assistant", Content: "now I have the result"},
			},
			expected: true,
		},
		{
			name: "multiple tool results followed by assistant",
			messages: []api.Message{
				{Role: "user", Content: "query"},
				{Role: "assistant", Content: "checking", ToolCalls: []api.ToolCall{{ID: "c1"}, {ID: "c2"}}},
				{Role: "tool", ToolCallID: "c1", Content: "result1"},
				{Role: "tool", ToolCallID: "c2", Content: "result2"},
				{Role: "assistant", Content: "now I have results"},
			},
			expected: true,
		},
		{
			name: "no tool results (user to assistant)",
			messages: []api.Message{
				{Role: "user", Content: "query"},
				{Role: "assistant", Content: "response"},
			},
			expected: false,
		},
		{
			name: "single assistant message",
			messages: []api.Message{
				{Role: "assistant", Content: "response"},
			},
			expected: false,
		},
		{
			name: "tool result without preceding assistant (edge case)",
			messages: []api.Message{
				{Role: "tool", Content: "result"},
			},
			expected: true,
		},
		{
			name: "only user message",
			messages: []api.Message{
				{Role: "user", Content: "query"},
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent := &Agent{
				state: NewAgentStateManager(false),
			}
			agent.state.SetMessages(tc.messages)

			var ch *ConversationHandler
			if tc.name != "nil handler" {
				ch = &ConversationHandler{agent: agent}
			}

			if got := ch.followsRecentToolResults(); got != tc.expected {
				t.Errorf("followsRecentToolResults() = %v, expected %v", got, tc.expected)
			}
		})
	}
}
