package agent

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestSanitizeToolMessagesV2(t *testing.T) {
	// Create a test conversation handler
	a := newTestAgent(t)
	ch := NewConversationHandler(a)

	t.Run("empty messages", func(t *testing.T) {
		result := ch.sanitizeToolMessages(nil)
		if len(result) != 0 {
			t.Errorf("sanitizeToolMessages(nil) = %d items; want 0", len(result))
		}
	})

	t.Run("only user messages", func(t *testing.T) {
		messages := []api.Message{
			{Role: "user", Content: "hello"},
			{Role: "system", Content: "instructions"},
		}
		result := ch.sanitizeToolMessages(messages)
		if len(result) != 2 {
			t.Errorf("sanitizeToolMessages() = %d items; want 2", len(result))
		}
	})

	t.Run("proper tool call pair", func(t *testing.T) {
		messages := []api.Message{
			{Role: "assistant", Content: "Let me check", ToolCalls: []api.ToolCall{
				{ID: "call_1", Type: "function", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "shell_command", Arguments: "{}"}},
			}},
			{Role: "tool", Content: "output", ToolCallId: "call_1"},
		}
		result := ch.sanitizeToolMessages(messages)
		if len(result) != 2 {
			t.Errorf("sanitizeToolMessages() = %d items; want 2 (both kept)", len(result))
		}
	})

	t.Run("orphaned tool result removed", func(t *testing.T) {
		messages := []api.Message{
			{Role: "user", Content: "hello"},
			{Role: "tool", Content: "orphaned result", ToolCallId: "call_orphan"},
		}
		result := ch.sanitizeToolMessages(messages)
		if len(result) != 1 {
			t.Errorf("sanitizeToolMessages() = %d items; want 1 (orphaned tool removed)", len(result))
		}
		if result[0].Role != "user" {
			t.Errorf("expected user message kept, got role: %s", result[0].Role)
		}
	})

	t.Run("tool result with empty tool_call_id removed", func(t *testing.T) {
		messages := []api.Message{
			{Role: "user", Content: "hello"},
			{Role: "tool", Content: "no id", ToolCallId: ""},
		}
		result := ch.sanitizeToolMessages(messages)
		if len(result) != 1 {
			t.Errorf("sanitizeToolMessages() = %d items; want 1 (tool with empty ID removed)", len(result))
		}
	})

	t.Run("multiple tool calls and results", func(t *testing.T) {
		messages := []api.Message{
			{Role: "assistant", Content: "Running tools", ToolCalls: []api.ToolCall{
				{ID: "call_a", Type: "function", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "read_file", Arguments: "{}"}},
				{ID: "call_b", Type: "function", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "write_file", Arguments: "{}"}},
			}},
			{Role: "tool", Content: "file content", ToolCallId: "call_a"},
			{Role: "tool", Content: "written", ToolCallId: "call_b"},
		}
		result := ch.sanitizeToolMessages(messages)
		if len(result) != 3 {
			t.Errorf("sanitizeToolMessages() = %d items; want 3", len(result))
		}
	})

	t.Run("mixed valid and orphaned", func(t *testing.T) {
		messages := []api.Message{
			{Role: "user", Content: "start"},
			{Role: "assistant", Content: "checking", ToolCalls: []api.ToolCall{
				{ID: "call_1", Type: "function", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "read_file", Arguments: "{}"}},
			}},
			{Role: "tool", Content: "result", ToolCallId: "call_1"},
			{Role: "tool", Content: "orphan", ToolCallId: "call_missing"},
		}
		result := ch.sanitizeToolMessages(messages)
		if len(result) != 3 {
			t.Errorf("sanitizeToolMessages() = %d items; want 3 (orphan removed)", len(result))
		}
		// Verify the orphaned tool message was removed
		for _, msg := range result {
			if msg.Role == "tool" && strings.Contains(msg.Content, "orphan") {
				t.Error("orphaned tool message should have been removed")
			}
		}
	})

	t.Run("assistant with no tool calls preserved", func(t *testing.T) {
		messages := []api.Message{
			{Role: "assistant", Content: "Just text, no tools"},
		}
		result := ch.sanitizeToolMessages(messages)
		if len(result) != 1 {
			t.Errorf("sanitizeToolMessages() = %d items; want 1", len(result))
		}
		if result[0].Role != "assistant" {
			t.Errorf("expected assistant message, got: %s", result[0].Role)
		}
	})

	t.Run("duplicate tool results — only first kept", func(t *testing.T) {
		// When there are two tool results for the same tool_call_id,
		// the second one should be dropped (already seen)
		messages := []api.Message{
			{Role: "assistant", Content: "checking", ToolCalls: []api.ToolCall{
				{ID: "call_dup", Type: "function", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "read_file", Arguments: "{}"}},
			}},
			{Role: "tool", Content: "first result", ToolCallId: "call_dup"},
			{Role: "tool", Content: "duplicate result", ToolCallId: "call_dup"},
		}
		result := ch.sanitizeToolMessages(messages)
		if len(result) != 2 {
			t.Errorf("sanitizeToolMessages() = %d items; want 2 (duplicate removed)", len(result))
		}
	})
}
