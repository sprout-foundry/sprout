package test

import (
	"strings"
	"sync"
)

// MockUI is a recording mock implementation of core.UI.
// It captures all Print/PrintLine output and can be pre-configured with
// prompt/confirm responses.
type MockUI struct {
	mu           sync.Mutex
	promptResp   string
	promptErr    error
	confirmResp  bool
	confirmErr   error
	printBuf     strings.Builder
	printLineBuf strings.Builder

	// Recorded calls for assertion
	Prompts    []string
	Confirms   []string
	Prints     []string
	PrintLines []string
}

// NewMockUI creates a new MockUI.
func NewMockUI() *MockUI {
	return &MockUI{}
}

// WithPromptResponse sets the response returned by the next Prompt call.
func (m *MockUI) WithPromptResponse(resp string) *MockUI {
	m.promptResp = resp
	return m
}

// WithConfirmResponse sets the response returned by the next Confirm call.
func (m *MockUI) WithConfirmResponse(resp bool) *MockUI {
	m.confirmResp = resp
	return m
}

// Prompt implements core.UI.
func (m *MockUI) Prompt(message string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Prompts = append(m.Prompts, message)
	return m.promptResp, m.promptErr
}

// Confirm implements core.UI.
func (m *MockUI) Confirm(message string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Confirms = append(m.Confirms, message)
	return m.confirmResp, m.confirmErr
}

// Print implements core.UI.
func (m *MockUI) Print(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.printBuf.WriteString(message)
	m.Prints = append(m.Prints, message)
}

// PrintLine implements core.UI.
func (m *MockUI) PrintLine(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.printLineBuf.WriteString(message + "\n")
	m.PrintLines = append(m.PrintLines, message)
}

// Output returns all Print output (without newlines).
func (m *MockUI) Output() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.printBuf.String()
}

// LineOutput returns all PrintLine output (with newlines).
func (m *MockUI) LineOutput() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.printLineBuf.String()
}

// Reset clears all recorded output and calls.
func (m *MockUI) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.printBuf.Reset()
	m.printLineBuf.Reset()
	m.Prompts = nil
	m.Confirms = nil
	m.Prints = nil
	m.PrintLines = nil
}
