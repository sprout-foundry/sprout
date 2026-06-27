//go:build !js

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Integration tests for the timeout-promotion path.
//
// Unlike the unit tests in shell_adoptable_test.go which call
// runShellCommandAdoptable() directly, these tests exercise runShellCommand()
// — the public entry point — to verify the full routing: context BPM lookup,
// decision between adoptable/fallback paths, and promotion message formatting.
// =============================================================================

// ---------------------------------------------------------------------------
// TestIntegration_TimeoutPromotionViaRunShellCommand
//
// Verifies the full flow: runShellCommand detects BPM in context, delegates to
// the adoptable path, times out, and returns the formatted promotion message.
// ---------------------------------------------------------------------------

func TestIntegration_TimeoutPromotionViaRunShellCommand(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// 1-second deadline — sleep 150 will definitely exceed it.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(1*time.Second))
	defer cancel()

	ctx = WithBackgroundProcessManager(ctx, bpm)

	output, err := runShellCommand(ctx, "sleep 150", false)
	require.NoError(t, err, "runShellCommand should return no error on timeout promotion")

	// The promotion message should contain expected phrases.
	assert.Contains(t, output, "exceeded the 2-minute tool deadline")
	assert.Contains(t, output, "still running in background session")

	// Extract the session ID and verify the session is tracked in BPM.
	sessionID := extractSessionIDFromPromotionMessage(output)
	require.NotEmpty(t, sessionID, "should find session ID in promotion message")
	assert.True(t, bpm.IsActive(sessionID), "session %s should be active in BPM after promotion", sessionID)

	// Clean up the adopted session.
	require.NoError(t, bpm.Stop(sessionID, 100*time.Millisecond))
}

// ---------------------------------------------------------------------------
// TestIntegration_FastCommandReturnsSynchronously
//
// Verifies that a fast command completes within its deadline and does NOT get
// promoted to background — no sessions should remain in the BPM.
// ---------------------------------------------------------------------------

func TestIntegration_FastCommandReturnsSynchronously(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// 5-second deadline — echo will finish well within it.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
	defer cancel()

	ctx = WithBackgroundProcessManager(ctx, bpm)

	output, err := runShellCommand(ctx, "echo hello", false)
	require.NoError(t, err)
	assert.Contains(t, output, "hello")

	// No background sessions should have been created.
	sessions := bpm.SessionIDs()
	assert.Empty(t, sessions, "fast command should not create any background sessions")
}

// ---------------------------------------------------------------------------
// TestIntegration_CheckBackgroundOutputAfterPromotion
//
// After a command is promoted to background via timeout, verify that the
// public CheckBackgroundOutput() function returns valid JSON with the
// expected status and partial output.
// ---------------------------------------------------------------------------

func TestIntegration_CheckBackgroundOutputAfterPromotion(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Promote a slow command that produces output before timing out.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(1*time.Second))
	defer cancel()

	ctx = WithBackgroundProcessManager(ctx, bpm)

	output, err := runShellCommand(ctx, "echo adoption-marker && sleep 30", false)
	require.NoError(t, err)

	sessionID := extractSessionIDFromPromotionMessage(output)
	require.NotEmpty(t, sessionID)

	// Now call the public CheckBackgroundOutput function (which returns JSON).
	checkResult, err := CheckBackgroundOutput(ctx, sessionID)
	require.NoError(t, err)

	// Parse the JSON response.
	var result map[string]string
	require.NoError(t, json.Unmarshal([]byte(checkResult), &result))

	assert.Equal(t, "running", result["status"], "promoted session should report running status")
	assert.Equal(t, sessionID, result["session_id"])
	assert.Contains(t, result["output"], "adoption-marker", "output should contain the echoed marker")

	// Clean up.
	require.NoError(t, bpm.Stop(sessionID, 100*time.Millisecond))
}

// ---------------------------------------------------------------------------
// TestIntegration_StopBackgroundKillsProcess
//
// After promoting a slow command, verify that bpm.Stop() actually kills the
// process and that CheckBackgroundOutput reflects the exited state.
// ---------------------------------------------------------------------------

func TestIntegration_StopBackgroundKillsProcess(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Promote a slow command.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(1*time.Second))
	defer cancel()

	ctx = WithBackgroundProcessManager(ctx, bpm)

	output, err := runShellCommand(ctx, "sleep 30", false)
	require.NoError(t, err)

	sessionID := extractSessionIDFromPromotionMessage(output)
	require.NotEmpty(t, sessionID)

	// Verify it's active before stopping.
	assert.True(t, bpm.IsActive(sessionID))

	// Stop the session with a short grace period.
	require.NoError(t, bpm.Stop(sessionID, 100*time.Millisecond))

	// Give the monitor goroutine time to update state.
	time.Sleep(200 * time.Millisecond)

	// Now it should be inactive.
	assert.False(t, bpm.IsActive(sessionID), "session should not be active after stop")

	// CheckBackgroundOutput should reflect exited status.
	checkResult, err := CheckBackgroundOutput(ctx, sessionID)
	require.NoError(t, err)

	var result map[string]string
	require.NoError(t, json.Unmarshal([]byte(checkResult), &result))
	assert.Equal(t, "exited", result["status"], "stopped session should report exited status")
}

// ---------------------------------------------------------------------------
// TestIntegration_OutputAccumulationAfterPromotion
//
// After promoting a long-running command that produces periodic output,
// verify that output continues to accumulate over time in the background
// session's output file.
// ---------------------------------------------------------------------------

func TestIntegration_OutputAccumulationAfterPromotion(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Promote a command that echoes lines with 1-second delays.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(1*time.Second))
	defer cancel()

	ctx = WithBackgroundProcessManager(ctx, bpm)

	cmd := "for i in 1 2 3 4 5; do echo \"line $i\"; sleep 1; done"
	output, err := runShellCommand(ctx, cmd, false)
	require.NoError(t, err)

	sessionID := extractSessionIDFromPromotionMessage(output)
	require.NotEmpty(t, sessionID)
	assert.True(t, bpm.IsActive(sessionID))

	// Wait a few seconds for the background process to produce more output.
	time.Sleep(3 * time.Second)

	// Read output via BPM's CheckOutput.
	out1, status1, err := bpm.CheckOutput(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "running", status1)

	lineCount1 := strings.Count(out1, "line ")
	assert.Greater(t, lineCount1, 0, "should have captured at least some output lines")

	// Wait a bit more for additional lines.
	time.Sleep(3 * time.Second)

	out2, status2, err := bpm.CheckOutput(sessionID)
	require.NoError(t, err)
	// The 5-iteration loop (~5s total) may have finished by our 7s check;
	// either status is fine — what matters is that output grew.
	assert.Contains(t, []string{"running", "exited"}, status2,
		"process should have run to completion or still be running: got %s", status2)

	lineCount2 := strings.Count(out2, "line ")
	assert.Greater(t, lineCount2, lineCount1,
		"output should grow over time: had %d lines, now has %d", lineCount1, lineCount2)

	// Clean up.
	require.NoError(t, bpm.Stop(sessionID, 100*time.Millisecond))
}

// ---------------------------------------------------------------------------
// TestIntegration_NoPromotionWithoutBPM
//
// Verify that runShellCommand falls back to the standard CommandContext path
// when no BPM is in the context — no background promotion, normal output.
// ---------------------------------------------------------------------------

func TestIntegration_NoPromotionWithoutBPM(t *testing.T) {
	t.Parallel()

	// No BPM in context — just a plain background context.
	ctx := context.Background()

	output, err := runShellCommand(ctx, "echo test", false)
	require.NoError(t, err)
	assert.Contains(t, output, "test")

	// Since there's no BPM we can check, just verify we got normal output
	// and didn't get a promotion message.
	assert.NotContains(t, output, "still running in background session",
		"without BPM, no promotion message should appear")
}
