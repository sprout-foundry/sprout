package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestStripConsecutiveAssistantMessages_EndOfList tests that consecutive assistant
// messages at the end of the list are properly stripped to avoid llama.cpp error:
// "Cannot have 2 or more assistant messages at the end of the list"
func TestStripConsecutiveAssistantMessages_EndOfList(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	
	handler := NewConversationHandler(agent)

	// Test case 1: Two consecutive assistant messages at the end
	messages := []api.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Summary of previous work"},
		{Role: "assistant", Content: "Another assistant message"},
	}
	
	result := handler.stripConsecutiveAssistantMessages(messages)
	
	if len(result) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result))
	}
	
	// Should keep user message and last assistant message
	if result[0].Role != "user" {
		t.Errorf("Expected first message to be user, got %s", result[0].Role)
	}
	if result[1].Role != "assistant" {
		t.Errorf("Expected second message to be assistant, got %s", result[1].Role)
	}
	if result[1].Content != "Another assistant message" {
		t.Errorf("Expected last assistant message content, got %q", result[1].Content)
	}
}

// TestStripConsecutiveAssistantMessages_WithToolCalls tests that assistant messages
// with tool_calls are preserved even if consecutive
func TestStripConsecutiveAssistantMessages_WithToolCalls(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	
	handler := NewConversationHandler(agent)

	// Test case: Assistant with tool_calls should be preserved, but trailing
	// assistant without tool_calls should be stripped to avoid consecutive assistants
	messages := []api.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Summary"},
		{Role: "assistant", ToolCalls: []api.ToolCall{{ID: "call1", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "test", Arguments: ""}}}},
		{Role: "assistant", Content: "Another summary"},
	}
	
	result := handler.stripConsecutiveAssistantMessages(messages)
	
	// Should keep messages 0, 1, 2 (user, summary, assistant with tool_calls)
	// and strip message 3 (trailing assistant without tool_calls)
	if len(result) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(result))
	}
	
	// Verify the last message is the one with tool_calls
	if len(result[2].ToolCalls) == 0 {
		t.Errorf("Expected last message to have tool_calls")
	}
}

// TestStripConsecutiveAssistantMessages_NoConsecutive tests that no changes are made
// when there are no consecutive assistant messages
func TestStripConsecutiveAssistantMessages_NoConsecutive(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	
	handler := NewConversationHandler(agent)

	messages := []api.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Response"},
		{Role: "user", Content: "Follow-up"},
	}
	
	result := handler.stripConsecutiveAssistantMessages(messages)
	
	if len(result) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(result))
	}
}

// TestStripConsecutiveAssistantMessages_AllAssistant tests edge case where all
// messages are assistant without tool_calls
func TestStripConsecutiveAssistantMessages_AllAssistant(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	
	handler := NewConversationHandler(agent)

	messages := []api.Message{
		{Role: "assistant", Content: "First"},
		{Role: "assistant", Content: "Second"},
		{Role: "assistant", Content: "Third"},
	}
	
	result := handler.stripConsecutiveAssistantMessages(messages)
	
	// Should keep only the last assistant message
	if len(result) != 1 {
		t.Errorf("Expected 1 message, got %d", len(result))
	}
	if result[0].Content != "Third" {
		t.Errorf("Expected last assistant message, got %q", result[0].Content)
	}
}

// TestStripConsecutiveAssistantMessages_SingleMessage tests edge case with single message
func TestStripConsecutiveAssistantMessages_SingleMessage(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	
	handler := NewConversationHandler(agent)

	messages := []api.Message{
		{Role: "assistant", Content: "Only message"},
	}
	
	result := handler.stripConsecutiveAssistantMessages(messages)
	
	if len(result) != 1 {
		t.Errorf("Expected 1 message, got %d", len(result))
	}
}

// TestStripConsecutiveAssistantMessages_Empty tests edge case with empty list
func TestStripConsecutiveAssistantMessages_Empty(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	
	handler := NewConversationHandler(agent)

	messages := []api.Message{}
	
	result := handler.stripConsecutiveAssistantMessages(messages)
	
	if len(result) != 0 {
		t.Errorf("Expected 0 messages, got %d", len(result))
	}
}

// TestStripConsecutiveAssistantMessages_WithToolMessages tests interleaved tool messages
func TestStripConsecutiveAssistantMessages_WithToolMessages(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	
	handler := NewConversationHandler(agent)

	// Test case: assistant with tool_calls followed by tool response, then consecutive assistants
	messages := []api.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", ToolCalls: []api.ToolCall{{ID: "call1", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "test", Arguments: ""}}}},
		{Role: "tool", Content: "tool result", ToolCallId: "call1"},
		{Role: "assistant", Content: "Response A"},
		{Role: "assistant", Content: "Response B"},
	}
	
	result := handler.stripConsecutiveAssistantMessages(messages)
	
	// Should keep messages 0-2 (user, assistant with tool_calls, tool) and last assistant (B)
	if len(result) != 4 {
		t.Errorf("Expected 4 messages, got %d", len(result))
	}
	
	// Verify the last message is "Response B"
	if result[3].Content != "Response B" {
		t.Errorf("Expected last message to be 'Response B', got %q", result[3].Content)
	}
}

// TestStripConsecutiveAssistantMessages_ToolBetweenAssistants tests tool message between assistants
func TestStripConsecutiveAssistantMessages_ToolBetweenAssistants(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	
	handler := NewConversationHandler(agent)

	// Test case: tool message between two assistants - should not be affected
	messages := []api.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Response A"},
		{Role: "tool", Content: "tool result", ToolCallId: "call1"},
		{Role: "assistant", Content: "Response B"},
	}
	
	result := handler.stripConsecutiveAssistantMessages(messages)
	
	// Tool message breaks the consecutive pattern, so no changes
	if len(result) != 4 {
		t.Errorf("Expected 4 messages, got %d", len(result))
	}
}

// TestStripConsecutiveAssistantMessages_AssistantWithToolCallsAtEnd tests assistant with tool_calls at end
func TestStripConsecutiveAssistantMessages_AssistantWithToolCallsAtEnd(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	
	handler := NewConversationHandler(agent)

	// Test case: assistant with tool_calls at end needs tool responses
	messages := []api.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", ToolCalls: []api.ToolCall{{ID: "call1", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "test", Arguments: ""}}}},
	}
	
	result := handler.stripConsecutiveAssistantMessages(messages)
	
	// Should keep all messages - tool_calls need tool responses
	if len(result) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result))
	}
	
	if len(result[1].ToolCalls) == 0 {
		t.Errorf("Expected last message to have tool_calls")
	}
}
