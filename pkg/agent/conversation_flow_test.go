package agent

import (
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"testing"
)

func TestBlankIterationDetection(t *testing.T) {
	// Test the method directly without needing a full agent initialization
	ch := &ConversationHandler{}

	tests := []struct {
		name      string
		content   string
		toolCalls []api.ToolCall
		expected  bool
	}{
		{
			name:      "Empty content",
			content:   "",
			toolCalls: []api.ToolCall{},
			expected:  true,
		},
		{
			name:      "Whitespace only",
			content:   "   ",
			toolCalls: []api.ToolCall{},
			expected:  true,
		},
		{
			name:      "Single character",
			content:   "a",
			toolCalls: []api.ToolCall{},
			expected:  true,
		},
		{
			name:      "Two characters - letters",
			content:   "OK",
			toolCalls: []api.ToolCall{},
			expected:  false,
		},
		{
			name:      "Three characters - letters",
			content:   "Yes",
			toolCalls: []api.ToolCall{},
			expected:  false,
		},
		{
			name:      "Two characters - punctuation",
			content:   "..",
			toolCalls: []api.ToolCall{},
			expected:  true,
		},
		{
			name:      "Three characters - punctuation",
			content:   "...",
			toolCalls: []api.ToolCall{},
			expected:  true,
		},
		{
			name:      "With tool calls",
			content:   "",
			toolCalls: []api.ToolCall{{ID: "test"}},
			expected:  false,
		},
		{
			name:      "Normal response",
			content:   "This is a normal response",
			toolCalls: []api.ToolCall{},
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ch.isBlankIteration(tt.content, tt.toolCalls)
			if result != tt.expected {
				t.Errorf("isBlankIteration() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestRepetitiveContentDetection(t *testing.T) {
	// Test the method directly without needing a full agent initialization
	ch := &ConversationHandler{}

	tests := []struct {
		name            string
		content         string
		existingContent string
		expected        bool
	}{
		{
			name:            "Specific problematic pattern",
			content:         "let me check for any simple improvements",
			existingContent: "",
			expected:        true,
		},
		{
			name:            "Another problematic pattern",
			content:         "let me look for any obvious issues:",
			existingContent: "",
			expected:        true,
		},
		{
			name:            "Legitimate analysis phrase",
			content:         "let me examine the code structure",
			existingContent: "",
			expected:        false,
		},
		{
			name:            "Legitimate analysis phrase 2",
			content:         "let me analyze the function",
			existingContent: "",
			expected:        false,
		},
		{
			name:            "Normal response",
			content:         "I found the issue in the main function",
			existingContent: "",
			expected:        false,
		},
		{
			name:            "Exact duplicate",
			content:         "Previous message",
			existingContent: "Previous message",
			expected:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the agent with the existing messages
			agent := &Agent{
				messages: []api.Message{},
			}

			// Add a user message first (required for the logic to work)
			agent.messages = append(agent.messages, api.Message{
				Role:    "user",
				Content: "Some user message",
			})

			// Add existing assistant message if provided
			if tt.existingContent != "" {
				agent.messages = append(agent.messages, api.Message{
					Role:    "assistant",
					Content: tt.existingContent,
				})
			}

			// Add the current message to simulate the actual flow
			// (this is what happens in processResponse before isRepetitiveContent is called)
			agent.messages = append(agent.messages, api.Message{
				Role:    "assistant",
				Content: tt.content,
			})

			ch.agent = agent
			result := ch.isRepetitiveContent(tt.content)
			if result != tt.expected {
				t.Errorf("isRepetitiveContent() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func containsStringForTest(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			findSubstringForTest(s, substr)))
}

func findSubstringForTest(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
