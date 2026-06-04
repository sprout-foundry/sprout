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
	"sync/atomic"
	"testing"
	"time"
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

func TestCheckBackgroundOutputWait_ZeroReturnsImmediately(t *testing.T) {
	calls := int32(0)
	tm := &mockTerminalManager{
		getBackgroundOutputFunc: func(sessionID string) (string, error) {
			atomic.AddInt32(&calls, 1)
			return "still running", nil
		},
		isSessionActiveFunc: func(sessionID string) bool { return true },
	}
	ctx := WithTerminalManager(context.Background(), tm)

	start := time.Now()
	_, err := CheckBackgroundOutputWait(ctx, "bg-x", 0)
	if err != nil {
		t.Fatalf("CheckBackgroundOutputWait failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("wait_seconds=0 should return immediately, took %v", elapsed)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected 1 snapshot, got %d", got)
	}
}

func TestCheckBackgroundOutputWait_ReturnsOnExit(t *testing.T) {
	var active atomic.Bool
	active.Store(true)
	tm := &mockTerminalManager{
		getBackgroundOutputFunc: func(sessionID string) (string, error) {
			return "done", nil
		},
		isSessionActiveFunc: func(sessionID string) bool { return active.Load() },
	}
	ctx := WithTerminalManager(context.Background(), tm)

	// Mark the session inactive after a short delay; the wait should
	// return well before the 10s cap.
	go func() {
		time.Sleep(600 * time.Millisecond)
		active.Store(false)
	}()

	start := time.Now()
	result, err := CheckBackgroundOutputWait(ctx, "bg-y", 10)
	if err != nil {
		t.Fatalf("CheckBackgroundOutputWait failed: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Errorf("expected wait to return shortly after exit, took %v", elapsed)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed["status"] != "exited" {
		t.Errorf("expected status 'exited', got %q", parsed["status"])
	}
}

func TestCheckBackgroundOutputWait_ReturnsOnDeadline(t *testing.T) {
	tm := &mockTerminalManager{
		getBackgroundOutputFunc: func(sessionID string) (string, error) { return "still running", nil },
		isSessionActiveFunc:     func(sessionID string) bool { return true },
	}
	ctx := WithTerminalManager(context.Background(), tm)

	start := time.Now()
	result, err := CheckBackgroundOutputWait(ctx, "bg-z", 1) // 1 second cap
	if err != nil {
		t.Fatalf("CheckBackgroundOutputWait failed: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 900*time.Millisecond {
		t.Errorf("expected wait to honor deadline (~1s), returned in %v", elapsed)
	}
	if elapsed > 2500*time.Millisecond {
		t.Errorf("expected wait to return shortly after deadline (~1s), took %v", elapsed)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed["status"] != "running" {
		t.Errorf("expected status 'running' when deadline hits, got %q", parsed["status"])
	}
}

func TestCheckBackgroundOutputWait_HonorsContextCancel(t *testing.T) {
	tm := &mockTerminalManager{
		getBackgroundOutputFunc: func(sessionID string) (string, error) { return "still running", nil },
		isSessionActiveFunc:     func(sessionID string) bool { return true },
	}
	ctx, cancel := context.WithCancel(WithTerminalManager(context.Background(), tm))

	go func() {
		time.Sleep(400 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	if _, err := CheckBackgroundOutputWait(ctx, "bg-cancel", 10); err != nil {
		t.Fatalf("CheckBackgroundOutputWait failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("expected context cancel to return promptly, took %v", elapsed)
	}
}

func TestCheckBackgroundOutputWait_CapsAtMax(t *testing.T) {
	// Exceeding the cap should not error — it should silently clamp.
	// We can only verify it doesn't reject the input. A second-precision
	// timing assertion against the 600s cap would make the test too slow.
	tm := &mockTerminalManager{
		getBackgroundOutputFunc: func(sessionID string) (string, error) { return "done", nil },
		isSessionActiveFunc:     func(sessionID string) bool { return false },
	}
	ctx := WithTerminalManager(context.Background(), tm)

	if _, err := CheckBackgroundOutputWait(ctx, "bg-cap", 99999); err != nil {
		t.Fatalf("CheckBackgroundOutputWait failed: %v", err)
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

