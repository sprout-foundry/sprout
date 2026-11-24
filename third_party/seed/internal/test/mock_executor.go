package test

import (
	"context"
	"sync"

	"github.com/sprout-foundry/seed/core"
)

// MockExecutor is a recording mock implementation of core.ToolExecutor.
// Configure tool results via AddResult or SetResults.
type MockExecutor struct {
	mu        sync.Mutex
	tools     []core.Tool
	results   []core.Message
	callIndex int

	// Recorded calls for assertion
	Calls [][]core.ToolCall
}

// NewMockExecutor creates an empty MockExecutor.
func NewMockExecutor() *MockExecutor {
	return &MockExecutor{}
}

// WithTools sets the available tools.
func (m *MockExecutor) WithTools(tools []core.Tool) *MockExecutor {
	m.tools = tools
	return m
}

// AddTool adds a single tool.
func (m *MockExecutor) AddTool(tool core.Tool) *MockExecutor {
	m.tools = append(m.tools, tool)
	return m
}

// AddResult appends a tool result message to the sequence.
func (m *MockExecutor) AddResult(msg core.Message) *MockExecutor {
	m.results = append(m.results, msg)
	return m
}

// AddToolResult adds a tool result for a specific call ID.
func (m *MockExecutor) AddToolResult(toolCallID, content string) *MockExecutor {
	m.results = append(m.results, core.Message{
		Role:       "tool",
		Content:    content,
		ToolCallID: toolCallID,
	})
	return m
}

// SetResults replaces the entire result sequence.
func (m *MockExecutor) SetResults(results []core.Message) *MockExecutor {
	m.results = results
	return m
}

// GetTools implements core.ToolExecutor.
func (m *MockExecutor) GetTools() []core.Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tools
}

// Execute implements core.ToolExecutor.
func (m *MockExecutor) Execute(_ context.Context, calls []core.ToolCall) []core.Message {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, calls)

	if m.callIndex+len(calls) <= len(m.results) {
		out := make([]core.Message, len(calls))
		for i := range calls {
			out[i] = m.results[m.callIndex+i]
		}
		m.callIndex += len(calls)
		return out
	}

	// Default: return placeholder results for each call
	results := make([]core.Message, len(calls))
	for i, call := range calls {
		results[i] = core.Message{
			Role:       "tool",
			Content:    "mock result",
			ToolCallID: call.ID,
		}
	}
	return results
}

// Reset clears recorded calls and resets the result index.
func (m *MockExecutor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = nil
	m.callIndex = 0
}

// CallCount returns the number of Execute calls made.
func (m *MockExecutor) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Calls)
}

// LastCalls returns the tool calls from the last Execute call.
func (m *MockExecutor) LastCalls() []core.ToolCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Calls) == 0 {
		return nil
	}
	return m.Calls[len(m.Calls)-1]
}
