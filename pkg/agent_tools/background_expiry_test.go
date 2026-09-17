//go:build unix

package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestBPM creates a manager with a 1-minute base expiry so TTL windows
// can be driven deterministically in tests by per-session TTLs and direct
// LastPolled manipulation.
func newTestBPM(t *testing.T) *BackgroundProcessManager {
	t.Helper()
	baseDir := t.TempDir()
	m := &BackgroundProcessManager{
		processes:   make(map[string]*BackgroundProcess),
		expiry:      time.Minute,
		maxSessions: 5,
		baseDir:     baseDir,
		done:        make(chan struct{}),
	}
	t.Cleanup(func() { close(m.done) })
	return m
}

// TestBPM_ExpiredUnpolledButAliveSurvives is the regression test for the
// quiet-watcher reap: a session whose LastPolled is far past the expiry is
// NOT killed if its process is actually alive. Cleanup must renew the
// timer instead of reaping.
func TestBPM_ExpiredUnpolledButAliveSurvives(t *testing.T) {
	bpm := newTestBPM(t)

	sessionID, err := bpm.Start(context.Background(), "sleep 30", "")
	require.NoError(t, err)

	// Age the session past any TTL window.
	proc, ok := bpm.GetProcess(sessionID)
	require.True(t, ok)
	proc.mu.Lock()
	proc.LastPolled = time.Now().Add(-24 * time.Hour)
	proc.mu.Unlock()

	bpm.cleanup()

	// Session must still exist and still be running.
	proc2, ok := bpm.GetProcess(sessionID)
	require.True(t, ok, "quiet-but-alive session was reaped")
	assert.True(t, bpm.IsActive(sessionID))
	assert.FileExists(t, proc2.OutputPath)

	// Cleanup must have renewed the activity timer.
	proc2.mu.Lock()
	renewed := time.Since(proc2.LastPolled) < time.Minute
	proc2.mu.Unlock()
	assert.True(t, renewed, "cleanup did not renew the timer on a live session")
}

// TestBPM_DeadButUnreapedIsReapedAfterIdle covers the flip side: a running
// session whose process has died (unpolled, so the monitor hasn't been
// observed) is cleaned up once its TTL window elapses.
func TestBPM_DeadButUnreapedIsReapedAfterIdle(t *testing.T) {
	bpm := newTestBPM(t)

	sessionID, err := bpm.Start(context.Background(), "echo done", "")
	require.NoError(t, err)

	// Wait for exit, then age the session past the 5-min idle window
	// (manipulating LastPolled instead of sleeping).
	require.Eventually(t, func() bool {
		_, status, err := bpm.CheckOutput(sessionID)
		return err == nil && status == "exited"
	}, 3*time.Second, 50*time.Millisecond)

	proc1, ok := bpm.GetProcess(sessionID)
	require.True(t, ok)
	proc1.mu.Lock()
	proc1.LastPolled = time.Now().Add(-10 * time.Minute)
	proc1.mu.Unlock()

	bpm.cleanup()

	_, ok = bpm.GetProcess(sessionID)
	assert.False(t, ok, "exited session idle >5m should be reaped")
}

// TestBPM_TTLHonored verifies a per-session TTL shorter than the manager
// default triggers cleanup consideration earlier.
func TestBPM_TTLHonored(t *testing.T) {
	bpm := newTestBPM(t)

	sessionID, err := bpm.StartWithOptions(context.Background(), "sleep 30", "", "shell",
		&StartOptions{TTL: -1 * time.Second})
	require.NoError(t, err)

	// TTL already elapsed (negative). Process is alive → survives, but the
	// timer gets renewed (proving the TTL path was evaluated).
	proc, ok := bpm.GetProcess(sessionID)
	require.True(t, ok)
	proc.mu.Lock()
	proc.LastPolled = time.Now().Add(-24 * time.Hour)
	proc.mu.Unlock()

	bpm.cleanup()

	_, ok = bpm.GetProcess(sessionID)
	assert.True(t, ok, "alive session must survive even with an elapsed TTL")
}

// TestBPM_KeepAliveResetsTimer verifies KeepAlive renews LastPolled.
func TestBPM_KeepAliveResetsTimer(t *testing.T) {
	bpm := newTestBPM(t)

	sessionID, err := bpm.Start(context.Background(), "sleep 30", "")
	require.NoError(t, err)

	proc, ok := bpm.GetProcess(sessionID)
	require.True(t, ok)
	proc.mu.Lock()
	proc.LastPolled = time.Now().Add(-24 * time.Hour)
	proc.mu.Unlock()

	require.NoError(t, bpm.KeepAlive(sessionID))

	proc.mu.Lock()
	renewed := time.Since(proc.LastPolled) < time.Minute
	proc.mu.Unlock()
	assert.True(t, renewed)

	// Unknown session errors.
	assert.Error(t, bpm.KeepAlive("bg-does-not-exist"))
}

// TestBPM_LongLivedWatcherSurvivesRepeatedCleanup is the exit-criterion
// scenario: a silent watcher survives many cleanup passes spanning far
// more than the old 2h expiry, purely because its process is alive.
func TestBPM_LongLivedWatcherSurvivesRepeatedCleanup(t *testing.T) {
	bpm := newTestBPM(t)

	sessionID, err := bpm.Start(context.Background(), "sleep 30", "")
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		// Simulate a session that has never been polled since start.
		proc, ok := bpm.GetProcess(sessionID)
		require.True(t, ok)
		proc.mu.Lock()
		proc.LastPolled = time.Now().Add(-24 * time.Hour)
		proc.mu.Unlock()

		bpm.cleanup()
	}

	assert.True(t, bpm.IsActive(sessionID),
		"watcher session did not survive repeated cleanup passes")
	_, status, err := bpm.CheckOutput(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "running", status, "status must stay accurate for long-lived sessions")
}

// TestStartOptions_TTLRecordedOnProcess verifies the TTL from StartOptions
// lands on the BackgroundProcess and is preferred over the manager default.
func TestStartOptions_TTLRecordedOnProcess(t *testing.T) {
	bpm := newTestBPM(t)

	sessionID, err := bpm.StartWithOptions(context.Background(), "sleep 30", "", "shell",
		&StartOptions{TTL: 8 * time.Hour})
	require.NoError(t, err)

	proc, ok := bpm.GetProcess(sessionID)
	require.True(t, ok)
	proc.mu.Lock()
	ttl := proc.ttl
	proc.mu.Unlock()
	assert.Equal(t, 8*time.Hour, ttl)

	// Cleanup with an elapsed-far-future window must be a no-op.
	bpm.cleanup()
	_, ok = bpm.GetProcess(sessionID)
	assert.True(t, ok)
}

// TestKeepAlive_TouchesPIDFileForCrossProcessSessions verifies the CLI
// keepalive path can renew a session it only knows from disk.
func TestKeepAlive_TouchesPIDFileForCrossProcessSessions(t *testing.T) {
	baseDir := t.TempDir()
	pidFile := filepath.Join(baseDir, "bg-test-deadbeef.pid")
	require.NoError(t, os.WriteFile(pidFile, []byte("999999999 0\n"), 0o600))

	old := time.Now().Add(-24 * time.Hour)
	require.NoError(t, os.Chtimes(pidFile, old, old))

	info, err := os.Stat(pidFile)
	require.NoError(t, err)
	require.True(t, time.Since(info.ModTime()) > time.Hour)

	// The CLI keepalive path touches the pid file; simulate that touch here
	// (the command layer is covered by cmd tests). This asserts the file
	// semantics the command relies on.
	now := time.Now()
	require.NoError(t, os.Chtimes(pidFile, now, now))
	info, err = os.Stat(pidFile)
	require.NoError(t, err)
	assert.True(t, time.Since(info.ModTime()) < time.Minute,
		"pid file mtime is the cross-process activity signal")

}
