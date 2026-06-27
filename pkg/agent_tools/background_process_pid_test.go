//go:build unix

package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// TestBPM_PIDFileWritten — Start() writes a .pid file alongside .output
// =============================================================================

func TestBPM_PIDFileWritten(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "sleep 5", "")
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)

	// Verify the .pid file exists at the expected path
	pidPath := filepath.Join(bpm.GetBaseDir(), sessionID+".pid")
	info, err := os.Stat(pidPath)
	require.NoError(t, err, "pid file should exist at %s", pidPath)
	assert.False(t, info.IsDir(), "pid file should be a regular file, not a directory")

	// Read and parse the PID
	data, err := os.ReadFile(pidPath)
	require.NoError(t, err)

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	require.NoError(t, err, "pid file should contain a valid decimal integer")
	assert.Greater(t, pid, 0, "pid should be a positive integer")
}

// =============================================================================
// TestBPM_PIDFileMatchesProcessPID — PID in file matches BackgroundProcess.GetPID()
// =============================================================================

func TestBPM_PIDFileMatchesProcessPID(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "sleep 5", "")
	require.NoError(t, err)

	// Read PID from file
	pidPath := filepath.Join(bpm.GetBaseDir(), sessionID+".pid")
	data, err := os.ReadFile(pidPath)
	require.NoError(t, err)

	filePID, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)

	// Get PID from process object
	proc, found := bpm.GetProcess(sessionID)
	require.True(t, found, "GetProcess should find the session")
	require.NotNil(t, proc, "process should not be nil")

	procPID := proc.GetPID()
	assert.Equal(t, filePID, procPID, "PID in .pid file should match BackgroundProcess.GetPID()")
	assert.Greater(t, procPID, 0, "process PID should be a positive integer")
}

// =============================================================================
// TestBPM_PIDFileFormat — PID file contains only decimal digits and a newline
// =============================================================================

func TestBPM_PIDFileFormat(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.Start(context.Background(), "sleep 5", "")
	require.NoError(t, err)

	pidPath := filepath.Join(bpm.GetBaseDir(), sessionID+".pid")
	data, err := os.ReadFile(pidPath)
	require.NoError(t, err)

	pidStr := strings.TrimSpace(string(data))

	// Every character should be a digit
	for i, c := range pidStr {
		assert.GreaterOrEqual(t, c, rune('0'), "char %d (%q) should be a digit", i, c)
		assert.LessOrEqual(t, c, rune('9'), "char %d (%q) should be a digit", i, c)
	}

	assert.NotEmpty(t, pidStr, "pid file should not be empty")
}

// =============================================================================
// TestBPM_PIDFileAfterAdopt — AdoptProcess also writes a .pid file
// =============================================================================

func TestBPM_PIDFileAfterAdopt(t *testing.T) {
	t.Parallel()

	// Skip in short mode — requires manual exec.Cmd setup
	if testing.Short() {
		t.Skip("skipping adopt PID test in short mode")
	}

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-c", "sleep 5")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	outputFile, err := os.CreateTemp("", "sprout-bg-*.output")
	require.NoError(t, err)
	outputPath := outputFile.Name()
	defer os.Remove(outputPath)

	cmd.Stdout = outputFile
	cmd.Stderr = outputFile

	require.NoError(t, cmd.Start())
	require.NotNil(t, cmd.Process)

	outputFile.Close()
	sessionID, err := bpm.AdoptProcess(cmd, outputPath, "sleep 5", cmd.Dir, nil)
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)

	// Verify the .pid file exists at the expected path
	pidPath := filepath.Join(bpm.GetBaseDir(), sessionID+".pid")
	data, err := os.ReadFile(pidPath)
	require.NoError(t, err, "pid file should exist after AdoptProcess")

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	require.NoError(t, err, "pid file should contain a valid integer")
	assert.Equal(t, cmd.Process.Pid, pid, "PID in file should match the adopted cmd's process PID")
}
