package tools

import (
	"context"
	"testing"
)

// mockTerminalManager is a mock implementation of TerminalAccess for testing.
type mockTerminalManager struct {
	executeCommandFunc func(ctx context.Context, sessionID, command string) (string, int, error)
}

func (m *mockTerminalManager) ExecuteCommandInHidden(ctx context.Context, sessionID, command string) (string, int, error) {
	if m.executeCommandFunc != nil {
		return m.executeCommandFunc(ctx, sessionID, command)
	}
	return "mock output", 0, nil
}

func TestWithTerminalManager(t *testing.T) {
	ctx := context.Background()

	// Initially, no terminal manager in context
	tm := TerminalManagerFromContext(ctx)
	if tm != nil {
		t.Errorf("expected nil terminal manager, got %v", tm)
	}

	// Add terminal manager to context
	mockTM := &mockTerminalManager{}
	ctx = WithTerminalManager(ctx, mockTM)

	// Retrieve terminal manager from context
	tm = TerminalManagerFromContext(ctx)
	if tm == nil {
		t.Errorf("expected terminal manager, got nil")
	}
	if tm != mockTM {
		t.Errorf("expected mockTM, got %v", tm)
	}

	// Test ExecuteCommandInHidden through interface
	output, exitCode, err := tm.ExecuteCommandInHidden(ctx, "test-session", "echo hello")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if output != "mock output" {
		t.Errorf("expected 'mock output', got %q", output)
	}
}

func TestTerminalManagerFromContext_Nil(t *testing.T) {
	ctx := context.Background()
	tm := TerminalManagerFromContext(ctx)
	if tm != nil {
		t.Errorf("expected nil terminal manager, got %v", tm)
	}
}

func TestTerminalManagerFromContext_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), terminalManagerKey, "not a TerminalAccess")
	tm := TerminalManagerFromContext(ctx)
	if tm != nil {
		t.Errorf("expected nil terminal manager for wrong type, got %v", tm)
	}
}
