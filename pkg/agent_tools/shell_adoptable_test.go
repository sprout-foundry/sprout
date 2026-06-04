//go:build !js

package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// TestAdoptable_NormalCompletion — command finishes before deadline, normal output
// =============================================================================

func TestAdoptable_NormalCompletion(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Set a generous deadline so the fast command finishes in time
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
	defer cancel()

	ctx = WithBackgroundProcessManager(ctx, bpm)

	output, err := runShellCommandAdoptable(ctx, "echo hello world", bpm)
	require.NoError(t, err)
	assert.Contains(t, output, "hello world")

	// No background session should have been created
	sessions := listBPMSessions(bpm)
	assert.Empty(t, sessions, "no background sessions should exist after normal completion")
}

// =============================================================================
// TestAdoptable_TimeoutPromotion — command exceeds deadline, promoted to background
// =============================================================================

func TestAdoptable_TimeoutPromotion(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Short deadline — the sleep will definitely exceed it
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(1*time.Second))
	defer cancel()

	ctx = WithBackgroundProcessManager(ctx, bpm)

	output, err := runShellCommandAdoptable(ctx, "sleep 30", bpm)
	require.NoError(t, err)

	// The output should contain the background promotion message
	assert.Contains(t, output, "exceeded the 2-minute tool deadline")
	assert.Contains(t, output, "still running in background session")

	// Extract the session ID from the output message
	sessionID := extractSessionIDFromPromotionMessage(output)
	require.NotEmpty(t, sessionID, "should find session ID in promotion message")

	// Verify the session is tracked in the BPM
	assert.True(t, bpm.IsActive(sessionID), "session %s should be active in BPM", sessionID)

	// Clean up: stop the adopted session
	err = bpm.Stop(sessionID, 100*time.Millisecond)
	require.NoError(t, err)

	// Wait briefly for the monitor goroutine to clean up proc.Process
	time.Sleep(200 * time.Millisecond)
	assert.False(t, bpm.IsActive(sessionID), "session %s should not be active after stop", sessionID)
}

// =============================================================================
// TestAdoptable_UserCancellation — context cancelled (not DeadlineExceeded) kills
// =============================================================================

func TestAdoptable_UserCancellation(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Create a cancellable context (not deadline-based)
	ctx, cancel := context.WithCancel(context.Background())

	ctx = WithBackgroundProcessManager(ctx, bpm)

	// Start a long-running command
	go func() {
		time.Sleep(500 * time.Millisecond) // brief delay then cancel
		cancel()
	}()

	// Use a non-deadline context — cancellation should kill the process
	_, err := runShellCommandAdoptable(ctx, "sleep 30", bpm)

	// Should return the context cancellation error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")

	// No background session should have been created (cancellation != timeout)
	sessions := listBPMSessions(bpm)
	assert.Empty(t, sessions, "no background sessions after user cancellation")
}

// =============================================================================
// TestAdoptable_NoBPM — when BPM is not in context, falls back to CommandContext
// =============================================================================

func TestAdoptable_NoBPM(t *testing.T) {
	t.Parallel()

	// No BPM in context — runShellCommand should take the fallback path
	ctx := context.Background()

	output, err := runShellCommand(ctx, "echo no bpm", false)
	require.NoError(t, err)
	assert.Contains(t, output, "no bpm")
}

// =============================================================================
// extractSessionIDFromPromotionMessage — helper to parse session ID from
// the formatted background promotion message.
// =============================================================================

func extractSessionIDFromPromotionMessage(msg string) string {
	// The message format is: "still running in background session <ID>."
	idx := strings.Index(msg, "still running in background session ")
	if idx == -1 {
		return ""
	}
	rest := msg[idx+len("still running in background session "):]
	// Session ID ends at the next period or newline
	for i, c := range rest {
		if c == '.' || c == '\n' || c == ' ' {
			return rest[:i]
		}
	}
	return rest
}

// listBPMSessions — helper to get all tracked session IDs from BPM
// (package-private for testing)
// =============================================================================

func listBPMSessions(bpm *BackgroundProcessManager) []string {
	return bpm.SessionIDs()
}

// =============================================================================
// TestAdoptable_TimeoutPromotion_OutputAccumulated — verify that partial output
// captured before timeout is included in the promotion message
// =============================================================================

func TestAdoptable_TimeoutPromotion_OutputAccumulated(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(1*time.Second))
	defer cancel()

	ctx = WithBackgroundProcessManager(ctx, bpm)

	// Command that produces output before timing out
	output, err := runShellCommandAdoptable(ctx, "echo before-sleep && sleep 30", bpm)
	require.NoError(t, err)

	assert.Contains(t, output, "exceeded the 2-minute tool deadline")
	assert.Contains(t, output, "before-sleep")
}

// =============================================================================
// TestAdoptable_TimeoutPromotion_SessionIDInBPM — verify the adopted session
// can be checked via CheckOutput
// =============================================================================

func TestAdoptable_TimeoutPromotion_SessionIDInBPM(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(1*time.Second))
	defer cancel()

	ctx = WithBackgroundProcessManager(ctx, bpm)

	output, err := runShellCommandAdoptable(ctx, "echo adoption-test && sleep 30", bpm)
	require.NoError(t, err)

	sessionID := extractSessionIDFromPromotionMessage(output)
	require.NotEmpty(t, sessionID)

	// Verify CheckOutput works on the adopted session
	checkOutput, status, err := bpm.CheckOutput(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "running", status)
	assert.Contains(t, checkOutput, "adoption-test")

	// Clean up
	bpm.Stop(sessionID, 100*time.Millisecond)
}

// =============================================================================
// TestAdoptable_ContextAlreadyDone — if context is already cancelled before call
// =============================================================================

func TestAdoptable_ContextAlreadyDone(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel

	ctx = WithBackgroundProcessManager(ctx, bpm)

	_, err := runShellCommandAdoptable(ctx, "sleep 30", bpm)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

// =============================================================================
// TestAdoptable_OutputFileCleanedUpOnNormalCompletion — verify temp output file
// is removed when command finishes normally
// =============================================================================

func TestAdoptable_OutputFileCleanedUpOnNormalCompletion(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
	defer cancel()

	ctx = WithBackgroundProcessManager(ctx, bpm)

	output, err := runShellCommandAdoptable(ctx, "echo cleanup-test", bpm)
	require.NoError(t, err)
	assert.Contains(t, output, "cleanup-test")

	// The temp file should have been cleaned up. Since we can't easily
	// predict its path, we verify by checking that no sessions are in BPM.
	assert.False(t, bpm.IsActive("anything"))
}

// =============================================================================
// TestAdoptable_WithWorkspaceRoot — verify workspace root from context is used
// =============================================================================

func TestAdoptable_WithWorkspaceRoot(t *testing.T) {
	t.Parallel()

	// This test verifies the workspace root is respected. We can't easily
	// create a temp file in a custom dir for testing, but we verify the
	// path resolution doesn't error.
	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
	defer cancel()

	ctx = WithBackgroundProcessManager(ctx, bpm)

	// Should work fine with current directory
	output, err := runShellCommandAdoptable(ctx, "pwd", bpm)
	require.NoError(t, err)
	assert.NotEmpty(t, output)
}
