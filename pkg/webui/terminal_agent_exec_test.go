package webui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Helper function unit tests (no PTY required)
// =============================================================================

func TestGenerateMarker(t *testing.T) {
	// generateMarker should produce a unique 32-character hex string each time.
	count := 100
	markers := make(map[string]bool, count)
	markerRegex := regexp.MustCompile(`^[0-9a-f]{32}$`)

	for i := 0; i < count; i++ {
		m, err := generateMarker()
		if err != nil {
			t.Fatalf("generateMarker failed on iteration %d: %v", i, err)
		}

		if len(m) != 32 {
			t.Errorf("marker should be 32 chars, got %d: %q", len(m), m)
		}

		if !markerRegex.MatchString(m) {
			t.Errorf("marker should be hex-only, got %q", m)
		}

		if markers[m] {
			t.Errorf("duplicate marker generated at iteration %d: %q", i, m)
		}
		markers[m] = true
	}
}

func TestStripANSI(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text unchanged",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "red color code",
			input:    "\x1b[31mred\x1b[0m",
			expected: "red",
		},
		{
			name:     "green bold",
			input:    "\x1b[1;32mgreen bold\x1b[0m",
			expected: "green bold",
		},
		{
			name:     "multiple escape sequences",
			input:    "\x1b[31mred\x1b[0m and \x1b[34mblue\x1b[0m",
			expected: "red and blue",
		},
		{
			name:     "OSC sequence",
			input:    "\x1b]0;window title\x07hello",
			expected: "hello",
		},
		{
			name:     "cursor movement sequences",
			input:    "\x1b[2J\x1b[Hclear",
			expected: "clear",
		},
		{
			name:     "complex mixed output",
			input:    "\x1b[1m\x1b[32m\x1b[4mstyled\x1b[0m\x1b[22m text\x1b[0m",
			expected: "styled text",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := stripANSI(tc.input)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

// =============================================================================
// Integration tests (require real PTY)
// =============================================================================

// waitForShellReady subscribes to a session's output and waits until the shell
// appears ready. It drains initial output (banner, rc files, prompt) and then
// waits for a quiet period. After receiving any output, it waits for quietPeriod
// of silence before considering the shell ready. This handles shells that emit
// multi-line startup sequences.
func waitForShellReady(t *testing.T, session *TerminalSession, quietPeriod time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sub := session.subscribe()
	defer session.unsubscribe(sub)

	// Drain all initial output and wait for quiet period.
	quietTimer := time.NewTimer(quietPeriod)
	defer quietTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Skipf("shell did not become ready (timed out after initial output); skipping PTY test")
			return

		case _, ok := <-sub.ch:
			if !ok {
				t.Skipf("PTY channel closed before shell became ready; skipping PTY test")
				return
			}
			// Reset the quiet timer on each chunk of output.
			if !quietTimer.Stop() {
				select {
				case <-quietTimer.C:
				default:
				}
			}
			quietTimer.Reset(quietPeriod)

		case <-quietTimer.C:
			// Quiet period elapsed — shell is ready.
			return
		}
	}
}

// createAndReadySession creates a hidden session and waits for shell readiness.
// Returns the session, or skips the test if the shell doesn't become ready.
func createAndReadySession(t *testing.T, tm *TerminalManager, id string) *TerminalSession {
	t.Helper()
	session, err := tm.CreateHiddenSession(id, "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	waitForShellReady(t, session, 2*time.Second)
	return session
}

func TestExecuteCommandAndWait_Success(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := createAndReadySession(t, tm, "exec-success")
	defer tm.CloseSession("exec-success")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, exitCode, err := tm.ExecuteCommandAndWait(ctx, session, "echo hello world")
	if err != nil {
		t.Fatalf("ExecuteCommandAndWait failed: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	if !strings.Contains(output, "hello world") {
		t.Errorf("output should contain 'hello world', got %q", output)
	}

	// Verify no ANSI escape sequences in output.
	if regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`).MatchString(output) {
		t.Errorf("output should have no ANSI sequences, got %q", output)
	}

	// Verify no sentinel line leaked into output.
	if strings.Contains(output, "__SPROUT_DONE__") {
		t.Errorf("output should not contain sentinel, got %q", output)
	}
}

func TestExecuteCommandAndWait_ExitCode1(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := createAndReadySession(t, tm, "exec-err")
	defer tm.CloseSession("exec-err")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// "false" is a shell builtin that always exits with code 1.
	output, exitCode, err := tm.ExecuteCommandAndWait(ctx, session, "false")
	if err != nil {
		t.Fatalf("ExecuteCommandAndWait failed: %v", err)
	}

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d (output: %q)", exitCode, output)
	}
}

func TestExecuteCommandAndWait_CommandNotFound(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := createAndReadySession(t, tm, "exec-notfound")
	defer tm.CloseSession("exec-notfound")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Run a command that definitely doesn't exist.
	output, exitCode, err := tm.ExecuteCommandAndWait(ctx, session, "this_command_does_not_exist_12345")
	if err != nil {
		t.Fatalf("ExecuteCommandAndWait failed: %v", err)
	}

	// On most shells, "command not found" returns exit code 127.
	if exitCode != 127 {
		t.Logf("exit code for 'command not found' was %d (output: %q)", exitCode, output)
	}

	if exitCode <= 0 {
		t.Errorf("expected non-zero exit code for command not found, got %d", exitCode)
	}
}

func TestExecuteCommandAndWait_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := createAndReadySession(t, tm, "exec-cancel")
	defer tm.CloseSession("exec-cancel")

	// Use a context that's already cancelled so the function returns immediately
	// on the first select iteration without actually running a long command.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling

	_, exitCode, err := tm.ExecuteCommandAndWait(ctx, session, "sleep 60")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}

	if exitCode != -1 {
		t.Errorf("expected exit code -1 for cancelled context, got %d", exitCode)
	}

	if !strings.Contains(err.Error(), "cancel") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("expected context error, got: %v", err)
	}
}

func TestExecuteCommandAndWait_ContextTimeout(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := createAndReadySession(t, tm, "exec-timeout")
	defer tm.CloseSession("exec-timeout")

	// Use a very short timeout to trigger deadline exceeded.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Sleep for longer than the timeout.
	_, exitCode, err := tm.ExecuteCommandAndWait(ctx, session, "sleep 10")
	if err == nil {
		t.Fatal("expected error from timed out context, got nil")
	}

	if exitCode != -1 {
		t.Errorf("expected exit code -1 for timed out context, got %d", exitCode)
	}
}

func TestExecuteCommandAndWait_NonHiddenSession(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session, err := tm.CreateSession("exec-visible")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	defer tm.CloseSession("exec-visible")

	waitForShellReady(t, session, 2*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, exitCode, err := tm.ExecuteCommandAndWait(ctx, session, "echo test")
	if err == nil {
		t.Fatal("expected error for non-hidden session, got nil")
	}

	if !strings.Contains(err.Error(), "hidden") {
		t.Errorf("expected error to mention 'hidden', got: %v", err)
	}

	if exitCode != -1 {
		t.Errorf("expected exit code -1, got %d", exitCode)
	}
}

func TestExecuteCommandAndWait_InactiveSession(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session, err := tm.CreateHiddenSession("exec-inactive", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	// Close the session to make it inactive.
	tm.CloseSession("exec-inactive")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, exitCode, err := tm.ExecuteCommandAndWait(ctx, session, "echo test")
	if err == nil {
		t.Fatal("expected error for inactive session, got nil")
	}

	if exitCode != -1 {
		t.Errorf("expected exit code -1, got %d", exitCode)
	}
}

func TestExecuteCommandAndWait_SessionNotFound(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, exitCode, err := tm.ExecuteCommandInHidden(ctx, "nonexistent-session", "echo test")
	if err == nil {
		t.Fatal("expected error for nonexistent session, got nil")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error to mention 'not found', got: %v", err)
	}

	if exitCode != -1 {
		t.Errorf("expected exit code -1, got %d", exitCode)
	}
}

func TestExecuteCommandInHidden(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	_ = createAndReadySession(t, tm, "exec-convenience")
	defer tm.CloseSession("exec-convenience")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, exitCode, err := tm.ExecuteCommandInHidden(ctx, "exec-convenience", "echo hello")
	if err != nil {
		t.Fatalf("ExecuteCommandInHidden failed: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	if !strings.Contains(output, "hello") {
		t.Errorf("output should contain 'hello', got %q", output)
	}

	// Also verify it rejects a non-existent session.
	_, _, err = tm.ExecuteCommandInHidden(ctx, "nonexistent-session", "echo test")
	if err == nil {
		t.Fatal("expected error for nonexistent session, got nil")
	}

	// And verify it rejects a visible session.
	_, err = tm.CreateSession("exec-visible-convenience")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	defer tm.CloseSession("exec-visible-convenience")

	visibleSession, exists := tm.GetSession("exec-visible-convenience")
	if !exists {
		t.Fatal("visible session should exist")
	}
	waitForShellReady(t, visibleSession, 2*time.Second)

	_, _, err = tm.ExecuteCommandInHidden(ctx, "exec-visible-convenience", "echo test")
	if err == nil {
		t.Fatal("expected error when using ExecuteCommandInHidden on visible session, got nil")
	}
}

func TestExecuteCommandAndWait_MultipleCommands(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := createAndReadySession(t, tm, "exec-multi")
	defer tm.CloseSession("exec-multi")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Run first command.
	output1, exitCode1, err := tm.ExecuteCommandAndWait(ctx, session, "echo first")
	if err != nil {
		t.Fatalf("first command failed: %v", err)
	}
	if exitCode1 != 0 {
		t.Errorf("first command: expected exit code 0, got %d", exitCode1)
	}
	if !strings.Contains(output1, "first") {
		t.Errorf("first command output should contain 'first', got %q", output1)
	}

	// Run second command on the same session.
	output2, exitCode2, err := tm.ExecuteCommandAndWait(ctx, session, "echo second")
	if err != nil {
		t.Fatalf("second command failed: %v", err)
	}
	if exitCode2 != 0 {
		t.Errorf("second command: expected exit code 0, got %d", exitCode2)
	}
	if !strings.Contains(output2, "second") {
		t.Errorf("second command output should contain 'second', got %q", output2)
	}

	// Run a failing command.
	_, exitCode3, err := tm.ExecuteCommandAndWait(ctx, session, "false")
	if err != nil {
		t.Fatalf("third command failed: %v", err)
	}
	if exitCode3 != 1 {
		t.Errorf("third command: expected exit code 1, got %d", exitCode3)
	}
}

func TestExecuteCommandAndWait_MarkerUniqueness(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := createAndReadySession(t, tm, "exec-marker")
	defer tm.CloseSession("exec-marker")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Run multiple commands quickly; each should produce a unique marker
	// and not interfere with each other.
	for i := 0; i < 3; i++ {
		cmd := fmt.Sprintf("echo run%d", i)
		output, exitCode, err := tm.ExecuteCommandAndWait(ctx, session, cmd)
		if err != nil {
			t.Fatalf("command %d failed: %v", i, err)
		}
		if exitCode != 0 {
			t.Errorf("command %d: expected exit code 0, got %d", i, exitCode)
		}
		expected := fmt.Sprintf("run%d", i)
		if !strings.Contains(output, expected) {
			t.Errorf("command %d: output should contain %q, got %q", i, expected, output)
		}
	}
}
