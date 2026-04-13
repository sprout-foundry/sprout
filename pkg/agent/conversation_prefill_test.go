package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// TestStripLeadingAssistantPrefill tests the stripLeadingAssistantPrefill method
// which removes leading assistant messages (compaction summaries) that appear
// immediately after the system prompt. Some providers with thinking/reasoning mode
// enabled reject messages where the first non-system message has role "assistant".
func TestStripLeadingAssistantPrefill(t *testing.T) {
	// Helper function to create a minimal ConversationHandler for testing
	createTestHandler := func(messages []api.Message, debug bool) *ConversationHandler {
		agent := &Agent{
			messages: messages,
			debug:    debug,
		}
		return NewConversationHandler(agent)
	}

	tests := []struct {
		name          string
		inputMessages []api.Message
		expectedLen   int
		expectedRoles []string
		debugEnabled  bool
		wantStripped  bool
	}{
		{
			name: "leading assistant summary is removed (main fix case)",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are a helpful assistant"},
				{Role: "assistant", Content: "[Context compaction summary] I've reviewed the previous conversation..."},
				{Role: "user", Content: "What should I do next?"},
				{Role: "assistant", Content: "You should review the code changes"},
			},
			expectedLen:   3,
			expectedRoles: []string{"system", "user", "assistant"},
			debugEnabled:  true,
			wantStripped:  true,
		},
		{
			name: "leading assistant with tool_calls is NOT removed (part of tool flow)",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are a helpful assistant"},
				{
					Role:    "assistant",
					Content: "Let me check the file",
					ToolCalls: []api.ToolCall{{ID: "call_123", Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "read_file", Arguments: `{}`}}},
				},
				{Role: "tool", Content: "file contents"},
				{Role: "user", Content: "What did you find?"},
			},
			expectedLen:   4,
			expectedRoles: []string{"system", "assistant", "tool", "user"},
			debugEnabled:  false,
			wantStripped:  false,
		},
		{
			name: "leading user message is preserved as-is",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are a helpful assistant"},
				{Role: "user", Content: "Hello, how are you?"},
				{Role: "assistant", Content: "I'm doing well!"},
			},
			expectedLen:   3,
			expectedRoles: []string{"system", "user", "assistant"},
			debugEnabled:  false,
			wantStripped:  false,
		},
		{
			name: "only system messages returns unchanged",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are a helpful assistant"},
				{Role: "system", Content: "Additional context"},
			},
			expectedLen:   2,
			expectedRoles: []string{"system", "system"},
			debugEnabled:  false,
			wantStripped:  false,
		},
		{
			name:          "empty messages returns empty",
			inputMessages: []api.Message{},
			expectedLen:   0,
			expectedRoles: []string{},
			debugEnabled:  false,
			wantStripped:  false,
		},
		{
			name: "multiple leading assistant summaries all stripped",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are a helpful assistant"},
				{Role: "assistant", Content: "[Summary 1] Previous context..."},
				{Role: "assistant", Content: "[Summary 2] More context..."},
				{Role: "assistant", Content: "[Summary 3] Even more..."},
				{Role: "user", Content: "What's the current state?"},
			},
			expectedLen:   2,
			expectedRoles: []string{"system", "user"},
			debugEnabled:  true,
			wantStripped:  true,
		},
		{
			name: "system + assistant with no content is stripped",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are a helpful assistant"},
				{Role: "assistant", Content: ""},
				{Role: "user", Content: "Let's begin"},
			},
			expectedLen:   2,
			expectedRoles: []string{"system", "user"},
			debugEnabled:  true,
			wantStripped:  true,
		},
		{
			name: "assistant with tool_calls followed by tool response preserves assistant",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are a helpful assistant"},
				{
					Role:    "assistant",
					Content: "I'll call a tool",
					ToolCalls: []api.ToolCall{{ID: "call_abc", Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "shell", Arguments: `{}`}}},
				},
				{Role: "tool", Content: "output", ToolCallId: "call_abc"},
				{Role: "assistant", Content: "The tool completed"},
			},
			expectedLen:   4,
			expectedRoles: []string{"system", "assistant", "tool", "assistant"},
			debugEnabled:  false,
			wantStripped:  false,
		},
		{
			name: "mixed system + assistant + assistant + user strips assistants",
			inputMessages: []api.Message{
				{Role: "system", Content: "System prompt"},
				{Role: "assistant", Content: "[Summary A]"},
				{Role: "assistant", Content: "[Summary B]"},
				{Role: "user", Content: "Question?"},
			},
			expectedLen:   2,
			expectedRoles: []string{"system", "user"},
			debugEnabled:  true,
			wantStripped:  true,
		},
		{
			name: "assistant with empty tool_calls slice is stripped",
			inputMessages: []api.Message{
				{Role: "system", Content: "System prompt"},
				{Role: "assistant", Content: "[Summary]", ToolCalls: []api.ToolCall{}},
				{Role: "user", Content: "Question?"},
			},
			expectedLen:   2,
			expectedRoles: []string{"system", "user"},
			debugEnabled:  true,
			wantStripped:  true,
		},
		{
			name: "assistant with nil tool_calls slice is stripped",
			inputMessages: []api.Message{
				{Role: "system", Content: "System prompt"},
				{Role: "assistant", Content: "[Summary]", ToolCalls: nil},
				{Role: "user", Content: "Question?"},
			},
			expectedLen:   2,
			expectedRoles: []string{"system", "user"},
			debugEnabled:  true,
			wantStripped:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := createTestHandler(tt.inputMessages, tt.debugEnabled)

			result := handler.stripLeadingAssistantPrefill(tt.inputMessages)

			if len(result) != tt.expectedLen {
				t.Errorf("expected %d messages, got %d", tt.expectedLen, len(result))
				for i, msg := range result {
					t.Logf("  result[%d]: role=%s, content=%q", i, msg.Role, msg.Content)
				}
			}

			if len(result) != len(tt.expectedRoles) {
				t.Fatalf("role check skipped due to length mismatch")
			}

			for i, expectedRole := range tt.expectedRoles {
				if i >= len(result) {
					t.Errorf("missing expected role at index %d: %s", i, expectedRole)
					continue
				}
				if result[i].Role != expectedRole {
					t.Errorf("result[%d].Role = %q, want %q", i, result[i].Role, expectedRole)
				}
			}

			// Verify that stripped messages are not in the result
			if tt.wantStripped {
				// Find first non-system message in result
				firstNonSystemIdx := -1
				for i, msg := range result {
					if msg.Role != "system" {
						firstNonSystemIdx = i
						break
					}
				}

				if firstNonSystemIdx >= 0 && result[firstNonSystemIdx].Role == "assistant" {
					t.Errorf("expected leading assistant to be stripped, but found assistant at index %d", firstNonSystemIdx)
				}
			}
		})
	}
}

// TestStripLeadingAssistantPrefill_WithDebugEnabled verifies correct behavior
// when debug mode is enabled (including debug log emission).
func TestStripLeadingAssistantPrefill_WithDebugEnabled(t *testing.T) {
	agent := &Agent{
		messages: []api.Message{
			{Role: "system", Content: "System prompt"},
			{Role: "assistant", Content: "[Summary]"},
			{Role: "user", Content: "Question?"},
		},
		debug: true,
	}

	handler := NewConversationHandler(agent)

	result := handler.stripLeadingAssistantPrefill(agent.messages)

	// Verify the result is correct (assistant summary should be stripped)
	if len(result) != 2 {
		t.Errorf("expected 2 messages after stripping, got %d", len(result))
	}

	if result[0].Role != "system" || result[1].Role != "user" {
		t.Errorf("expected roles [system, user], got [%s, %s]", result[0].Role, result[1].Role)
	}
}

// TestStripLeadingAssistantPrefill_NoCrashOnEdgeCases ensures the function
// handles edge cases without panicking.
func TestStripLeadingAssistantPrefill_NoCrashOnEdgeCases(t *testing.T) {
	handler := NewConversationHandler(&Agent{debug: false})

	// Test with nil agent
	nilAgentHandler := &ConversationHandler{}

	tests := []struct {
		name  string
		input []api.Message
	}{
		{"nil input", nil},
		{"empty slice", []api.Message{}},
		{"single system message", []api.Message{{Role: "system", Content: "prompt"}}},
		{"single assistant summary", []api.Message{{Role: "assistant", Content: "[summary]"}}},
		{
			name: "single assistant with tool_calls",
			input: []api.Message{
				{
					Role:    "assistant",
					Content: "calling tool",
					ToolCalls: []api.ToolCall{{ID: "call_1", Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "test", Arguments: `{}`}}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("function panicked: %v", r)
				}
			}()

			result := handler.stripLeadingAssistantPrefill(tt.input)
			if result == nil && tt.input != nil {
				t.Errorf("expected non-nil result for non-nil input")
			}

			// Also test with nil agent handler
			nilResult := nilAgentHandler.stripLeadingAssistantPrefill(tt.input)
			if nilResult == nil && tt.input != nil {
				t.Errorf("nil agent handler: expected non-nil result for non-nil input")
			}
		})
	}
}

// TestStripLeadingAssistantPrefill_PreservesNonLeadingAssistants ensures that
// assistant messages after the first non-assistant are preserved.
func TestStripLeadingAssistantPrefill_PreservesNonLeadingAssistants(t *testing.T) {
	handler := NewConversationHandler(&Agent{debug: false})

	input := []api.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "assistant", Content: "[Summary]"},
		{Role: "user", Content: "Question?"},
		{Role: "assistant", Content: "Answer 1"},
		{Role: "user", Content: "Follow-up?"},
		{Role: "assistant", Content: "Answer 2"},
	}

	result := handler.stripLeadingAssistantPrefill(input)

	// Should have: system, user, assistant, user, assistant (5 messages)
	if len(result) != 5 {
		t.Errorf("expected 5 messages, got %d", len(result))
	}

	// Verify the non-leading assistants are preserved
	expectedSequence := []string{"system", "user", "assistant", "user", "assistant"}
	for i, expectedRole := range expectedSequence {
		if result[i].Role != expectedRole {
			t.Errorf("result[%d].Role = %q, want %q", i, result[i].Role, expectedRole)
		}
	}
}
