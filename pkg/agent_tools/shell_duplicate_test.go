//go:build unix

package tools

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The duplicate-command guard: an agent that hits its tool deadline gets its
// command promoted into a background session that keeps running. If it then
// runs the same command again — same text, same directory — the second copy
// is refused, because on a small machine the copies starve it until even
// `echo hi` stops answering. Anything that differs at all still runs.

// countActiveSessions counts sessions whose process is still running.
func countActiveSessions(bpm *BackgroundProcessManager) int {
	bpm.mu.RLock()
	ids := make([]string, 0, len(bpm.processes))
	for id := range bpm.processes {
		ids = append(ids, id)
	}
	bpm.mu.RUnlock()

	active := 0
	for _, id := range ids {
		if bpm.IsActive(id) {
			active++
		}
	}
	return active
}

func startLongRunningSession(t *testing.T, bpm *BackgroundProcessManager, command, dir string) string {
	t.Helper()
	sessionID, err := bpm.Start(context.Background(), command, dir)
	require.NoError(t, err)
	t.Cleanup(func() { bpm.StopAll() })
	return sessionID
}

func TestRunningSessionMatching_RefusesIdenticalCommand(t *testing.T) {
	bpm := newTestBPM(t)
	dir := t.TempDir()
	sessionID := startLongRunningSession(t, bpm, "sleep 30", dir)

	gotID, elapsed, busy := bpm.RunningSessionMatching("  sleep 30  ", dir)
	require.True(t, busy, "identical command in the same directory must match")
	assert.Equal(t, sessionID, gotID)
	assert.GreaterOrEqual(t, elapsed, time.Duration(0))
}

func TestRunningSessionMatching_RelativeDirMatchesAbsolute(t *testing.T) {
	bpm := newTestBPM(t)
	dir := t.TempDir()
	sessionID := startLongRunningSession(t, bpm, "sleep 30", dir)

	gotID, _, busy := bpm.RunningSessionMatching("sleep 30", dir+string(filepath.Separator)+".")
	require.True(t, busy)
	assert.Equal(t, sessionID, gotID)
}

func TestRunningSessionMatching_DifferentCommandOrDirRuns(t *testing.T) {
	bpm := newTestBPM(t)
	dir := t.TempDir()
	startLongRunningSession(t, bpm, "sleep 30", dir)

	otherDir := t.TempDir()
	tests := []struct {
		name    string
		command string
		dir     string
	}{
		{"extra argument", "sleep 30 && echo done", dir},
		{"extra flag", "sleep 30 -x", dir},
		{"different command", "sleep 31", dir},
		{"different directory", "sleep 30", otherDir},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, busy := bpm.RunningSessionMatching(tt.command, tt.dir)
			assert.False(t, busy, "a command that differs in any way must run")
		})
	}
}

// A session that has finished must not block the next run of the same command.
func TestRunningSessionMatching_IgnoresFinishedSession(t *testing.T) {
	bpm := newTestBPM(t)
	dir := t.TempDir()
	sessionID, err := bpm.Start(context.Background(), "echo done", dir)
	require.NoError(t, err)

	proc, ok := bpm.GetProcess(sessionID)
	require.True(t, ok)
	select {
	case <-proc.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("session did not finish")
	}

	_, _, busy := bpm.RunningSessionMatching("echo done", dir)
	assert.False(t, busy)
}

// The agent-facing message names the session, its age, and the ways out.
func TestDuplicateCommandMessage(t *testing.T) {
	msg := duplicateCommandMessage("bg-shell-1234abcd", 90*time.Second)

	assert.Contains(t, msg, "bg-shell-1234abcd")
	assert.Contains(t, msg, "1 minute 30 seconds")
	assert.Contains(t, msg, `check_background="bg-shell-1234abcd"`)
	assert.Contains(t, msg, `stop_background="bg-shell-1234abcd"`)
	assert.Contains(t, msg, "Do not run it again")
}

// A start still in flight has no addressable session, so the message must not
// name its pending id: check_background and stop_background would find
// nothing. It says the command is starting instead.
func TestDuplicateCommandMessage_PendingStartNamesNoSession(t *testing.T) {
	for _, id := range []string{"", PendingSessionIDPrefix + "1", PendingSessionIDPrefix + "42"} {
		msg := duplicateCommandMessage(id, 5*time.Second)

		assert.Contains(t, msg, "already starting", "id %q", id)
		assert.NotContains(t, msg, "check_background=", "id %q must not invite check_background", id)
		assert.NotContains(t, msg, "stop_background=", "id %q must not invite stop_background", id)
		if id != "" {
			assert.NotContains(t, msg, id, "the pending id must not be named")
		}
	}
}

func TestIsPendingSessionID(t *testing.T) {
	assert.True(t, IsPendingSessionID(PendingSessionIDPrefix+"7"))
	assert.False(t, IsPendingSessionID("bg-shell-1234abcd"))
	assert.False(t, IsPendingSessionID(""))
}

func TestFormatSessionElapsed(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0 seconds"},
		{time.Second, "1 second"},
		{45 * time.Second, "45 seconds"},
		{time.Minute, "1 minute"},
		{2 * time.Minute, "2 minutes"},
		{90 * time.Second, "1 minute 30 seconds"},
		{5*time.Minute + 7*time.Second, "5 minutes 7 seconds"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, formatSessionElapsed(tt.d), "duration %s", tt.d)
	}
}

// shell_command with background=true: the second copy is refused and starts
// no process.
func TestExecuteShellCommandBackground_RefusesDuplicate(t *testing.T) {
	bpm := newTestBPM(t)
	dir := t.TempDir()
	sessionID := startLongRunningSession(t, bpm, "sleep 30", dir)

	ctx := WithBackgroundProcessManager(context.Background(), bpm)
	ctx = filesystem.WithWorkspaceRoot(ctx, dir)

	out, err := ExecuteShellCommandBackground(ctx, "sleep 30", "")
	require.NoError(t, err)
	assert.Contains(t, out, sessionID)
	assert.Contains(t, out, "already running")
	assert.NotContains(t, out, "session_id", "refusal must not look like a started session")
	assert.Equal(t, 1, countActiveSessions(bpm), "no second process may be started")

	// A different command still starts.
	out, err = ExecuteShellCommandBackground(ctx, "sleep 31", "")
	require.NoError(t, err)
	assert.Contains(t, out, `"session_id"`)
}

// shell_command in the foreground: the same refusal applies before the
// process is started, whether or not the context has a deadline.
func TestRunShellCommandAdoptable_RefusesDuplicate(t *testing.T) {
	bpm := newTestBPM(t)
	dir := t.TempDir()
	sessionID := startLongRunningSession(t, bpm, "sleep 30", dir)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx = filesystem.WithWorkspaceRoot(ctx, dir)

	start := time.Now()
	out, err := runShellCommandAdoptable(ctx, "sleep 30", bpm)
	require.NoError(t, err)
	assert.Less(t, time.Since(start), time.Second, "refusal must not wait for the deadline")
	assert.Contains(t, out, sessionID)
	assert.Contains(t, out, "already running")
	assert.Equal(t, 1, countActiveSessions(bpm))

	out, err = runShellCommandAdoptable(ctx, "echo hi", bpm)
	require.NoError(t, err)
	assert.Contains(t, out, "hi", "a different command still runs")
}
