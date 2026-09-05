//go:build unix

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parsePIDFile accepts the owner-aware format and the legacy single-PID
// format; malformed content errors.
func TestParsePIDFile(t *testing.T) {
	t.Parallel()

	pid, owner, err := parsePIDFile([]byte("123 456\n"))
	require.NoError(t, err)
	assert.Equal(t, 123, pid)
	assert.Equal(t, 456, owner)

	pid, owner, err = parsePIDFile([]byte("123\n"))
	require.NoError(t, err)
	assert.Equal(t, 123, pid)
	assert.Equal(t, 0, owner, "legacy single-PID format has no owner")

	_, _, err = parsePIDFile([]byte("\n"))
	assert.Error(t, err)

	_, _, err = parsePIDFile([]byte("not-a-pid\n"))
	assert.Error(t, err)
}

// A .pid file whose owner process is still alive must be skipped by
// orphan cleanup — both files left in place, no signal sent. This is the
// regression guard for the "exit code -1 + vanished output file" bug:
// a second sprout process (test binary inheriting SPROUT_CONFIG) ran
// cleanup at agent creation and reaped the first process's live sessions.
func TestCleanupOrphaned_SkipsLiveOwner(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	sid := "bg-live-aaaaaaaaaa"

	// Owner = this test process: guaranteed alive.
	pidPath := filepath.Join(baseDir, sid+".pid")
	outPath := filepath.Join(baseDir, sid+".output")
	require.NoError(t, os.WriteFile(pidPath, []byte(fmt.Sprintf("999999999 %d\n", os.Getpid())), 0600))
	require.NoError(t, os.WriteFile(outPath, []byte("precious output\n"), 0600))

	require.NoError(t, CleanupOrphanedBackgroundProcesses(baseDir))

	_, err := os.Stat(pidPath)
	assert.False(t, os.IsNotExist(err), "pid file for a live-owner session must survive cleanup")
	_, err = os.Stat(outPath)
	assert.False(t, os.IsNotExist(err), "output file for a live-owner session must survive cleanup")
}

// A .pid file whose owner is dead is a true orphan: cleaned up as before.
// PID 999999998 is not a real process; ownerProcessAlive must report
// false for it.
func TestCleanupOrphaned_DeadOwnerStillCleaned(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	sid := "bg-dead-bbbbbbbbbb"

	require.False(t, ownerProcessAlive(999999998), "unassigned high PID must report dead")

	pidPath := filepath.Join(baseDir, sid+".pid")
	outPath := filepath.Join(baseDir, sid+".output")
	require.NoError(t, os.WriteFile(pidPath, []byte("999999999 999999998\n"), 0600))
	require.NoError(t, os.WriteFile(outPath, []byte("stale\n"), 0600))

	require.NoError(t, CleanupOrphanedBackgroundProcesses(baseDir))

	_, err := os.Stat(pidPath)
	assert.True(t, os.IsNotExist(err), "dead-owner pid file should be removed")
	_, err = os.Stat(outPath)
	assert.True(t, os.IsNotExist(err), "dead-owner output file should be removed")
}

// Legacy single-PID pidfiles (pre-owner-format) keep the old behavior:
// treated as unowned orphans and cleaned.
func TestCleanupOrphaned_LegacyPidfileCleaned(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	sid := "bg-legacy-cccccccc"

	pidPath := filepath.Join(baseDir, sid+".pid")
	outPath := filepath.Join(baseDir, sid+".output")
	require.NoError(t, os.WriteFile(pidPath, []byte("999999999\n"), 0600))
	require.NoError(t, os.WriteFile(outPath, []byte("stale\n"), 0600))

	require.NoError(t, CleanupOrphanedBackgroundProcesses(baseDir))

	_, err := os.Stat(pidPath)
	assert.True(t, os.IsNotExist(err), "legacy pid file should be removed")
	_, err = os.Stat(outPath)
	assert.True(t, os.IsNotExist(err), "legacy output file should be removed")
}

// The live-owner skip is age-gated: a pidfile old enough that a recycled
// owner PID could be pinning it is reaped despite the "live" owner.
func TestCleanupOrphaned_LiveOwnerSkipAgeGated(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	sid := "bg-aged-dddddddddd"

	pidPath := filepath.Join(baseDir, sid+".pid")
	outPath := filepath.Join(baseDir, sid+".output")
	require.NoError(t, os.WriteFile(pidPath, []byte(fmt.Sprintf("999999999 %d\n", os.Getpid())), 0600))
	require.NoError(t, os.WriteFile(outPath, []byte("ancient\n"), 0600))

	// Backdate the pidfile beyond the gate.
	past := time.Now().Add(-maxLiveOwnerSkipAge - time.Hour)
	require.NoError(t, os.Chtimes(pidPath, past, past))

	require.NoError(t, CleanupOrphanedBackgroundProcesses(baseDir))

	_, err := os.Stat(pidPath)
	assert.True(t, os.IsNotExist(err), "aged pid file with live owner should still be reaped")
	_, err = os.Stat(outPath)
	assert.True(t, os.IsNotExist(err), "aged output file with live owner should still be reaped")
}

// Start() writes the owner-aware pidfile format.
func TestBPM_PIDFileOwnerAware(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "sleep 2", "")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(bpm.GetBaseDir(), sessionID+".pid"))
	require.NoError(t, err)

	pid, owner, err := parsePIDFile(data)
	require.NoError(t, err)
	assert.Greater(t, pid, 0)
	assert.Equal(t, os.Getpid(), owner, "owner must be the BPM's own process")
}

// The completion notification embeds the output tail so a reaped session
// still delivers its last output to the agent.
func TestFormatShellBgCompletion_Tail(t *testing.T) {
	t.Parallel()

	msg := formatShellBgCompletion("bg-x", 143, "BUILD FAILED")
	assert.Contains(t, msg, "exit code 143")
	assert.Contains(t, msg, "BUILD FAILED")
	assert.Contains(t, msg, `check_background="bg-x"`)

	msg = formatShellBgCompletion("bg-x", 0, "")
	assert.Contains(t, msg, "exit code 0")
	assert.NotContains(t, msg, "Last output", "empty tail should omit the output section")
}

// tailString cuts at line boundaries and keeps the last n bytes.
func TestTailString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "one\ntwo", tailString("one\ntwo", 100))
	assert.Equal(t, "two", tailString("one\ntwo", 3))
	assert.Equal(t, "", tailString("", 10))
	assert.Equal(t, "", tailString("\n\n\n", 2), "only newlines should trim to empty")
	long := strings.Repeat("a", 3000) + "\nend\n"
	assert.Equal(t, "end", tailString(long, 10))
}

// End-to-end: a live BPM session completes; the watcher notification
// carries the output tail read from the output file.
func TestStartWakeupWatcher_NotificationIncludesTail(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "echo tail-marker-123", "")
	require.NoError(t, err)

	ctx := WithBackgroundProcessManager(context.Background(), bpm)
	notifier := &fakeWakeupNotifier{}

	h := &shellCommandHandler{}
	resultJSON := fmt.Sprintf(`{"session_id":%q,"status":"running"}`, sessionID)
	h.startWakeupWatcher(ctx, ToolEnv{Notifier: notifier}, resultJSON, 0, "make build")

	deadline := time.After(5 * time.Second)
	for {
		calls := notifier.of("shell_bg")
		if len(calls) == 1 {
			assert.Contains(t, calls[0].content, "tail-marker-123",
				"completion notification must embed the output tail")
			assert.Contains(t, calls[0].content, "exit code 0")
			return
		}
		select {
		case <-deadline:
			t.Fatal("completion notification never arrived")
		case <-time.After(20 * time.Millisecond):
		}
	}
}
