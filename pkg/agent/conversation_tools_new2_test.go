package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// createToolCall2 helper creates a ToolCall for testing (unique name)
func createToolCall2(id, name, args string) api.ToolCall {
	return api.ToolCall{
		ID:   id,
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name:      name,
			Arguments: args,
		},
	}
}

// TestSanitizeToolMessages_Empty2 tests empty message array
func TestSanitizeToolMessages_Empty2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.debug = true

	ch := &ConversationHandler{
		agent: a,
	}

	messages := []api.Message{}

	result := ch.sanitizeToolMessages(messages)

	if len(result) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result))
	}
}

// TestSanitizeToolMessages_ValidPair2 tests valid assistant/tool call pair
func TestSanitizeToolMessages_ValidPair2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.debug = true

	ch := &ConversationHandler{
		agent: a,
	}

	messages := []api.Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "What time is it?"},
		{Role: "assistant", Content: "I'll check", ToolCalls: []api.ToolCall{
			createToolCall2("call_1", "get_time", "{}"),
		}},
		{Role: "tool", ToolCallId: "call_1", Content: "12:00 PM"},
	}

	result := ch.sanitizeToolMessages(messages)

	if len(result) != 4 {
		t.Errorf("expected 4 messages, got %d", len(result))
	}

	if result[2].Role != "assistant" {
		t.Error("expected assistant at index 2")
	}
	if result[3].Role != "tool" {
		t.Error("expected tool at index 3")
	}
}

// TestSanitizeToolMessages_OrphanedToolResult2 tests dropping orphaned tool results
func TestSanitizeToolMessages_OrphanedToolResult2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.debug = true

	ch := &ConversationHandler{
		agent: a,
	}

	messages := []api.Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "Hello"},
		{Role: "tool", ToolCallId: "orphan_123", Content: "some result"},
	}

	result := ch.sanitizeToolMessages(messages)

	if len(result) != 2 {
		t.Errorf("expected 2 messages (orphaned tool dropped), got %d", len(result))
	}

	if result[1].Role == "tool" {
		t.Error("orphaned tool result should have been dropped")
	}
}

// TestSanitizeToolMessages_DuplicateToolCallIds2 tests handling of duplicate tool call IDs
func TestSanitizeToolMessages_DuplicateToolCallIds2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.debug = true

	ch := &ConversationHandler{
		agent: a,
	}

	messages := []api.Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "assistant", Content: "I'll check", ToolCalls: []api.ToolCall{
			createToolCall2("call_1", "get_time", "{}"),
		}},
		{Role: "tool", ToolCallId: "call_1", Content: "result 1"},
		{Role: "tool", ToolCallId: "call_1", Content: "result 2"},
	}

	result := ch.sanitizeToolMessages(messages)

	if len(result) != 3 {
		t.Errorf("expected 3 messages (duplicate tool dropped), got %d", len(result))
	}
}

// TestSanitizeToolMessages_OrderPreservation2 tests that message order is preserved
func TestSanitizeToolMessages_OrderPreservation2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.debug = true

	ch := &ConversationHandler{
		agent: a,
	}

	messages := []api.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "user1"},
		{Role: "assistant", Content: "asst1"},
		{Role: "user", Content: "user2"},
		{Role: "assistant", Content: "asst2", ToolCalls: []api.ToolCall{
			createToolCall2("call_1", "tool1", "{}"),
		}},
		{Role: "tool", ToolCallId: "call_1", Content: "res1"},
		{Role: "user", Content: "user3"},
	}

	result := ch.sanitizeToolMessages(messages)

	if len(result) != 7 {
		t.Errorf("expected 7 messages, got %d", len(result))
	}

	expectedOrder := []string{"system", "user", "assistant", "user", "assistant", "tool", "user"}
	for i, expectedRole := range expectedOrder {
		if result[i].Role != expectedRole {
			t.Errorf("expected %s at index %d, got %s", expectedRole, i, result[i].Role)
		}
	}
}

// TestSanitizeToolMessages_NilAgent2 tests with nil agent
func TestSanitizeToolMessages_NilAgent2(t *testing.T) {
	ch := &ConversationHandler{
		agent: nil,
	}

	messages := []api.Message{
		{Role: "system", Content: "sys"},
	}

	result := ch.sanitizeToolMessages(messages)
	if len(result) != 1 {
		t.Errorf("expected 1 message, got %d", len(result))
	}
}

// TestSanitizeToolMessages_MultipleToolCalls2 tests assistant with multiple tool calls
func TestSanitizeToolMessages_MultipleToolCalls2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.debug = true

	ch := &ConversationHandler{
		agent: a,
	}

	messages := []api.Message{
		{Role: "system", Content: "sys"},
		{Role: "assistant", Content: "checking", ToolCalls: []api.ToolCall{
			createToolCall2("call_1", "tool1", "{}"),
			createToolCall2("call_2", "tool2", "{}"),
		}},
		{Role: "tool", ToolCallId: "call_1", Content: "res1"},
		{Role: "tool", ToolCallId: "call_2", Content: "res2"},
	}

	result := ch.sanitizeToolMessages(messages)

	if len(result) != 4 {
		t.Errorf("expected 4 messages, got %d", len(result))
	}
}
