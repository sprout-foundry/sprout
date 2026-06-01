package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// mockTerminalAccess implements tools.TerminalAccess for agent-level testing.
// Most methods return no-op defaults; only the methods under test need custom
// behavior set per-scenario.
type mockTerminalAccess struct {
	executeCommandInHiddenFunc func(ctx context.Context, sessionID, command string) (string, int, error)
	getOrCreateHiddenFunc      func(ctx context.Context, chatID string) (string, error)
	executeBackgroundFunc      func(ctx context.Context, chatID, command string) (string, error)
	getBackgroundOutputFunc    func(sessionID string) (string, error)
	stopBackgroundFunc         func(sessionID string) error
	isSessionActiveFunc        func(sessionID string) bool
}

func (m *mockTerminalAccess) ExecuteCommandInHidden(ctx context.Context, sessionID, command string) (string, int, error) {
	if m.executeCommandInHiddenFunc != nil {
		return m.executeCommandInHiddenFunc(ctx, sessionID, command)
	}
	return "mock", 0, nil
}

func (m *mockTerminalAccess) GetOrCreateHiddenSessionForChat(ctx context.Context, chatID string) (string, error) {
	if m.getOrCreateHiddenFunc != nil {
		return m.getOrCreateHiddenFunc(ctx, chatID)
	}
	return "mock-session", nil
}

func (m *mockTerminalAccess) ExecuteCommandInBackground(ctx context.Context, chatID, command string) (string, error) {
	if m.executeBackgroundFunc != nil {
		return m.executeBackgroundFunc(ctx, chatID, command)
	}
	return "mock-bg-session", nil
}

func (m *mockTerminalAccess) GetBackgroundOutput(sessionID string) (string, error) {
	if m.getBackgroundOutputFunc != nil {
		return m.getBackgroundOutputFunc(sessionID)
	}
	return "mock output", nil
}

func (m *mockTerminalAccess) StopBackgroundSession(sessionID string) error {
	if m.stopBackgroundFunc != nil {
		return m.stopBackgroundFunc(sessionID)
	}
	return nil
}

func (m *mockTerminalAccess) IsSessionActive(sessionID string) bool {
	if m.isSessionActiveFunc != nil {
		return m.isSessionActiveFunc(sessionID)
	}
	return true
}

// TestStopBackgroundSession_ValidSessionID verifies that providing a valid
// stop_background parameter results in the TerminalManager's
// StopBackgroundSession being called and a success message returned.
func TestStopBackgroundSession_ValidSessionID(t *testing.T) {

	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Track that StopBackgroundSession was called with the expected session ID.
	var capturedSessionID string
	mockTM := &mockTerminalAccess{
		stopBackgroundFunc: func(sessionID string) error {
			capturedSessionID = sessionID
			return nil
		},
	}
	agent.SetTerminalManager(mockTM)

	result, err := handleShellCommand(context.Background(), agent, map[string]interface{}{
		"stop_background": "bg-echo-abcdef12",
	})

	if err != nil {
		t.Fatalf("handleShellCommand returned error: %v", err)
	}
	if capturedSessionID != "bg-echo-abcdef12" {
		t.Errorf("expected StopBackgroundSession called with 'bg-echo-abcdef12', got %q", capturedSessionID)
	}
	if !strings.Contains(result, "Background session bg-echo-abcdef12 stopped successfully") {
		t.Errorf("expected success message containing session ID, got %q", result)
	}
}

// TestStopBackgroundSession_SessionNotFound verifies that when the TerminalManager
// returns an error (e.g. session not found), the agent propagates a meaningful error
// that includes the session ID.
func TestStopBackgroundSession_SessionNotFound(t *testing.T) {

	agent := newTestAgent(t)
	defer agent.Shutdown()

	agent.SetTerminalManager(&mockTerminalAccess{
		stopBackgroundFunc: func(sessionID string) error {
			return errors.New("session not found: bg-nonexistent")
		},
	})

	_, err := handleShellCommand(context.Background(), agent, map[string]interface{}{
		"stop_background": "bg-nonexistent",
	})

	if err == nil {
		t.Fatal("expected error when session not found")
	}
	if !strings.Contains(err.Error(), "bg-nonexistent") {
		t.Errorf("expected error to contain session ID 'bg-nonexistent', got %q", err.Error())
	}
}

// TestStopBackgroundSession_NoTerminalManager verifies that calling
// stop_background when the agent has no TerminalManager set (CLI mode) falls
// back to BackgroundProcessManager. Since BPM lazy-initializes and the
// session doesn't exist there either, it returns a "session not found" error.
func TestStopBackgroundSession_NoTerminalManager(t *testing.T) {

	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Do NOT set a TerminalManager — simulates CLI mode.
	// In CLI mode, the agent lazy-creates a BackgroundProcessManager and
	// delegates stop_background to it. Since the session was never started
	// in the BPM, it returns "session not found".
	_, err := handleShellCommand(context.Background(), agent, map[string]interface{}{
		"stop_background": "bg-echo-abcdef12",
	})

	if err == nil {
		t.Fatal("expected error when no terminal manager is set (BPM session not found)")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error mentioning session not found (BPM fallback), got %q", err.Error())
	}
}

// TestStopBackgroundConflictsWithBackground verifies that providing both
// stop_background and background=true is rejected as conflicting parameters.
func TestStopBackgroundConflictsWithBackground(t *testing.T) {

	agent := newTestAgent(t)
	defer agent.Shutdown()

	mockTM := &mockTerminalAccess{
		stopBackgroundFunc: func(sessionID string) error {
			// Should never be called.
			t.Fatal("StopBackgroundSession should not be called when parameters conflict")
			return nil
		},
	}
	agent.SetTerminalManager(mockTM)

	_, err := handleShellCommand(context.Background(), agent, map[string]interface{}{
		"stop_background": "bg-echo-abcdef12",
		"background":      true,
	})

	if err == nil {
		t.Fatal("expected error when stop_background and background=true are both set")
	}
	if !strings.Contains(err.Error(), "stop_background and background=true cannot be used together") {
		t.Errorf("expected conflict error, got %q", err.Error())
	}
}

// TestStopBackgroundConflictsWithCheckBackground verifies that providing both
// stop_background and check_background is rejected as conflicting parameters.
func TestStopBackgroundConflictsWithCheckBackground(t *testing.T) {

	agent := newTestAgent(t)
	defer agent.Shutdown()

	mockTM := &mockTerminalAccess{
		stopBackgroundFunc: func(sessionID string) error {
			// Should never be called.
			t.Fatal("StopBackgroundSession should not be called when parameters conflict")
			return nil
		},
	}
	agent.SetTerminalManager(mockTM)

	_, err := handleShellCommand(context.Background(), agent, map[string]interface{}{
		"stop_background":  "bg-echo-abcdef12",
		"check_background": "bg-other-123456",
	})

	if err == nil {
		t.Fatal("expected error when stop_background and check_background are both set")
	}
	if !strings.Contains(err.Error(), "stop_background and check_background cannot be used together") {
		t.Errorf("expected conflict error, got %q", err.Error())
	}
}

// TestStopBackgroundSession_EmptySessionID verifies that passing an empty
// stop_background string is treated as "not set" — the handler falls through
// to command validation and requires a command parameter, rather than calling
// StopBackgroundSession. This confirms the `if stopBackground != ""` guard.
func TestStopBackgroundSession_EmptySessionID(t *testing.T) {

	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Track that StopBackgroundSession is NOT called when stop_background is "".
	stopCalled := false
	agent.SetTerminalManager(&mockTerminalAccess{
		stopBackgroundFunc: func(sessionID string) error {
			stopCalled = true
			return errors.New("should not be called")
		},
	})

	_, err := handleShellCommand(context.Background(), agent, map[string]interface{}{
		"stop_background": "",
	})

	if err == nil {
		t.Fatal("expected error when stop_background is empty and no command is provided")
	}
	if stopCalled {
		t.Error("StopBackgroundSession should NOT be called when stop_background is empty string")
	}
	// The handler treats "" as "not set" and falls through to require a command.
	// The actual error is from convertToString because command was never provided.
	if !strings.Contains(err.Error(), "command") {
		t.Errorf("expected error mentioning 'command', got %q", err.Error())
	}
}

// TestStopBackground_DoesNotExecuteCommand verifies that when stop_background is
// set, no shell command is executed — the function returns immediately after
// calling StopBackgroundSession.
func TestStopBackground_DoesNotExecuteCommand(t *testing.T) {

	agent := newTestAgent(t)
	defer agent.Shutdown()

	mockTM := &mockTerminalAccess{
		stopBackgroundFunc: func(sessionID string) error {
			return nil
		},
		executeCommandInHiddenFunc: func(ctx context.Context, sessionID, command string) (string, int, error) {
			// Should never be called — stop_background should short-circuit.
			t.Fatal("ExecuteCommandInHidden should not be called when stop_background is set")
			return "", 0, nil
		},
	}
	agent.SetTerminalManager(mockTM)

	// Provide both stop_background and command — command should be ignored.
	result, err := handleShellCommand(context.Background(), agent, map[string]interface{}{
		"stop_background": "bg-sleep-abc123",
		"command":         "sleep 100",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "bg-sleep-abc123") {
		t.Errorf("expected success message for session, got %q", result)
	}
}

// TestStopBackgroundSession_MockImplementsInterface ensures our mock
// satisfies the tools.TerminalAccess interface at compile time.
func TestStopBackgroundSession_MockImplementsInterface(t *testing.T) {

	// This assignment will fail to compile if mockTerminalAccess does not
	// implement tools.TerminalAccess.
	var _ tools.TerminalAccess = &mockTerminalAccess{}
}
