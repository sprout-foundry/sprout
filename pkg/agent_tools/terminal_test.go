//go:build !js

// Terminal-manager tests exercise the native shell fallback path. The
// js/wasm build has no terminal manager and routes shell through
// wasmshell instead — see shell_js.go / cmd/wasm/shell_executor.go.

package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// mockTerminalManager is a mock implementation of TerminalAccess for testing.
type mockTerminalManager struct {
	executeCommandFunc       func(ctx context.Context, sessionID, command string) (string, int, error)
	getOrCreateFunc          func(ctx context.Context, chatID string) (string, error)
	executeBackgroundFunc    func(ctx context.Context, chatID, command string) (string, error)
	getBackgroundOutputFunc  func(sessionID string) (string, error)
	stopBackgroundFunc       func(sessionID string) error
	isSessionActiveFunc      func(sessionID string) bool
}

func (m *mockTerminalManager) ExecuteCommandInHidden(ctx context.Context, sessionID, command string) (string, int, error) {
	if m.executeCommandFunc != nil {
		return m.executeCommandFunc(ctx, sessionID, command)
	}
	return "mock output", 0, nil
}

func (m *mockTerminalManager) GetOrCreateHiddenSessionForChat(ctx context.Context, chatID string) (string, error) {
	if m.getOrCreateFunc != nil {
		return m.getOrCreateFunc(ctx, chatID)
	}
	return "mock-session-" + chatID, nil
}

func (m *mockTerminalManager) ExecuteCommandInBackground(ctx context.Context, chatID, command string) (string, error) {
	if m.executeBackgroundFunc != nil {
		return m.executeBackgroundFunc(ctx, chatID, command)
	}
	return "mock-bg-session-" + chatID, nil
}

func (m *mockTerminalManager) GetBackgroundOutput(sessionID string) (string, error) {
	if m.getBackgroundOutputFunc != nil {
		return m.getBackgroundOutputFunc(sessionID)
	}
	return "mock background output", nil
}

func (m *mockTerminalManager) StopBackgroundSession(sessionID string) error {
	if m.stopBackgroundFunc != nil {
		return m.stopBackgroundFunc(sessionID)
	}
	return nil
}

func (m *mockTerminalManager) IsSessionActive(sessionID string) bool {
	if m.isSessionActiveFunc != nil {
		return m.isSessionActiveFunc(sessionID)
	}
	return true
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

// Tests for PTY routing functionality

func TestShellCommandRoutesThroughTerminalManager(t *testing.T) {
	tm := &mockTerminalManager{
		getOrCreateFunc: func(ctx context.Context, chatID string) (string, error) {
			return "agent-hidden-test-chat", nil
		},
		executeCommandFunc: func(ctx context.Context, sessionID, command string) (string, int, error) {
			if sessionID != "agent-hidden-test-chat" {
				t.Errorf("expected session ID 'agent-hidden-test-chat', got %q", sessionID)
			}
			return "hello from PTY", 0, nil
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	output, err := ExecuteShellCommand(ctx, "echo hello")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "hello from PTY") {
		t.Errorf("expected output to contain 'hello from PTY', got %q", output)
	}
}

func TestShellCommandFallsBackWhenNoTerminalManager(t *testing.T) {
	ctx := context.Background()
	// No terminal manager in context
	output, err := ExecuteShellCommand(ctx, "echo hello")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected output to contain 'hello', got %q", output)
	}
}

func TestShellCommandFallsBackOnSessionCreationFailure(t *testing.T) {
	tm := &mockTerminalManager{
		getOrCreateFunc: func(ctx context.Context, chatID string) (string, error) {
			return "", fmt.Errorf("failed to create session")
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	output, err := ExecuteShellCommand(ctx, "echo hello")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Should fall back to os/exec
	if !strings.Contains(output, "hello") {
		t.Errorf("expected output to contain 'hello', got %q", output)
	}
}

func TestShellCommandFallsBackOnExecutionFailure(t *testing.T) {
	tm := &mockTerminalManager{
		getOrCreateFunc: func(ctx context.Context, chatID string) (string, error) {
			return "agent-hidden-test", nil
		},
		executeCommandFunc: func(ctx context.Context, sessionID, command string) (string, int, error) {
			return "", -1, fmt.Errorf("PTY error")
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	output, err := ExecuteShellCommand(ctx, "echo hello")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Should fall back to os/exec
	if !strings.Contains(output, "hello") {
		t.Errorf("expected output to contain 'hello', got %q", output)
	}
}

func TestShellCommandSkipsTerminalManagerWhenStreaming(t *testing.T) {
	tm := &mockTerminalManager{
		getOrCreateFunc: func(ctx context.Context, chatID string) (string, error) {
			t.Error("GetOrCreateHiddenSessionForChat should not be called for streaming mode")
			return "", nil
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	// streamOutput=true should skip PTY routing
	_, err := ExecuteShellCommandWithSafety(ctx, "echo hello", true, "test-chat", true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestShellCommandPTYPath_NonZeroExitCode(t *testing.T) {
	tm := &mockTerminalManager{
		getOrCreateFunc: func(ctx context.Context, chatID string) (string, error) {
			return "mock-session", nil
		},
		executeCommandFunc: func(ctx context.Context, sessionID, command string) (string, int, error) {
			return "command failed with error", 1, nil
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	output, err := ExecuteShellCommand(ctx, "false")
	if err != nil {
		t.Errorf("expected no error for non-zero exit via PTY, got %v", err)
	}
	if !strings.Contains(output, "command failed with error") {
		t.Errorf("expected output to contain 'command failed with error', got %q", output)
	}
}
