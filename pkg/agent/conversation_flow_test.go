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

func TestHandleFinishReason_StopWithEmptyContentContinues(t *testing.T) {
	ch := &ConversationHandler{
		agent: &Agent{},
	}

	shouldStop, reason := ch.handleFinishReason("stop", "   ")
	if shouldStop {
		t.Fatalf("expected empty stop response to continue, got stop=true (reason=%q)", reason)
	}
	if reason != "empty stop response" {
		t.Fatalf("expected reason 'empty stop response', got %q", reason)
	}
	if len(ch.transientMessages) != 1 {
		t.Fatalf("expected one transient continuation message, got %d", len(ch.transientMessages))
	}
	if ch.transientMessages[0].Role != "user" {
		t.Fatalf("expected transient message role user, got %q", ch.transientMessages[0].Role)
	}
	if !containsStringForTest(ch.transientMessages[0].Content, "Please continue") {
		t.Fatalf("expected continuation prompt, got %q", ch.transientMessages[0].Content)
	}
}

func TestHandleFinishReason_StopWithIncompleteContentContinues(t *testing.T) {
	agent := &Agent{}
	ch := &ConversationHandler{
		agent:             agent,
		responseValidator: NewResponseValidator(agent),
	}

	shouldStop, reason := ch.handleFinishReason("stop", "I checked.")
	if shouldStop {
		t.Fatalf("expected incomplete stop response to continue, got stop=true (reason=%q)", reason)
	}
	if reason != "incomplete stop response" {
		t.Fatalf("expected reason 'incomplete stop response', got %q", reason)
	}
	if len(ch.transientMessages) != 1 {
		t.Fatalf("expected one transient message, got %d", len(ch.transientMessages))
	}
	if !containsStringForTest(ch.transientMessages[0].Content, "appears incomplete") {
		t.Fatalf("expected incomplete-response nudge, got %q", ch.transientMessages[0].Content)
	}
}

func TestHandleFinishReason_StopWithTentativePostToolContentContinues(t *testing.T) {
	agent := &Agent{
		messages: []api.Message{
			{Role: "user", Content: "Investigate the issue"},
			{Role: "assistant", Content: "Let me inspect the logs first.", ToolCalls: []api.ToolCall{{ID: "call_1"}}},
			{Role: "tool", ToolCallId: "call_1", Content: "log output"},
			{Role: "assistant", Content: "Let me investigate the issue by checking the backend logs and testing directly:"},
		},
	}
	ch := &ConversationHandler{
		agent:             agent,
		responseValidator: NewResponseValidator(agent),
	}

	shouldStop, reason := ch.handleFinishReason("stop", "Let me investigate the issue by checking the backend logs and testing directly:")
	if shouldStop {
		t.Fatalf("expected tentative post-tool stop response to continue, got stop=true (reason=%q)", reason)
	}
	if reason != "tentative post-tool stop response" {
		t.Fatalf("expected reason 'tentative post-tool stop response', got %q", reason)
	}
	if len(ch.transientMessages) != 1 {
		t.Fatalf("expected one transient message, got %d", len(ch.transientMessages))
	}
	if !containsStringForTest(ch.transientMessages[0].Content, "Do not stop with a planning note") {
		t.Fatalf("expected post-tool continuation nudge, got %q", ch.transientMessages[0].Content)
	}
}

func TestHandleFinishReason_StopWithAcknowledgedNextStepPostToolContentContinues(t *testing.T) {
	agent := &Agent{
		messages: []api.Message{
			{Role: "user", Content: "Add validation events"},
			{Role: "assistant", Content: "I will inspect the events package.", ToolCalls: []api.ToolCall{{ID: "call_1"}}},
			{Role: "tool", ToolCallId: "call_1", Content: "events constants already exist"},
			{Role: "assistant", Content: "Good, the constant already exists. Now I need to add the ValidationEvent struct type and helper functions to the events package:"},
		},
	}
	ch := &ConversationHandler{
		agent:             agent,
		responseValidator: NewResponseValidator(agent),
	}

	shouldStop, reason := ch.handleFinishReason("stop", "Good, the constant already exists. Now I need to add the ValidationEvent struct type and helper functions to the events package:")
	if shouldStop {
		t.Fatalf("expected acknowledged next-step post-tool response to continue, got stop=true (reason=%q)", reason)
	}
	if reason != "tentative post-tool stop response" {
		t.Fatalf("expected reason 'tentative post-tool stop response', got %q", reason)
	}
	if len(ch.transientMessages) != 1 {
		t.Fatalf("expected one transient message, got %d", len(ch.transientMessages))
	}
}

func TestHandleFinishReason_StopWithConcretePostToolAnswerCompletes(t *testing.T) {
	agent := &Agent{
		messages: []api.Message{
			{Role: "user", Content: "Investigate the issue"},
			{Role: "assistant", Content: "Checking logs.", ToolCalls: []api.ToolCall{{ID: "call_1"}}},
			{Role: "tool", ToolCallId: "call_1", Content: "panic on startup"},
			{Role: "assistant", Content: "I found the root cause and updated the startup guard to handle nil config safely."},
		},
	}
	ch := &ConversationHandler{
		agent:             agent,
		responseValidator: NewResponseValidator(agent),
	}

	shouldStop, reason := ch.handleFinishReason("stop", "I found the root cause and updated the startup guard to handle nil config safely.")
	if !shouldStop {
		t.Fatalf("expected concrete post-tool stop response to complete, got stop=false (reason=%q)", reason)
	}
	if reason != "completion" {
		t.Fatalf("expected completion reason, got %q", reason)
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
