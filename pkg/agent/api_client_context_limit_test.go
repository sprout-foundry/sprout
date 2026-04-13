package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestIsContextLimitError(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		expected bool
	}{
		{
			name:     "context window exceeds",
			errMsg:   "context window exceeds limit (2013)",
			expected: true,
		},
		{
			name:     "context window over",
			errMsg:   "context window over limit",
			expected: true,
		},
		{
			name:     "context limit in error",
			errMsg:   "context_limit exceeded",
			expected: true,
		},
		{
			name:     "context exceeds",
			errMsg:   "context exceeds max",
			expected: true,
		},
		{
			name:     "max context",
			errMsg:   "max context tokens exceeded",
			expected: true,
		},
		{
			name:     "token limit exceeded",
			errMsg:   "token limit exceeded",
			expected: true,
		},
		{
			name:     "generic error",
			errMsg:   "something went wrong",
			expected: false,
		},
		{
			name:     "rate limit error",
			errMsg:   "rate limit exceeded",
			expected: false,
		},
		{
			name:     "nil error",
			errMsg:   "",
			expected: false,
		},
	}

	ac := &APIClient{agent: &Agent{}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.errMsg != "" {
				err = &testError{message: tc.errMsg}
			}
			result := ac.isContextLimitError(err)
			if result != tc.expected {
				t.Errorf("isContextLimitError(%q) = %v, want %v", tc.errMsg, result, tc.expected)
			}
		})
	}
}

type testError struct {
	message string
}

func (e *testError) Error() string {
	return e.message
}

func TestTriggerCompactionNil(t *testing.T) {
	// Test nil agent
	var agent *Agent
	result := agent.TriggerCompaction()
	if result != false {
		t.Errorf("TriggerCompaction() on nil = %v, want false", result)
	}
}

func TestTriggerCompactionWithCheckpoints(t *testing.T) {
	// Test with checkpoint - should apply checkpoint compaction
	messages := []api.Message{
		{Role: "user", Content: "First request"},
		{Role: "assistant", Content: "Long response content here that gets compacted"},
		{Role: "user", Content: "Second request"},
	}
	checkpoints := []TurnCheckpoint{{
		StartIndex: 0,
		EndIndex:   1,
		Summary:   "Compacted summary",
	}}

	a := &Agent{
		messages:      messages,
		turnCheckpoints: checkpoints,
		debug:        false,
	}

	result := a.TriggerCompaction()
	if result != true {
		t.Errorf("TriggerCompaction() with checkpoint = %v, want true", result)
	}

	// Verify messages were compacted
	if len(a.messages) >= len(messages) {
		t.Errorf("messages not compacted: before=%d, after=%d", len(messages), len(a.messages))
	}
}

func TestTriggerCompactionEmergencyTruncation(t *testing.T) {
	// Test emergency truncation when no checkpoints or optimizer
	// Need more than 3 messages to trigger emergency truncation
	messages := []api.Message{
		{Role: "user", Content: "First"},
		{Role: "assistant", Content: "Response 1"},
		{Role: "user", Content: "Second"},
		{Role: "assistant", Content: "Response 2"},
		{Role: "user", Content: "Third"},
		{Role: "assistant", Content: "Response 3"},
	}

	a := &Agent{
		messages: messages,
		debug:   false,
		// No checkpoints, no optimizer - should trigger emergency truncation
	}

	result := a.TriggerCompaction()
	if result != true {
		t.Errorf("TriggerCompaction() emergency = %v, want true", result)
	}

	// Verify messages were truncated (should keep system + last 2)
	if len(a.messages) > 3 {
		t.Errorf("messages not truncated: got %d, want <=3", len(a.messages))
	}
}