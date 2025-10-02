package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestSanitizeToolMessagesDropsOrphanToolResult(t *testing.T) {
	handler := &ConversationHandler{agent: &Agent{}}

	messages := []api.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "do something"},
		{Role: "tool", Content: "Tool call result for read_file: pkg/foo.go", ToolCallId: "call-orphan"},
	}

	sanitized := handler.sanitizeToolMessages(messages)

	if len(sanitized) != 2 {
		t.Fatalf("expected orphan tool message to be dropped, got %d messages", len(sanitized))
	}

	for _, msg := range sanitized {
		if msg.Role == "tool" {
			t.Fatalf("expected no tool messages after sanitization, got %#v", msg)
		}
	}
}

func TestSanitizeToolMessagesKeepsMatchingToolResult(t *testing.T) {
	handler := &ConversationHandler{agent: &Agent{}}

	assistant := api.Message{
		Role: "assistant",
		ToolCalls: []api.ToolCall{
			{
				ID:   "call-keep",
				Type: "function",
			},
		},
	}

	tool := api.Message{
		Role:       "tool",
		Content:    "Tool call result for read_file: pkg/foo.go",
		ToolCallId: "call-keep",
	}

	sanitized := handler.sanitizeToolMessages([]api.Message{assistant, tool})

	if len(sanitized) != 2 {
		t.Fatalf("expected both messages to remain, got %d", len(sanitized))
	}

	foundTool := false
	for _, msg := range sanitized {
		if msg.Role == "tool" {
			foundTool = true
			if msg.ToolCallId != "call-keep" {
				t.Fatalf("unexpected tool_call_id: %s", msg.ToolCallId)
			}
		}
	}

	if !foundTool {
		t.Fatalf("expected tool message to be preserved")
	}
}
