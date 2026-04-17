package agent

import (
	"errors"
	"fmt"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// TestIsPrefillIncompatibilityError tests the isPrefillIncompatibilityError method
// which detects errors related to prefill incompatibility with thinking mode.
func TestIsPrefillIncompatibilityError(t *testing.T) {
	// Create a minimal APIClient for testing
	ac := &APIClient{agent: &Agent{}}

	tests := []struct {
		name      string
		err       error
		wantMatch bool
	}{
		{
			name:      "nil error returns false",
			err:       nil,
			wantMatch: false,
		},
		{
			name:      "full error message with both keywords returns true",
			err:       errors.New("Assistant response prefill is incompatible with enable_thinking"),
			wantMatch: true,
		},
		{
			name:      "error containing 'prefill' returns true",
			err:       errors.New("some prefill error occurred"),
			wantMatch: true,
		},
		{
			name:      "error containing 'enable_thinking' returns true",
			err:       errors.New("invalid parameter enable_thinking"),
			wantMatch: true,
		},
		{
			name:      "error with both keywords in different order returns true",
			err:       errors.New("enable_thinking cannot be used with prefill"),
			wantMatch: true,
		},
		{
			name:      "error with 'PREFILL' in uppercase returns false (case-sensitive)",
			err:       errors.New("PREFILL is not supported"),
			wantMatch: false,
		},
		{
			name:      "error with 'ENABLE_THINKING' in uppercase returns false (case-sensitive)",
			err:       errors.New("ENABLE_THINKING failed"),
			wantMatch: false,
		},
		{
			name:      "generic error without keywords returns false",
			err:       errors.New("some other error"),
			wantMatch: false,
		},
		{
			name:      "context limit error returns false",
			err:       errors.New("context window exceeds limit"),
			wantMatch: false,
		},
		{
			name:      "rate limit error returns false",
			err:       errors.New("rate limit exceeded"),
			wantMatch: false,
		},
		{
			name:      "network error returns false",
			err:       errors.New("connection timeout"),
			wantMatch: false,
		},
		{
			name:      "empty error returns false",
			err:       errors.New(""),
			wantMatch: false,
		},
		{
			name:      "wrapped error with prefill returns true",
			err:       fmt.Errorf("wrapped: %w", errors.New("prefill not allowed")),
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ac.isPrefillIncompatibilityError(tt.err)
			if got != tt.wantMatch {
				t.Errorf("isPrefillIncompatibilityError(%v) = %v, want %v", tt.err, got, tt.wantMatch)
			}
		})
	}
}

// TestStripLeadingAssistantPrefillFromMessages tests the stripLeadingAssistantPrefillFromMessages
// standalone function which removes leading assistant messages (compaction summaries)
// that appear immediately after system messages.
func TestStripLeadingAssistantPrefillFromMessages(t *testing.T) {
	tests := []struct {
		name          string
		inputMessages []api.Message
		expectedLen   int
		expectedRoles []string
		description   string
	}{
		{
			name:          "empty messages returns as-is",
			inputMessages: []api.Message{},
			expectedLen:   0,
			expectedRoles: []string{},
			description:   "empty slice should remain empty",
		},
		{
			name: "only system messages returns as-is",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are helpful"},
				{Role: "system", Content: "More context"},
			},
			expectedLen:   2,
			expectedRoles: []string{"system", "system"},
			description:   "no non-system messages to strip",
		},
		{
			name: "system followed by user returns as-is",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are helpful"},
				{Role: "user", Content: "Hello"},
			},
			expectedLen:   2,
			expectedRoles: []string{"system", "user"},
			description:   "first non-system is user, nothing to strip",
		},
		{
			name: "system, assistant, user strips the assistant",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are helpful"},
				{Role: "assistant", Content: "[Context compaction summary] Previous discussion..."},
				{Role: "user", Content: "What next?"},
			},
			expectedLen:   2,
			expectedRoles: []string{"system", "user"},
			description:   "strips leading assistant without tool_calls",
		},
		{
			name: "system, two assistants, user strips both assistants",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are helpful"},
				{Role: "assistant", Content: "[Summary 1] First part..."},
				{Role: "assistant", Content: "[Summary 2] Second part..."},
				{Role: "user", Content: "Continue"},
			},
			expectedLen:   2,
			expectedRoles: []string{"system", "user"},
			description:   "strips multiple consecutive leading assistants",
		},
		{
			name: "system, assistant with tool_calls, user preserves assistant",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are helpful"},
				{
					Role:    "assistant",
					Content: "Let me check the file",
					ToolCalls: []api.ToolCall{{
						ID: "call_123",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "read_file", Arguments: "{}"},
					}},
				},
				{Role: "user", Content: "What did you find?"},
			},
			expectedLen:   3,
			expectedRoles: []string{"system", "assistant", "user"},
			description:   "assistant with tool_calls is not stripped",
		},
		{
			name: "system, assistant content, assistant with tool_calls, user strips only content assistant",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are helpful"},
				{Role: "assistant", Content: "[Summary] Previous context..."},
				{
					Role:    "assistant",
					Content: "Let me check the file",
					ToolCalls: []api.ToolCall{{
						ID: "call_456",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "shell", Arguments: "{}"},
					}},
				},
				{Role: "user", Content: "Results?"},
			},
			expectedLen:   3,
			expectedRoles: []string{"system", "assistant", "user"},
			description:   "strips only content-only assistant, preserves tool_call assistant",
		},
		{
			name: "assistant without system strips the assistant",
			inputMessages: []api.Message{
				{Role: "assistant", Content: "[Summary]"},
			},
			expectedLen:   0,
			expectedRoles: []string{},
			description:   "leading assistant with no system messages gets stripped",
		},
		{
			name: "multiple system messages then assistant strips assistant",
			inputMessages: []api.Message{
				{Role: "system", Content: "System prompt"},
				{Role: "system", Content: "Additional context"},
				{Role: "system", Content: "More context"},
				{Role: "assistant", Content: "[Summary]"},
				{Role: "user", Content: "Question?"},
			},
			expectedLen:   4,
			expectedRoles: []string{"system", "system", "system", "user"},
			description:   "skips all leading system messages before stripping",
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
			description:   "empty tool_calls slice means no actual tool calls",
		},
		{
			name: "assistant with nil tool_calls is stripped",
			inputMessages: []api.Message{
				{Role: "system", Content: "System prompt"},
				{Role: "assistant", Content: "[Summary]", ToolCalls: nil},
				{Role: "user", Content: "Question?"},
			},
			expectedLen:   2,
			expectedRoles: []string{"system", "user"},
			description:   "nil tool_calls means no actual tool calls",
		},
		{
			name: "system, assistant, user, assistant preserves non-leading assistant",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are helpful"},
				{Role: "assistant", Content: "[Summary]"},
				{Role: "user", Content: "Question?"},
				{Role: "assistant", Content: "Answer"},
			},
			expectedLen:   3,
			expectedRoles: []string{"system", "user", "assistant"},
			description:   "non-leading assistant after user is preserved",
		},
		{
			name: "full conversation with multiple turns after stripping",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are helpful"},
				{Role: "assistant", Content: "[Summary 1]"},
				{Role: "assistant", Content: "[Summary 2]"},
				{Role: "user", Content: "First question"},
				{Role: "assistant", Content: "Answer 1"},
				{Role: "user", Content: "Second question"},
				{Role: "assistant", Content: "Answer 2"},
			},
			expectedLen:   5,
			expectedRoles: []string{"system", "user", "assistant", "user", "assistant"},
			description:   "complex conversation with multiple assistant summaries stripped",
		},
		{
			name: "single system message followed by assistant",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are helpful"},
				{Role: "assistant", Content: "[Summary]"},
			},
			expectedLen:   1,
			expectedRoles: []string{"system"},
			description:   "assistant with no following messages still stripped",
		},
		{
			name: "assistant with empty content but no tool_calls is stripped",
			inputMessages: []api.Message{
				{Role: "system", Content: "You are helpful"},
				{Role: "assistant", Content: ""},
				{Role: "user", Content: "Hello"},
			},
			expectedLen:   2,
			expectedRoles: []string{"system", "user"},
			description:   "empty content assistant still stripped if no tool_calls",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripLeadingAssistantPrefillFromMessages(tt.inputMessages)

			// Check length
			if len(result) != tt.expectedLen {
				t.Errorf("expected %d messages, got %d", tt.expectedLen, len(result))
				t.Logf("Test case: %s", tt.description)
				for i, msg := range result {
					t.Logf("  result[%d]: role=%s, content=%q, toolCalls=%d",
						i, msg.Role, msg.Content, len(msg.ToolCalls))
				}
			}

			// Check roles
			if len(result) != len(tt.expectedRoles) {
				t.Fatalf("role check skipped due to length mismatch: expected %d roles, got %d",
					len(tt.expectedRoles), len(result))
			}

			for i, expectedRole := range tt.expectedRoles {
				if result[i].Role != expectedRole {
					t.Errorf("result[%d].Role = %q, want %q", i, result[i].Role, expectedRole)
				}
			}
		})
	}
}

// TestStripLeadingAssistantPrefillFromMessages_NoCrash ensures the function
// handles edge cases without panicking.
func TestStripLeadingAssistantPrefillFromMessages_NoCrash(t *testing.T) {
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
					ToolCalls: []api.ToolCall{{
						ID: "call_1",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "test", Arguments: "{}"},
					}},
				},
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

			result := stripLeadingAssistantPrefillFromMessages(tt.input)
			// For nil input, result should be nil
			if tt.input == nil && result != nil {
				t.Errorf("expected nil result for nil input, got %v", result)
			}
		})
	}
}

// TestStripLeadingAssistantPrefillFromMessages_PreservesOriginal verifies that
// the function does not modify the original message slice.
func TestStripLeadingAssistantPrefillFromMessages_PreservesOriginal(t *testing.T) {
	original := []api.Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "assistant", Content: "[Summary]"},
		{Role: "user", Content: "Question?"},
	}

	// Make a copy to compare later
	originalCopy := make([]api.Message, len(original))
	copy(originalCopy, original)

	result := stripLeadingAssistantPrefillFromMessages(original)

	// Verify original was not modified
	if len(original) != len(originalCopy) {
		t.Errorf("original slice was modified: expected length %d, got %d",
			len(originalCopy), len(original))
	}

	for i := range original {
		if original[i].Role != originalCopy[i].Role {
			t.Errorf("original[%d].Role was modified: expected %q, got %q",
				i, originalCopy[i].Role, original[i].Role)
		}
		if original[i].Content != originalCopy[i].Content {
			t.Errorf("original[%d].Content was modified: expected %q, got %q",
				i, originalCopy[i].Content, original[i].Content)
		}
	}

	// Verify result is different from original
	if len(result) == len(original) {
		t.Errorf("expected result to have different length than original")
	}
}

// TestStripLeadingAssistantPrefillFromMessages_ToolCallDetails verifies that
// tool call details are preserved when an assistant with tool_calls is kept.
func TestStripLeadingAssistantPrefillFromMessages_ToolCallDetails(t *testing.T) {
	input := []api.Message{
		{Role: "system", Content: "You are helpful"},
		{
			Role:    "assistant",
			Content: "Checking file",
			ToolCalls: []api.ToolCall{
				{
					ID: "call_123",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "read_file", Arguments: `{"path":"test.go"}`},
				},
				{
					ID: "call_456",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "list_files", Arguments: `{"dir":"."}`},
				},
			},
		},
		{Role: "user", Content: "Results?"},
	}

	result := stripLeadingAssistantPrefillFromMessages(input)

	// Should preserve the assistant with tool_calls
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}

	if result[1].Role != "assistant" {
		t.Errorf("result[1].Role = %q, want assistant", result[1].Role)
	}

	// Verify tool calls are preserved
	if len(result[1].ToolCalls) != 2 {
		t.Errorf("expected 2 tool calls, got %d", len(result[1].ToolCalls))
	}

	if result[1].ToolCalls[0].ID != "call_123" {
		t.Errorf("tool call 0 ID = %q, want call_123", result[1].ToolCalls[0].ID)
	}

	if result[1].ToolCalls[1].Function.Name != "list_files" {
		t.Errorf("tool call 1 function name = %q, want list_files",
			result[1].ToolCalls[1].Function.Name)
	}
}
