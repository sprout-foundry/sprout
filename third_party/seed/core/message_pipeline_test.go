package core

import (
	"fmt"
	"testing"
)

func TestReorderToolResultsForThreading_NoToolCalls(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}

	result := reorderToolResultsForThreading(messages)

	if len(result) != len(messages) {
		t.Fatalf("expected %d messages, got %d", len(messages), len(result))
	}
	for i := range result {
		if result[i].Content != messages[i].Content {
			t.Errorf("message %d content changed unexpectedly", i)
		}
	}
}

func TestReorderToolResultsForThreading_SingleToolCall(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Read this file"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		}},
		{Role: "tool", Content: "file content", ToolCallID: "call-1"},
	}

	result := reorderToolResultsForThreading(messages)

	if len(result) != len(messages) {
		t.Fatalf("expected %d messages, got %d", len(messages), len(result))
	}

	// Verify order is preserved
	if result[3].ToolCallID != "call-1" {
		t.Errorf("tool result ID mismatch: got %s", result[3].ToolCallID)
	}
}

func TestReorderToolResultsForThreading_MultipleToolCallsInOrder(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Read these files"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
			{ID: "call-2", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"b.txt"}`}},
			{ID: "call-3", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"c.txt"}`}},
		}},
		{Role: "tool", Content: "content a", ToolCallID: "call-1"},
		{Role: "tool", Content: "content b", ToolCallID: "call-2"},
		{Role: "tool", Content: "content c", ToolCallID: "call-3"},
	}

	result := reorderToolResultsForThreading(messages)

	if len(result) != len(messages) {
		t.Fatalf("expected %d messages, got %d", len(messages), len(result))
	}

	// Verify order is preserved (already in order)
	expectedIDs := []string{"call-1", "call-2", "call-3"}
	for i, expected := range expectedIDs {
		if result[3+i].ToolCallID != expected {
			t.Errorf("tool result %d: expected %s, got %s", i, expected, result[3+i].ToolCallID)
		}
	}
}

func TestReorderToolResultsForThreading_MultipleToolCallsOutOfOrder(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Read these files"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
			{ID: "call-2", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"b.txt"}`}},
			{ID: "call-3", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"c.txt"}`}},
		}},
		// Results arrive out of order (e.g., parallel execution)
		{Role: "tool", Content: "content c", ToolCallID: "call-3"},
		{Role: "tool", Content: "content a", ToolCallID: "call-1"},
		{Role: "tool", Content: "content b", ToolCallID: "call-2"},
	}

	result := reorderToolResultsForThreading(messages)

	if len(result) != len(messages) {
		t.Fatalf("expected %d messages, got %d", len(messages), len(result))
	}

	// Verify results are reordered to match tool call order
	expectedIDs := []string{"call-1", "call-2", "call-3"}
	expectedContent := []string{"content a", "content b", "content c"}
	for i, expected := range expectedIDs {
		if result[3+i].ToolCallID != expected {
			t.Errorf("tool result %d: expected ID %s, got %s", i, expected, result[3+i].ToolCallID)
		}
		if result[3+i].Content != expectedContent[i] {
			t.Errorf("tool result %d: expected content %q, got %q", i, expectedContent[i], result[3+i].Content)
		}
	}
}

func TestReorderToolResultsForThreading_EmptyToolCallIDs(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Do something"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
			{ID: "call-2", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"b.txt"}`}},
		}},
		{Role: "tool", Content: "content a", ToolCallID: "call-1"},
		{Role: "tool", Content: "content b", ToolCallID: "call-2"},
	}

	result := reorderToolResultsForThreading(messages)

	if len(result) != len(messages) {
		t.Fatalf("expected %d messages, got %d", len(messages), len(result))
	}

	// call-2 should come first (it's in the tool call order), then call-1 (orphaned within block)
	if result[3].ToolCallID != "call-2" {
		t.Errorf("expected first tool result to be call-2, got %s", result[3].ToolCallID)
	}
}

func TestReorderToolResultsForThreading_MultipleAssistantBlocks(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Do something"},
		// First assistant turn with tool calls
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
			{ID: "call-2", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"b.txt"}`}},
		}},
		// Results out of order
		{Role: "tool", Content: "content b", ToolCallID: "call-2"},
		{Role: "tool", Content: "content a", ToolCallID: "call-1"},
		// Second assistant turn with tool calls
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-3", Function: ToolCallFunction{Name: "search_files", Arguments: `{"pattern":"foo"}`}},
			{ID: "call-4", Function: ToolCallFunction{Name: "search_files", Arguments: `{"pattern":"bar"}`}},
		}},
		// Results out of order
		{Role: "tool", Content: "search bar results", ToolCallID: "call-4"},
		{Role: "tool", Content: "search foo results", ToolCallID: "call-3"},
		// Final assistant response
		{Role: "assistant", Content: "Here are the results..."},
	}

	result := reorderToolResultsForThreading(messages)

	if len(result) != len(messages) {
		t.Fatalf("expected %d messages, got %d", len(messages), len(result))
	}

	// First block: call-1, call-2 (indices 3, 4 after system/user/assistant)
	if result[3].ToolCallID != "call-1" {
		t.Errorf("first block result 0: expected call-1, got %s", result[3].ToolCallID)
	}
	if result[4].ToolCallID != "call-2" {
		t.Errorf("first block result 1: expected call-2, got %s", result[4].ToolCallID)
	}

	// Second block: call-3, call-4 (indices 6, 7 after system/user/assistant1/tools1/assistant2)
	if result[6].ToolCallID != "call-3" {
		t.Errorf("second block result 0: expected call-3, got %s", result[6].ToolCallID)
	}
	if result[7].ToolCallID != "call-4" {
		t.Errorf("second block result 1: expected call-4, got %s", result[7].ToolCallID)
	}
}

func TestReorderToolResultsForThreading_UserMessageBetweenBlocks(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Do something"},
		// First assistant turn with tool calls
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		}},
		{Role: "tool", Content: "content a", ToolCallID: "call-1"},
		// Injected user message (e.g., steer message)
		{Role: "user", Content: "Also check this"},
		// Second assistant turn
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call-2", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"b.txt"}`}},
		}},
		{Role: "tool", Content: "content b", ToolCallID: "call-2"},
	}

	result := reorderToolResultsForThreading(messages)

	if len(result) != len(messages) {
		t.Fatalf("expected %d messages, got %d", len(messages), len(result))
	}

	// Verify the user message is still between the blocks
	// Index layout: 0=system, 1=user, 2=assistant(tc), 3=tool, 4=user(steer), 5=assistant(tc), 6=tool
	if result[4].Role != "user" {
		t.Errorf("expected user message at index 4, got role %s at index 4 (actual layout: %v)", result[4].Role, func() string {
			var s string
			for i, m := range result {
				s += fmt.Sprintf("%d:%s", i, m.Role)
				if m.Role == "tool" {
					s += "(" + m.ToolCallID + ")"
				}
				s += " "
			}
			return s
		}())
	}
}

func TestReorderToolResultsForThreading_EmptyMessages(t *testing.T) {
	result := reorderToolResultsForThreading(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}

	result = reorderToolResultsForThreading([]Message{})
	if len(result) != 0 {
		t.Errorf("expected empty for empty input, got %d messages", len(result))
	}

	result = reorderToolResultsForThreading([]Message{{Role: "user", Content: "hi"}})
	if len(result) != 1 {
		t.Errorf("expected 1 message for single message, got %d", len(result))
	}
}
