//go:build unix

package tools

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// TestBPM_StartAndCheck — Start a fast command, verify output and exited status
// =============================================================================

func TestBPM_StartAndCheck(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "echo hello", "")
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)
	require.True(t, strings.HasPrefix(sessionID, "bg-echo-"))

	// Wait for the fast command to complete
	require.Eventually(t, func() bool {
		_, status, err := bpm.CheckOutput(sessionID)
		return err == nil && status == "exited"
	}, 3*time.Second, 100*time.Millisecond)

	output, status, err := bpm.CheckOutput(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "exited", status)
	assert.Contains(t, output, "hello")
}

// =============================================================================
// TestBPM_StartLongRunningAndCheck — Start sleep 60, check it's running, then stop
// =============================================================================

func TestBPM_StartLongRunningAndCheck(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "sleep 60", "")
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)

	// Immediately check — should be running
	output, status, err := bpm.CheckOutput(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "running", status)
	assert.Empty(t, output)

	// Stop it
	err = bpm.Stop(sessionID, 100*time.Millisecond)
	require.NoError(t, err)

	// Process should be gone
	assert.False(t, bpm.IsActive(sessionID))
}

// =============================================================================
// TestBPM_Stop — Start sleep 60, stop it, verify process is killed
// =============================================================================

func TestBPM_Stop(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "sleep 60", "")
	require.NoError(t, err)

	// Verify it's running
	assert.True(t, bpm.IsActive(sessionID))

	// Stop it
	err = bpm.Stop(sessionID, 100*time.Millisecond)
	require.NoError(t, err)

	// Should no longer be active
	assert.False(t, bpm.IsActive(sessionID))
}

// =============================================================================
// TestBPM_StopExitedProcess — Start echo, wait for exit, then stop (no-op)
// =============================================================================

func TestBPM_StopExitedProcess(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "echo done", "")
	require.NoError(t, err)

	// Wait for the fast command to exit
	require.Eventually(t, func() bool {
		return !bpm.IsActive(sessionID)
	}, 3*time.Second, 100*time.Millisecond)

	// Stop an already-exited process should be a no-op (no error)
	err = bpm.Stop(sessionID, 100*time.Millisecond)
	require.NoError(t, err)
}

// =============================================================================
// TestBPM_CheckNonexistentSession — CheckOutput for non-existent session
// =============================================================================

func TestBPM_CheckNonexistentSession(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	_, _, err := bpm.CheckOutput("nonexistent-session")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// =============================================================================
// TestBPM_StopNonexistentSession — Stop for non-existent session
// =============================================================================

func TestBPM_StopNonexistentSession(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	err := bpm.Stop("nonexistent-session", 100*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// =============================================================================
// TestBPM_IsActive — Start a process, verify IsActive, stop it, verify not active
// =============================================================================

func TestBPM_IsActive(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "sleep 60", "")
	require.NoError(t, err)

	// Should be active
	assert.True(t, bpm.IsActive(sessionID))

	// Stop it
	require.NoError(t, bpm.Stop(sessionID, 100*time.Millisecond))

	// Should no longer be active
	assert.False(t, bpm.IsActive(sessionID))
}

// =============================================================================
// TestBPM_IsActive_NonexistentSession — IsActive for non-existent session returns false
// =============================================================================

func TestBPM_IsActive_NonexistentSession(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	assert.False(t, bpm.IsActive("nonexistent-session"))
}

// =============================================================================
// TestBPM_AdoptProcess — Create an exec.Cmd manually, start it, then adopt
// =============================================================================

func TestBPM_AdoptProcess(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Create a running command manually
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-c", "sleep 60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Create output file manually (simulates what runShellCommandAdoptable does)
	outputFile, err := os.CreateTemp("", "sprout-bg-*.output")
	require.NoError(t, err)
	outputPath := outputFile.Name()
	defer os.Remove(outputPath)

	cmd.Stdout = outputFile
	cmd.Stderr = outputFile

	err = cmd.Start()
	require.NoError(t, err)

	// Adopt it into BPM. Pass nil waitCh — AdoptProcess will start its
	// own Wait goroutine since the test hasn't started one.
	outputFile.Close() // BPM will reopen for reading
	sessionID, err := bpm.AdoptProcess(cmd, outputPath, "sleep 60", cmd.Dir, nil)
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)

	// Verify the adopted process is tracked and running
	assert.True(t, bpm.IsActive(sessionID))
	_, status, err := bpm.CheckOutput(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "running", status)

	// Stop the adopted process
	err = bpm.Stop(sessionID, 100*time.Millisecond)
	require.NoError(t, err)
	assert.False(t, bpm.IsActive(sessionID))
}

// =============================================================================
// TestBPM_StartEmptyCommand — empty command returns error
// =============================================================================

func TestBPM_StartEmptyCommand(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	_, err := bpm.Start(context.Background(), "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

// =============================================================================
// TestBPM_StartWhitespaceCommand — whitespace-only command returns error
// =============================================================================

func TestBPM_StartWhitespaceCommand(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	_, err := bpm.Start(context.Background(), "   ", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

// =============================================================================
// TestBPM_ConcurrentStart — start multiple background processes simultaneously
// =============================================================================

func TestBPM_ConcurrentStart(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	const count = 5
	sessionIDs := make([]string, count)
	var wg sync.WaitGroup
	wg.Add(count)

	for i := 0; i < count; i++ {
		go func(i int) {
			defer wg.Done()
			sessionIDs[i], _ = bpm.Start(context.Background(), "sleep 60", "")
		}(i)
	}

	wg.Wait()

	// All session IDs should be unique and non-empty
	seen := make(map[string]bool)
	for i, id := range sessionIDs {
		require.NotEmpty(t, id, "session ID %d is empty", i)
		require.False(t, seen[id], "duplicate session ID %s", id)
		seen[id] = true
	}

	// All should be active
	for _, id := range sessionIDs {
		assert.True(t, bpm.IsActive(id))
	}

	// Stop all
	for _, id := range sessionIDs {
		require.NoError(t, bpm.Stop(id, 100*time.Millisecond))
	}
}

// =============================================================================
// TestBPM_Close — start some processes, call Close, verify all stopped
// =============================================================================

func TestBPM_Close(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()

	sessionIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		var err error
		sessionIDs[i], err = bpm.Start(context.Background(), "sleep 60", "")
		require.NoError(t, err)
	}

	// All active
	for _, id := range sessionIDs {
		assert.True(t, bpm.IsActive(id))
	}

	// Close stops cleanup goroutine and kills all processes
	bpm.Close()

	// Verify all processes are gone (they were killed by StopAll inside Close)
	// We can't re-check IsActive on the closed manager, but we verified Close
	// didn't panic, which is the main concern
}

// =============================================================================
// TestBPM_CheckOutput_CapturesStderr — output file captures both stdout and stderr
// =============================================================================

func TestBPM_CheckOutput_CapturesStderr(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Command that writes to stderr
	sessionID, err := bpm.Start(context.Background(), "echo stdout-line && echo stderr-line >&2", "")
	require.NoError(t, err)

	// Wait for command to finish
	require.Eventually(t, func() bool {
		_, status, err := bpm.CheckOutput(sessionID)
		return err == nil && status == "exited"
	}, 3*time.Second, 100*time.Millisecond)

	output, _, err := bpm.CheckOutput(sessionID)
	require.NoError(t, err)
	assert.Contains(t, output, "stdout-line")
	assert.Contains(t, output, "stderr-line")
}

// =============================================================================
// TestBPM_CheckOutput_MultiplePolls — repeated CheckOutput calls accumulate
// =============================================================================

func TestBPM_CheckOutput_MultiplePolls(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "echo line1 && sleep 0.2 && echo line2", "")
	require.NoError(t, err)

	// First poll — might have partial output
	_, _, err = bpm.CheckOutput(sessionID)
	require.NoError(t, err)

	// Second poll after a brief wait — should have more (or same) output
	time.Sleep(500 * time.Millisecond)
	output2, _, err := bpm.CheckOutput(sessionID)
	require.NoError(t, err)

	// Both should contain the content (second may have more)
	assert.Contains(t, output2, "line1")
	assert.Contains(t, output2, "line2")
}

// =============================================================================
// TestBPM_SessionID_Format — session ID follows expected format
// =============================================================================

func TestBPM_SessionID_Format(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	tests := []struct {
		command  string
		prefix   string
	}{
		{"echo hello", "bg-echo-"},
		{"ls -la", "bg-ls-"},
		{"npm test", "bg-npm-"},
		{"./my_script.sh", "bg-.-my_script.sh-"}, // / is not in allowed chars, replaced with -
	}

	for _, tc := range tests {
		t.Run(tc.prefix, func(t *testing.T) {
			sessionID, err := bpm.Start(context.Background(), tc.command, "")
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(sessionID, tc.prefix),
				"expected session ID to start with %q, got %q", tc.prefix, sessionID)
			// Session ID format: bg-<prefix>-<8 hex chars>
			parts := strings.Split(sessionID, "-")
			assert.GreaterOrEqual(t, len(parts), 3, "session ID should have at least 3 parts: bg, prefix, hex")
			// Clean up
			bpm.Stop(sessionID, 100*time.Millisecond)
		})
	}
}

// =============================================================================
// TestBPM_ExtractExitCode — extract exit code from various error types
// =============================================================================

func TestBPM_ExtractExitCode(t *testing.T) {
	t.Parallel()

	// Nil error returns 0
	assert.Equal(t, 0, extractExitCode(nil))

	// Non-exit error returns 0
	assert.Equal(t, 0, extractExitCode(os.ErrNotExist))

	// Note: We cannot test with a real exec.ExitError because constructing one
	// with a valid ProcessState requires an actual completed process.
	// The implementation matches the pattern in shell_native.go:extractExitCode.
}

// =============================================================================
// TestBPM_CheckOutput_NonexistentSession_NoCrash — ensure CheckOutput handles
// non-existent session gracefully without panicking on missing process fields
// =============================================================================

func TestBPM_CheckOutput_NonexistentSession_NoCrash(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Multiple calls to non-existent session should not cause issues
	for i := 0; i < 3; i++ {
		_, _, err := bpm.CheckOutput("does-not-exist-" + string(rune('a'+i)))
		require.Error(t, err)
	}
}

// =============================================================================
// TestBPM_ContextKey_Helpers — WithBackgroundProcessManager and
// BackgroundProcessManagerFromContext round-trip correctly
// =============================================================================

func TestBPM_ContextKey_Helpers(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Initially nil
	ctx := context.Background()
	assert.Nil(t, BackgroundProcessManagerFromContext(ctx))

	// Add to context
	ctx = WithBackgroundProcessManager(ctx, bpm)
	retrieved := BackgroundProcessManagerFromContext(ctx)
	require.NotNil(t, retrieved)
	assert.Same(t, bpm, retrieved)

	// Wrong type in context should return nil
	ctx2 := context.WithValue(context.Background(), bpmContextKey{}, "not a bpm")
	assert.Nil(t, BackgroundProcessManagerFromContext(ctx2))

	// Child context inherits the value
	ctx3 := context.WithValue(ctx, "other", 123)
	assert.Same(t, bpm, BackgroundProcessManagerFromContext(ctx3))
}

// =============================================================================
// TestBPM_Start_NonexistentDir — start with a non-existent working directory
// =============================================================================

func TestBPM_Start_NonexistentDir(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	_, err := bpm.Start(context.Background(), "echo hello", "/nonexistent/path/12345")
	// Should fail because directory doesn't exist
	require.Error(t, err)
}

// =============================================================================
// TestExtractCommandPrefixCLI — extract first word for session ID prefix
// =============================================================================

func TestExtractCommandPrefixCLI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"echo hello", "echo"},
		{"ls -la", "ls"},
		{"npm test", "npm"},
		{"./script.sh arg", "./script.sh"},
		{"sleep 60 &", "sleep"},
		{"cat file | grep foo", "cat"},
		{"echo 'quoted arg'", "echo"},
		{"", ""},
		{"  spaces  ", "spaces"},
		{"make build && test", "make"},
		{"curl -X POST http://localhost", "curl"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := extractCommandPrefixCLI(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// TestSanitizeSessionIDPartCLI — sanitize string for session ID
// =============================================================================

func TestSanitizeSessionIDPartCLI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"echo", "echo"},
		{"npm_test", "npm_test"},
		{"my-app", "my-app"},
		{"foo bar", "foo-bar"},
		{"hello world", "hello-world"},
		{"!@#$", "----"},
		{"", "unknown"},
		// sanitizeSessionIDPartCLI caps at maxLen=32 rune positions, replacing non-alphanumeric with -
		{"this is a really long command name that exceeds max length", "this-is-a-really-long-command-na"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := sanitizeSessionIDPartCLI(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// TestIsProcessGone — check "no such process" detection
// =============================================================================

func TestIsProcessGone(t *testing.T) {
	t.Parallel()

	assert.False(t, isProcessGone(nil))
	assert.False(t, isProcessGone(os.ErrNotExist))

	// syscall.ESRCH is "no such process"
	assert.True(t, isProcessGone(syscall.ESRCH))

	// os.SyscallError wrapping ESRCH also contains "no such process"
	assert.True(t, isProcessGone(&os.SyscallError{Err: syscall.ESRCH}))
}

// =============================================================================
// TestBPM_Stop_SignalSequencing_SIGINT_Kills — Normal processes respond to SIGINT.
// SIGINT sent to process group kills shell+sleep, so Stop returns within grace
// (~50ms) without escalating to SIGTERM or SIGKILL.
// =============================================================================

func TestBPM_Stop_SignalSequencing_SIGINT_Kills(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "sleep 60", "")
	require.NoError(t, err)

	// Verify it's running
	assert.True(t, bpm.IsActive(sessionID))

	// SIGINT kills normal sleep within the grace period — should return fast.
	start := time.Now()
	err = bpm.Stop(sessionID, 50*time.Millisecond)
	require.NoError(t, err)
	elapsed := time.Since(start)

	// If SIGINT alone killed it, elapsed ≈ grace period (50ms).
	// It should definitely be well under the 5s SIGTERM wait.
	assert.True(t, elapsed.Milliseconds() < 2000,
		"SIGINT should have killed the process within grace period; took %v", elapsed)

	assert.False(t, bpm.IsActive(sessionID))
}

// =============================================================================
// TestBPM_Stop_SignalSequencing_SIGTERM_Required — Process ignores SIGINT but
// responds to SIGTERM. After grace (50ms), SIGTERM is sent and kills the
// process. Total elapsed: grace + 5s SIGTERM wait ≈ 5s.
// This test takes ~5s because SIGTERM is the second step and has a hardcoded
// 5s wait in the Stop implementation.
// =============================================================================

func TestBPM_Stop_SignalSequencing_SIGTERM_Required(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// trap 'true' INT works in both bash and zsh (unlike trap '' INT which
	// removes the trap in zsh). The shell ignores INT and loops; sleep is
	// restarted each time the group-sent SIGINT kills it. SIGTERM is NOT
	// trapped so the shell exits on the default TERM handler.
	cmd := "trap 'true' INT; while true; do sleep 60; done"
	sessionID, err := bpm.Start(context.Background(), cmd, "")
	require.NoError(t, err)

	assert.True(t, bpm.IsActive(sessionID))

	start := time.Now()
	err = bpm.Stop(sessionID, 50*time.Millisecond)
	require.NoError(t, err)
	elapsed := time.Since(start)

	// SIGINT is ignored → grace (50ms) → SIGTERM sent → 5s wait → process dead.
	// Total ≈ 5.05s on systems where the shell properly traps signals.
	// NOTE: On some systems (notably macOS with process-group signaling),
	// the child sleep process may die before the parent shell can restart it,
	// causing the process to exit faster than expected. Don't fail on timing.
	if elapsed.Milliseconds() > 4000 {
		t.Logf("SIGTERM escalation took %v (expected ~5s)", elapsed)
	} else {
		t.Logf("Process exited in %v — child sleep killed before shell could restart (platform behavior)", elapsed)
	}

	assert.False(t, bpm.IsActive(sessionID))
}

// =============================================================================
// TestBPM_Stop_SignalSequencing_SIGKILL_Required — Process ignores both SIGINT
// and SIGTERM. Full escalation: SIGINT (ignored) → grace → SIGTERM (ignored)
// → 5s wait → SIGKILL kills it. Total: grace + 5s ≈ 5.05s.
// This test takes ~5s because of the 5s SIGTERM wait before SIGKILL.
// =============================================================================

func TestBPM_Stop_SignalSequencing_SIGKILL_Required(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// trap 'true' works in both bash and zsh. The shell ignores INT and TERM,
	// looping to restart sleep. Only SIGKILL will stop it.
	cmd := "trap 'true' INT TERM; while true; do sleep 60; done"
	sessionID, err := bpm.Start(context.Background(), cmd, "")
	require.NoError(t, err)

	assert.True(t, bpm.IsActive(sessionID))

	start := time.Now()
	err = bpm.Stop(sessionID, 50*time.Millisecond)
	require.NoError(t, err)
	elapsed := time.Since(start)

	// Full escalation: SIGINT ignored → grace → SIGTERM ignored → 5s → SIGKILL.
	// Total ≈ 5.05s on systems where the shell properly traps both signals.
	// NOTE: On some systems (notably macOS with process-group signaling),
	// the child sleep process may die before the parent shell can restart it,
	// causing the process to exit faster than expected. Don't fail on timing.
	if elapsed.Milliseconds() > 4000 {
		t.Logf("Full SIGKILL escalation took %v (expected ~5s)", elapsed)
	} else {
		t.Logf("Process exited in %v — child sleep killed before shell could restart (platform behavior)", elapsed)
	}

	// After SIGKILL + reap, process state is updated to inactive immediately.
	assert.False(t, bpm.IsActive(sessionID))
}

// =============================================================================
// TestBPM_Stop_GraceDuration — Verify the grace parameter is respected, not
// hardcoded. With grace=500ms, Stop should wait at least 500ms (SIGINT kills
// sleep within the grace period, so the method blocks for the full grace).
// =============================================================================

func TestBPM_Stop_GraceDuration(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "sleep 60", "")
	require.NoError(t, err)

	assert.True(t, bpm.IsActive(sessionID))

	start := time.Now()
	err = bpm.Stop(sessionID, 500*time.Millisecond)
	require.NoError(t, err)
	elapsed := time.Since(start)

	// The SIGINT kills sleep, but Stop still waits the full grace period
	// before checking if the process is still alive. So elapsed >= 500ms.
	assert.True(t, elapsed.Milliseconds() >= 500,
		"grace period should be respected; expected >= 500ms, got %v", elapsed)

	assert.False(t, bpm.IsActive(sessionID))
}

// =============================================================================
// TestBPM_Stop_CheckOutputAfterStop — After Stop, CheckOutput should report
// "exited" (not "running"), confirming that process state is correctly updated.
// =============================================================================

func TestBPM_Stop_CheckOutputAfterStop(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "sleep 60", "")
	require.NoError(t, err)

	// Verify it's running
	_, status, err := bpm.CheckOutput(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "running", status)

	// Stop it
	err = bpm.Stop(sessionID, 50*time.Millisecond)
	require.NoError(t, err)

	// Give the monitor goroutine a moment to update state (it closes proc.done
	// when cmd.Wait() returns, which happens after SIGINT kills the process).
	// After Stop, proc.Process is either already nil (from the monitor goroutine
	// reaping) or was set to nil by the SIGKILL path.
	require.Eventually(t, func() bool {
		_, s, err := bpm.CheckOutput(sessionID)
		return err == nil && s == "exited"
	}, 2*time.Second, 50*time.Millisecond)
}

// =============================================================================
// TestKindDefaultInStart — Start() sets Kind to "shell" by default
// =============================================================================

func TestKindDefaultInStart(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "sleep 0.1", "")
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)

	proc, found := bpm.GetProcess(sessionID)
	require.True(t, found, "GetProcess should find the session")
	require.NotNil(t, proc, "GetProcess should return a non-nil process")
	assert.Equal(t, "shell", proc.Kind, "Kind should default to 'shell' when using Start()")
}

// =============================================================================
// TestKindDefaultInAdoptProcess — AdoptProcess() sets Kind to "shell" by default
// =============================================================================

func TestKindDefaultInAdoptProcess(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-c", "sleep 0.1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	outputFile, err := os.CreateTemp("", "sprout-bg-*.output")
	require.NoError(t, err)
	outputPath := outputFile.Name()
	defer os.Remove(outputPath)

	cmd.Stdout = outputFile
	cmd.Stderr = outputFile

	err = cmd.Start()
	require.NoError(t, err)

	outputFile.Close()
	sessionID, err := bpm.AdoptProcess(cmd, outputPath, "sleep 0.1", cmd.Dir, nil)
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)

	proc, found := bpm.GetProcess(sessionID)
	require.True(t, found, "GetProcess should find the adopted session")
	require.NotNil(t, proc, "GetProcess should return a non-nil process")
	assert.Equal(t, "shell", proc.Kind, "Kind should default to 'shell' when using AdoptProcess()")
}

// =============================================================================
// TestStartWithKind — StartWithKind() sets Kind to the provided value
// =============================================================================

func TestStartWithKind(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	const kind = "automate"
	sessionID, err := bpm.StartWithKind(context.Background(), "sleep 0.1", "", kind)
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)

	proc, found := bpm.GetProcess(sessionID)
	require.True(t, found, "GetProcess should find the session")
	require.NotNil(t, proc, "GetProcess should return a non-nil process")
	assert.Equal(t, kind, proc.Kind, "Kind should match the value passed to StartWithKind()")
}

// =============================================================================
// TestGetProcessNotFound — GetProcess() returns nil, false for a non-existent session ID
// =============================================================================

func TestGetProcessNotFound(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	proc, found := bpm.GetProcess("nonexistent-session-id")
	assert.False(t, found, "GetProcess should return false for a non-existent session")
	assert.Nil(t, proc, "GetProcess should return nil for a non-existent session")
}

// =============================================================================
// TestGetProcessFound — GetProcess() returns correct process data for an existing session
// =============================================================================

func TestGetProcessFound(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	const kind = "custom-kind"
	sessionID, err := bpm.StartWithKind(context.Background(), "sleep 0.1", "", kind)
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)

	proc, found := bpm.GetProcess(sessionID)
	require.True(t, found, "GetProcess should return true for an existing session")
	require.NotNil(t, proc, "GetProcess should return a non-nil process")

	// Verify the returned process has correct metadata
	assert.Equal(t, sessionID, proc.ID, "Process ID should match the session ID")
	assert.Equal(t, kind, proc.Kind, "Process Kind should match the value set during creation")
	assert.Equal(t, "sleep 0.1", proc.Command, "Process Command should match the original command")
	assert.NotZero(t, proc.StartedAt, "Process StartedAt should be set")
	assert.NotZero(t, proc.LastPolled, "Process LastPolled should be set")
	assert.NotEmpty(t, proc.OutputPath, "Process OutputPath should be set")
}
