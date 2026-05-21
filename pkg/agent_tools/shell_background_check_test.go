//go:build !js

// Background-shell tests use the mock TerminalManager which only exists
// in native test code. The js/wasm shell path has no background-session
// concept — see shell_js.go for the streamlined wasmshell-backed impl.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestCheckBackgroundOutput_Success(t *testing.T) {
	tm := &mockTerminalManager{
		getBackgroundOutputFunc: func(sessionID string) (string, error) {
			if sessionID != "bg-npm-dev-aabbccdd" {
				t.Errorf("expected sessionID 'bg-npm-dev-aabbccdd', got %q", sessionID)
			}
			return "Server running on port 3000", nil
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	result, err := CheckBackgroundOutput(ctx, "bg-npm-dev-aabbccdd")
	if err != nil {
		t.Fatalf("CheckBackgroundOutput failed: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	if parsed["session_id"] != "bg-npm-dev-aabbccdd" {
		t.Errorf("expected session_id 'bg-npm-dev-aabbccdd', got %q", parsed["session_id"])
	}
	if parsed["status"] != "running" {
		t.Errorf("expected status 'running', got %q", parsed["status"])
	}
	if parsed["output"] != "Server running on port 3000" {
		t.Errorf("expected output 'Server running on port 3000', got %q", parsed["output"])
	}
}

func TestCheckBackgroundOutput_EmptySessionID(t *testing.T) {
	tm := &mockTerminalManager{}
	ctx := WithTerminalManager(context.Background(), tm)

	_, err := CheckBackgroundOutput(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error to mention 'empty', got %q", err.Error())
	}
}

func TestCheckBackgroundOutput_NoTerminalManager(t *testing.T) {
	ctx := context.Background()

	_, err := CheckBackgroundOutput(ctx, "bg-session-1")
	if err == nil {
		t.Fatal("expected error when no terminal manager available")
	}
	if !strings.Contains(err.Error(), "WebUI") {
		t.Errorf("expected error to mention 'WebUI', got %q", err.Error())
	}
}

func TestCheckBackgroundOutput_SessionNotFound(t *testing.T) {
	tm := &mockTerminalManager{
		getBackgroundOutputFunc: func(sessionID string) (string, error) {
			return "", fmt.Errorf("session %s not found", sessionID)
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	_, err := CheckBackgroundOutput(ctx, "bg-nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error to mention 'not found', got %q", err.Error())
	}
}

func TestCheckBackgroundOutput_WhitespaceSessionID(t *testing.T) {
	tm := &mockTerminalManager{}
	ctx := WithTerminalManager(context.Background(), tm)

	_, err := CheckBackgroundOutput(ctx, "   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only session ID")
	}
}

func TestCheckBackgroundOutput_LargeOutput(t *testing.T) {
	largeOutput := strings.Repeat("output line\n", 1000)

	tm := &mockTerminalManager{
		getBackgroundOutputFunc: func(sessionID string) (string, error) {
			return largeOutput, nil
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	result, err := CheckBackgroundOutput(ctx, "bg-large-output")
	if err != nil {
		t.Fatalf("CheckBackgroundOutput failed: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	if len(parsed["output"]) != len(largeOutput) {
		t.Errorf("expected output length %d, got %d", len(largeOutput), len(parsed["output"]))
	}
}

// --- Security classifier integration tests ---

func TestClassifyShellCommand_CheckBackgroundSafe(t *testing.T) {
	// When check_background is set with no command, it should be SAFE (not CAUTION)
	result := ClassifyToolCall("shell_command", map[string]interface{}{
		"check_background": "bg-npm-dev-aabbccdd",
	})
	if result.Risk != SecuritySafe {
		t.Errorf("expected SecuritySafe for check_background-only call, got %v", result.Risk)
	}
	if result.ShouldPrompt {
		t.Error("check_background-only call should not prompt")
	}
}

func TestClassifyShellCommand_CheckBackgroundWithCommand(t *testing.T) {
	// When both check_background and command are provided, it should classify the command normally
	result := ClassifyToolCall("shell_command", map[string]interface{}{
		"check_background": "bg-npm-dev-aabbccdd",
		"command":          "ls",
	})
	if result.Risk != SecuritySafe {
		t.Errorf("expected SecuritySafe for 'ls' command with check_background, got %v", result.Risk)
	}
}

func TestClassifyShellCommand_EmptyCommandWithoutCheckBackground(t *testing.T) {
	// Empty command without check_background should still be CAUTION
	result := ClassifyToolCall("shell_command", map[string]interface{}{
		"command": "",
	})
	if result.Risk != SecurityCaution {
		t.Errorf("expected SecurityCaution for empty command without check_background, got %v", result.Risk)
	}
}

func TestClassifyShellCommand_StopBackgroundSafe(t *testing.T) {
	// stop_background is a session management operation — no shell execution.
	// It should be classified as SAFE, not CAUTION.
	result := ClassifyToolCall("shell_command", map[string]interface{}{
		"stop_background": "bg-npm-dev-aabbccdd",
	})
	if result.Risk != SecuritySafe {
		t.Errorf("expected SecuritySafe for stop_background, got %v", result.Risk)
	}
	if result.ShouldPrompt {
		t.Error("stop_background should not prompt")
	}
}

func TestCheckBackgroundOutput_StatusExited(t *testing.T) {
	// When IsSessionActive returns false, CheckBackgroundOutput should report status='exited'
	tm := &mockTerminalManager{
		getBackgroundOutputFunc: func(sessionID string) (string, error) {
			return "hello\n", nil
		},
		isSessionActiveFunc: func(sessionID string) bool {
			return false // Session has finished
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	result, err := CheckBackgroundOutput(ctx, "bg-exited-session")
	if err != nil {
		t.Fatalf("CheckBackgroundOutput failed: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	if parsed["status"] != "exited" {
		t.Errorf("expected status 'exited', got %q", parsed["status"])
	}
	if parsed["session_id"] != "bg-exited-session" {
		t.Errorf("expected session_id 'bg-exited-session', got %q", parsed["session_id"])
	}
	if parsed["output"] != "hello\n" {
		t.Errorf("expected output 'hello\\n', got %q", parsed["output"])
	}
}

func TestCheckBackgroundOutput_StatusRunning(t *testing.T) {
	// When IsSessionActive returns true, CheckBackgroundOutput should report status='running'
	tm := &mockTerminalManager{
		getBackgroundOutputFunc: func(sessionID string) (string, error) {
			return "still working...\n", nil
		},
		isSessionActiveFunc: func(sessionID string) bool {
			return true // Session is still running
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	result, err := CheckBackgroundOutput(ctx, "bg-running-session")
	if err != nil {
		t.Fatalf("CheckBackgroundOutput failed: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	if parsed["status"] != "running" {
		t.Errorf("expected status 'running', got %q", parsed["status"])
	}
}

