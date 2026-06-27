//go:build unix

package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// CleanupOrphanedBackgroundProcesses — full lifecycle of orphan cleanup
// =============================================================================

func TestCleanupOrphanedBackgroundProcesses_LivePID(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	// Start a real process outside the BPM (no process group — orphan cleanup
	// targets individual PIDs, not groups).
	cmd := exec.Command("sleep", "30")
	require.NoError(t, cmd.Start())

	pid := cmd.Process.Pid
	sessionID := "bg-orphan-test-abcdef01"

	// Manually create .pid and .output files as if BPM started them
	pidPath := filepath.Join(baseDir, sessionID+".pid")
	outputPath := filepath.Join(baseDir, sessionID+".output")
	require.NoError(t, os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0600))
	require.NoError(t, os.WriteFile(outputPath, []byte("some output\n"), 0600))

	// Verify files exist before cleanup
	_, err := os.Stat(pidPath)
	require.NoError(t, err, "pid file should exist before cleanup")
	_, err = os.Stat(outputPath)
	require.NoError(t, err, "output file should exist before cleanup")

	// Verify process is alive before cleanup
	assert.False(t, isProcessGone(cmd.Process.Signal(syscall.Signal(0))),
		"process should be alive before cleanup")

	// Run cleanup — terminateOrphanedPID sends SIGTERM/SIGKILL (blocks ~2s)
	err = CleanupOrphanedBackgroundProcesses(baseDir)
	require.NoError(t, err)

	// Verify both files were removed
	_, err = os.Stat(pidPath)
	assert.True(t, os.IsNotExist(err), "pid file should be removed after cleanup")
	_, err = os.Stat(outputPath)
	assert.True(t, os.IsNotExist(err), "output file should be removed after cleanup")

	// Wait for the process to exit and verify the exit status is non-nil
	// (killed by signal). We call Wait on our exec.Cmd which owns the
	// process — terminateOrphanedPID already sent SIGKILL.
	waitErr := cmd.Wait()
	assert.NotNil(t, waitErr, "process should have been killed by cleanup")
}

func TestCleanupOrphanedBackgroundProcesses_DeadPID(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	// Use a PID that almost certainly doesn't exist
	nonexistentPID := 999999999
	sessionID := "bg-dead-test-12345678"

	pidPath := filepath.Join(baseDir, sessionID+".pid")
	outputPath := filepath.Join(baseDir, sessionID+".output")
	require.NoError(t, os.WriteFile(pidPath, []byte(strconv.Itoa(nonexistentPID)+"\n"), 0600))
	require.NoError(t, os.WriteFile(outputPath, []byte("stale output\n"), 0600))

	// Run cleanup — should not error even though PID doesn't exist
	err := CleanupOrphanedBackgroundProcesses(baseDir)
	require.NoError(t, err)

	// Both files should still be cleaned up (dead PID path)
	_, err = os.Stat(pidPath)
	assert.True(t, os.IsNotExist(err), "pid file should be removed for dead PID")
	_, err = os.Stat(outputPath)
	assert.True(t, os.IsNotExist(err), "output file should be removed for dead PID")
}

func TestCleanupOrphanedBackgroundProcesses_UnparseablePID(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	// Write a .pid file with garbage content
	sessionID := "bg-garbage-abcdef01"
	pidPath := filepath.Join(baseDir, sessionID+".pid")
	outputPath := filepath.Join(baseDir, sessionID+".output")
	require.NoError(t, os.WriteFile(pidPath, []byte("not-a-number\n"), 0600))
	require.NoError(t, os.WriteFile(outputPath, []byte("output\n"), 0600))

	// Should not error — unparseable PIDs are just cleaned up
	err := CleanupOrphanedBackgroundProcesses(baseDir)
	require.NoError(t, err)

	// PID file should be removed (the code removes it before continue)
	_, err = os.Stat(pidPath)
	assert.True(t, os.IsNotExist(err), "unparseable pid file should be removed")

	// The output file is NOT removed because the `continue` after the
	// unparseable PID path skips the sessionID derivation and output
	// file removal entirely. This is a known limitation of the code.
	_, err = os.Stat(outputPath)
	assert.False(t, os.IsNotExist(err),
		"output file is NOT removed for unparseable PIDs (known limitation — continue skips output cleanup)")
}

func TestCleanupOrphanedBackgroundProcesses_NoPIDFiles(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	// Directory with no .pid files — should return nil error
	err := CleanupOrphanedBackgroundProcesses(baseDir)
	require.NoError(t, err)
}

func TestCleanupOrphanedBackgroundProcesses_NonexistentBaseDir(t *testing.T) {
	t.Parallel()

	// Pass a directory that doesn't exist — it should be created
	baseDir := filepath.Join(t.TempDir(), "new-subdir", "bg-dir")

	err := CleanupOrphanedBackgroundProcesses(baseDir)
	require.NoError(t, err)

	// The directory should now exist
	info, err := os.Stat(baseDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "baseDir should have been created")
}

func TestCleanupOrphanedBackgroundProcesses_MultipleOrphans(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	// Create multiple orphan .pid/.output pairs (no process group —
	// orphan cleanup targets individual PIDs).
	var cmds []*exec.Cmd
	var sessionIDs []string
	for i := 0; i < 3; i++ {
		cmd := exec.Command("sleep", "30")
		require.NoError(t, cmd.Start())

		cmds = append(cmds, cmd)
		sid := "bg-multi-test-" + strconv.Itoa(i)
		sessionIDs = append(sessionIDs, sid)

		pidPath := filepath.Join(baseDir, sid+".pid")
		outputPath := filepath.Join(baseDir, sid+".output")
		require.NoError(t, os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0600))
		require.NoError(t, os.WriteFile(outputPath, []byte("output "+strconv.Itoa(i)+"\n"), 0600))
	}

	// Run cleanup (blocks ~2s per process for SIGTERM/SIGKILL)
	err := CleanupOrphanedBackgroundProcesses(baseDir)
	require.NoError(t, err)

	// Reap all zombie processes
	for i, cmd := range cmds {
		waitErr := cmd.Wait()
		assert.NotNil(t, waitErr, "process %d should have been killed by cleanup", i)
	}

	// Verify all files are cleaned up
	for i, sid := range sessionIDs {
		pidPath := filepath.Join(baseDir, sid+".pid")
		outputPath := filepath.Join(baseDir, sid+".output")

		_, err := os.Stat(pidPath)
		assert.True(t, os.IsNotExist(err), "pid file %d should be removed", i)
		_, err = os.Stat(outputPath)
		assert.True(t, os.IsNotExist(err), "output file %d should be removed", i)
	}
}
