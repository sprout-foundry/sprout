//go:build unix

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the duplicate-command guard's contract: a command that is
// already running as a background session is not started again; "same"
// means equal after trimming and collapsing whitespace; only a running
// session blocks; and concurrent identical starts admit exactly one.

// startBackgroundForChat starts a command through the agent-facing
// background path, in dir as the workspace root.
func startBackgroundForChat(t *testing.T, bpm *BackgroundProcessManager, command, dir string) (string, error) {
	t.Helper()
	ctx := WithBackgroundProcessManager(context.Background(), bpm)
	ctx = filesystem.WithWorkspaceRoot(ctx, dir)
	t.Cleanup(func() { bpm.StopAll() })
	return ExecuteShellCommandBackground(ctx, command, "")
}

// TestDuplicateGuard_WhitespaceVariantsAreTheSameCommand: the comparison
// ignores surrounding whitespace and whitespace runs, and nothing else.
func TestDuplicateGuard_WhitespaceVariantsAreTheSameCommand(t *testing.T) {
	tests := []struct {
		name    string
		running string
		second  string
	}{
		{"surrounding whitespace", "sleep 30", "   sleep 30  "},
		{"internal whitespace run", "sleep  30", "sleep 30"},
		{"both, with tabs and newlines", "sleep\t30", " \n sleep 30 \t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bpm := newTestBPM(t)
			dir := t.TempDir()

			firstResult, err := startBackgroundForChat(t, bpm, tt.running, dir)
			require.NoError(t, err)
			sessionID := sessionIDFromResult(t, firstResult)

			out, err := startBackgroundForChat(t, bpm, tt.second, dir)
			require.NoError(t, err, "a refusal is a normal result, not an error")
			assert.Contains(t, out, "Not started")
			assert.Contains(t, out, sessionID, "the refusal must name the running session")
			assert.NotContains(t, out, `"session_id"`, "no session may be started")
			assert.Equal(t, 1, countActiveSessions(bpm))
		})
	}
}

// TestDuplicateGuard_SimilarCommandsAreNotTheSame: anything that differs in
// its words still runs. Only whitespace is normalized.
func TestDuplicateGuard_SimilarCommandsAreNotTheSame(t *testing.T) {
	for _, command := range []string{
		"sleep 31",            // different argument
		"sleep 30 && echo hi", // chained statement
		"  sleep 31 ",         // extra flag, same whitespace shape
		"sleep '30'",          // different quoting
		"sleep 30 ; true",     // suffixed
		"sleep 30\n",          // trailing newline is whitespace: same
	} {
		// A fresh manager per case keeps the per-manager session cap out
		// of the way of what is being tested.
		bpm := newTestBPM(t)
		dir := t.TempDir()
		_, err := startBackgroundForChat(t, bpm, "sleep 30", dir)
		require.NoError(t, err)

		out, err := startBackgroundForChat(t, bpm, command, dir)
		require.NoError(t, err, command)
		wantRefused := command == "sleep 30\n"
		assert.Equal(t, wantRefused, containsNotStarted(out), "command %q", command)
	}
}

// containsNotStarted reports whether out is the duplicate refusal rather
// than a started session's JSON result.
func containsNotStarted(out string) bool {
	return strings.Contains(out, "Not started")
}

// sessionIDFromResult reads the session_id out of a background start result.
func sessionIDFromResult(t *testing.T, result string) string {
	t.Helper()
	var res struct {
		SessionID string `json:"session_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(result), &res), result)
	require.NotEmpty(t, res.SessionID)
	return res.SessionID
}

// TestDuplicateGuard_StartsOnceFinished: the guard covers the run, not the
// command forever. After the first session's command exits, the same command
// starts normally.
func TestDuplicateGuard_StartsOnceFinished(t *testing.T) {
	bpm := newTestBPM(t)
	dir := t.TempDir()

	firstID, err := startBackgroundForChat(t, bpm, "sleep 0.2", dir)
	require.NoError(t, err)

	// While running: refused.
	out, err := startBackgroundForChat(t, bpm, "sleep 0.2", dir)
	require.NoError(t, err)
	assert.True(t, containsNotStarted(out), "must refuse while the first copy runs")

	// Wait for the first command to exit.
	proc, ok := bpm.GetProcess(sessionIDFromResult(t, firstID))
	require.True(t, ok)
	select {
	case <-proc.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("first session did not finish")
	}

	out, err = startBackgroundForChat(t, bpm, "sleep 0.2", dir)
	require.NoError(t, err)
	assert.Contains(t, out, `"session_id"`, "a finished session must not block the command")
}

// TestDuplicateGuard_StoppedSessionDoesNotBlock: once stopped, the command
// starts again — stopping is one of the ways out the message offers.
func TestDuplicateGuard_StoppedSessionDoesNotBlock(t *testing.T) {
	bpm := newTestBPM(t)
	dir := t.TempDir()

	firstID, err := startBackgroundForChat(t, bpm, "sleep 30", dir)
	require.NoError(t, err)
	require.NoError(t, bpm.Stop(sessionIDFromResult(t, firstID), 2*time.Second))

	out, err := startBackgroundForChat(t, bpm, "sleep 30", dir)
	require.NoError(t, err)
	assert.Contains(t, out, `"session_id"`)
}

// TestDuplicateGuard_PromotedForegroundCountsAsRunning: a command promoted
// when it hit its tool deadline is a running session, so asking for it again
// is refused — it is the case that started this.
func TestDuplicateGuard_PromotedForegroundCountsAsRunning(t *testing.T) {
	bpm := newTestBPM(t)
	dir := t.TempDir()
	ctx := filesystem.WithWorkspaceRoot(context.Background(), dir)

	// A short deadline promotes the still-running command.
	shortCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	out, nativeErr := runShellCommandAdoptable(shortCtx, "sleep 30", bpm)
	require.NoError(t, nativeErr)
	sessionID, promoted := ParsePromotedBackgroundSession(out)
	require.True(t, promoted, "the command should have been promoted: %s", out)
	t.Cleanup(func() { bpm.StopAll() })

	// Background start of the same command: refused, naming the session.
	result, err := ExecuteShellCommandBackground(WithBackgroundProcessManager(ctx, bpm), "sleep  30", "")
	require.NoError(t, err)
	assert.Contains(t, result, "Not started")
	assert.Contains(t, result, sessionID)

	// Foreground start of the same command: refused too, promptly.
	foregroundCtx, fgCancel := context.WithTimeout(WithBackgroundProcessManager(ctx, bpm), 5*time.Second)
	defer fgCancel()
	start := time.Now()
	result, err = runShellCommandAdoptable(foregroundCtx, "sleep 30", bpm)
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 2*time.Second, "refusal must not wait for the deadline")
	assert.Contains(t, result, "Not started")
	assert.Contains(t, result, sessionID)
}

// TestDuplicateGuard_ConcurrentIdenticalStartsAdmitOne: the check and the
// start are one step, so parallel identical calls produce one session.
func TestDuplicateGuard_ConcurrentIdenticalStartsAdmitOne(t *testing.T) {
	bpm := newTestBPM(t)
	dir := t.TempDir()
	ctx := filesystem.WithWorkspaceRoot(context.Background(), dir)
	t.Cleanup(func() { bpm.StopAll() })

	const callers = 8
	results := make([]string, callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			callCtx := WithBackgroundProcessManager(ctx, bpm)
			results[i], _ = ExecuteShellCommandBackground(callCtx, "sleep 30", "")
		}(i)
	}
	close(start)
	wg.Wait()

	started := 0
	for _, out := range results {
		if strings.Contains(out, `"session_id"`) {
			started++
		}
	}
	assert.Equal(t, 1, started, "exactly one call may start a session: %v", results)
	assert.Equal(t, 1, countActiveSessions(bpm))
}

// TestDuplicateGuard_FailedStartReleasesReservation: a start that fails must
// not leave the command permanently blocked.
func TestDuplicateGuard_FailedStartReleasesReservation(t *testing.T) {
	bpm := newTestBPM(t)
	dir := t.TempDir()

	promise, busyID, _, busy := bpm.CommandPromise("sleep 30", dir)
	require.False(t, busy, busyID)
	bpm.ReleaseCommandPromise(promise)

	_, _, _, busy = bpm.CommandPromise("sleep 30", dir)
	assert.False(t, busy, "a released reservation must not block")
}

// TestDuplicateGuard_PromiseMapIsBounded: a leaked reservation must not let
// the map grow without limit. The oldest is dropped once it passes
// bgPromiseMax, so a command is never blocked forever by a leak.
func TestDuplicateGuard_PromiseMapIsBounded(t *testing.T) {
	bpm := newTestBPM(t)
	dir := t.TempDir()

	for i := 0; i < bgPromiseMax+10; i++ {
		// Distinct commands, so each is admitted rather than refused.
		promise, busyID, _, busy := bpm.CommandPromise(fmt.Sprintf("sleep %d", i+1), dir)
		require.False(t, busy, "command %d: %s", i, busyID)
		require.NotNil(t, promise)
		// Deliberately never released: this is the leak the bound covers.
	}

	bpm.mu.RLock()
	held := len(bpm.promises)
	bpm.mu.RUnlock()
	assert.LessOrEqual(t, held, bgPromiseMax, "the reservation map must stay bounded")
}

// TestDuplicateGuard_ForegroundInFlightRefusesBackgroundStart: while a
// foreground run holds its reservation, a background=true call for the same
// command must be refused with the generic already-starting message — never
// naming the reservation's pending- id, which no session answers to.
func TestDuplicateGuard_ForegroundInFlightRefusesBackgroundStart(t *testing.T) {
	bpm := newTestBPM(t)
	dir := t.TempDir()

	// Stand in for a foreground run holding its reservation for the whole
	// command (what runShellCommandAdoptable does).
	promise, busyID, _, busy := bpm.CommandPromise("npm run build", dir)
	require.False(t, busy, busyID)
	t.Cleanup(func() { bpm.ReleaseCommandPromise(promise) })

	msg := duplicateCommandMessage(pendingBusyID(t, bpm, "npm run build", dir), 3*time.Second)
	assert.Contains(t, msg, "already starting", "the refusal must say the command is starting")
	assert.NotContains(t, msg, "check_background=", "no pending id may be offered to check_background")
	assert.NotContains(t, msg, "stop_background=", "no pending id may be offered to stop_background")
	assert.NotContains(t, msg, PendingSessionIDPrefix, "the pending id must not be named")
}

// pendingBusyID returns the id a second identical reservation is refused with.
func pendingBusyID(t *testing.T, bpm *BackgroundProcessManager, command, dir string) string {
	t.Helper()
	_, id, _, busy := bpm.CommandPromise(command, dir)
	require.True(t, busy, "the second identical reservation must be refused")
	return id
}
