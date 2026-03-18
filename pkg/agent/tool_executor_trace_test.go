package agent

import (
	"context"
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/trace"
)

// MockTraceSession is a mock implementation for testing
type MockTraceSession struct {
	GetRunIDFunc       func() string
	RecordToolCallFunc func(record interface{}) error
}

func (m *MockTraceSession) GetRunID() string {
	if m.GetRunIDFunc != nil {
		return m.GetRunIDFunc()
	}
	return "test-run-id"
}

func (m *MockTraceSession) RecordToolCall(record interface{}) error {
	if m.RecordToolCallFunc != nil {
		return m.RecordToolCallFunc(record)
	}
	return nil
}

func TestRecordToolExecution(t *testing.T) {
	// Create a test agent
	agent := &Agent{
		currentIteration: 1,
	}

	// Create tool executor
	te := NewToolExecutor(agent)

	// Test with nil trace session (should not crash)
	te.recordToolExecutionWithIndex("test_tool", "{}", nil, "full result", "model result", nil, 0)

	// Test with mock trace session - success case
	mockTrace := &MockTraceSession{
		GetRunIDFunc: func() string {
			return "mock-run-id"
		},
		RecordToolCallFunc: func(record interface{}) error {
			rec := record.(trace.ToolCallRecord)
			if rec.RunID != "mock-run-id" {
				t.Errorf("Expected run_id 'mock-run-id', got '%v'", rec.RunID)
			}
			if rec.TurnIndex != 1 {
				t.Errorf("Expected turn_index 1, got %v", rec.TurnIndex)
			}
			if rec.ToolIndex != 0 {
				t.Errorf("Expected tool_index 0, got %v", rec.ToolIndex)
			}
			if rec.ToolName != "test_tool" {
				t.Errorf("Expected tool_name 'test_tool', got '%v'", rec.ToolName)
			}
			if rec.Success != true {
				t.Errorf("Expected success true, got %v", rec.Success)
			}
			// Check full_result and model_result
			if rec.FullResult != "full result" {
				t.Errorf("Expected full_result 'full result', got '%v'", rec.FullResult)
			}
			if rec.ModelResult != "model result" {
				t.Errorf("Expected model_result 'model result', got '%v'", rec.ModelResult)
			}
			return nil
		},
	}

	agent.traceSession = mockTrace
	te.recordToolExecutionWithIndex("test_tool", "{}", nil, "full result", "model result", nil, 0)

	// Test error case
	mockTrace.RecordToolCallFunc = func(record interface{}) error {
		rec := record.(trace.ToolCallRecord)
		if rec.Success != false {
			t.Errorf("Expected success false for error case, got %v", rec.Success)
		}
		if rec.ErrorCategory != "validation" {
			t.Errorf("Expected error_category 'validation', got '%v'", rec.ErrorCategory)
		}
		return nil
	}
	testErr := &toolError{msg: "parsing arguments failed"}
	te.recordToolExecutionWithIndex("test_tool", "{}", nil, "", "", testErr, 1)
}

func TestNormalizeArguments(t *testing.T) {
	agent := &Agent{}
	te := NewToolExecutor(agent)

	tests := []struct {
		name     string
		input    map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name:  "normalizes positive int",
			input: map[string]interface{}{"count": 5},
			expected: map[string]interface{}{
				"count": 5,
			},
		},
		{
			name:  "handles float to int conversion",
			input: map[string]interface{}{"value": 10.0},
			expected: map[string]interface{}{
				"value": 10,
			},
		},
		{
			name:  "handles float that is not a whole number",
			input: map[string]interface{}{"value": 10.5},
			expected: map[string]interface{}{
				"value": 10.5,
			},
		},
		{
			name:  "handles nil input",
			input: nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := te.normalizeArguments(tt.input)
			if tt.expected == nil && result != nil {
				t.Errorf("Expected nil, got %v", result)
				return
			}
			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("For key %s: expected %v, got %v", k, v, result[k])
				}
			}
		})
	}
}

func TestCategorizeError(t *testing.T) {
	agent := &Agent{}
	te := NewToolExecutor(agent)

	tests := []struct {
		name            string
		err             error
		category        string
		messageContains string
	}{
		{
			name:     "unknown tool",
			err:      error(nil),
			category: "unknown_tool",
			messageContains: "unknown",
		},
		{
			name:     "timeout",
			err:      error(nil),
			category: "timeout",
			messageContains: "timed out",
		},
		{
			name:     "validation error",
			err:      error(nil),
			category: "validation",
			messageContains: "parsing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create actual errors
			var err error
			switch tt.category {
			case "unknown_tool":
				err = &toolError{msg: "unknown tool"}
			case "timeout":
				err = &toolError{msg: "tool execution timed out"}
			case "validation":
				err = &toolError{msg: "parsing arguments failed"}
			}

			category, message := te.categorizeError("test_tool", err)
			if category != tt.category {
				t.Errorf("Expected category '%s', got '%s'", tt.category, category)
			}
			if message != "" && !strings.Contains(message, tt.messageContains) {
				t.Errorf("Expected message to contain '%s', got '%s'", tt.messageContains, message)
			}
		})
	}
}

// toolError is a simple error type for testing
type toolError struct {
	msg string
}

func (e *toolError) Error() string {
	return e.msg
}

func TestToolIndex(t *testing.T) {
	agent := &Agent{
		currentIteration: 1,
		traceSession: &MockTraceSession{
			GetRunIDFunc: func() string {
				return "test-run"
			},
			RecordToolCallFunc: func(record interface{}) error {
				rec := record.(trace.ToolCallRecord)
				toolIndex := rec.ToolIndex
				// Verify tool indices are sequential
				if toolIndex < 0 || toolIndex > 2 {
					t.Errorf("Unexpected tool index: %v", toolIndex)
				}
				return nil
			},
		},
	}

	te := NewToolExecutor(agent)

	// Simulate tool execution
	te.recordToolExecutionWithIndex("tool1", "{}", nil, "full result", "model result", nil, 0)
	te.recordToolExecutionWithIndex("tool2", "{}", nil, "full result", "model result", nil, 1)
	te.recordToolExecutionWithIndex("tool3", "{}", nil, "full result", "model result", nil, 2)
}

func TestExecuteToolsResetsToolIndex(t *testing.T) {
	// Create a minimal agent with required fields
	agent := &Agent{
		currentIteration: 1,
		interruptCtx:     context.Background(),
		circuitBreaker:   &CircuitBreakerState{Actions: make(map[string]*CircuitBreakerAction)},
	}

	te := NewToolExecutor(agent)

	// Set toolIndex to non-zero value
	te.toolIndex = 5

	// Call ExecuteTools with no tools (should reset toolIndex)
	// This will not panic because agent has required fields
	toolCalls := []api.ToolCall{}
	result := te.ExecuteTools(toolCalls)

	// Verify toolIndex was reset
	if te.toolIndex != 0 {
		t.Errorf("Expected toolIndex to be reset to 0, got %d", te.toolIndex)
	}

	// Verify result is empty (no tools executed)
	if len(result) != 0 {
		t.Errorf("Expected empty result, got %d results", len(result))
	}
}
