package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// --- sanitizeToolMessages tests (from conversation_tools.go) ---

func TestSanitizeToolMessages_NilAndEmpty(t *testing.T) {
	ch := &ConversationHandler{agent: &Agent{}}
	if result := ch.sanitizeToolMessages(nil); result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
	if result := ch.sanitizeToolMessages([]api.Message{}); len(result) != 0 {
		t.Errorf("expected empty for empty input")
	}
}

func TestSanitizeToolMessages_DropsOrphanedToolMessages(t *testing.T) {
	ch := &ConversationHandler{agent: &Agent{debug: false}}
	messages := []api.Message{
		{Role: "user", Content: "hello"},
		{Role: "tool", Content: "orphaned", ToolCallID: "missing_id"},
		{Role: "assistant", Content: "response"},
	}

	result := ch.sanitizeToolMessages(messages)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
}

func TestSanitizeToolMessages_KeepsMatchingToolResult(t *testing.T) {
	ch := &ConversationHandler{agent: &Agent{debug: false}}

	tc := api.ToolCall{ID: "call_1"}
	tc.Function.Name = "test"

	messages := []api.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{tc}},
		{Role: "tool", Content: "result", ToolCallID: "call_1"},
	}

	result := ch.sanitizeToolMessages(messages)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
}

func TestSanitizeToolMessages_DropsToolResultWithoutID(t *testing.T) {
	ch := &ConversationHandler{agent: &Agent{debug: false}}

	tc := api.ToolCall{ID: "call_1"}
	tc.Function.Name = "test"

	messages := []api.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{tc}},
		{Role: "tool", Content: "no-id", ToolCallID: ""},
	}

	result := ch.sanitizeToolMessages(messages)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages (no-ID tool dropped), got %d", len(result))
	}
}

// --- determineReasoningEffort nil checks ---

func TestDetermineReasoningEffort_NilChecks(t *testing.T) {
	ch := (*ConversationHandler)(nil)
	if result := ch.determineReasoningEffort(); result != "" {
		t.Errorf("expected empty for nil handler, got %q", result)
	}

	ch2 := &ConversationHandler{agent: nil}
	if result := ch2.determineReasoningEffort(); result != "" {
		t.Errorf("expected empty for nil agent, got %q", result)
	}
}
