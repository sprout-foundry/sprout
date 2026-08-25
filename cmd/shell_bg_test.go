//go:build !js

package cmd

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Helpers
// =============================================================================

// resetShellBgGlobals saves then restores all shell-bg-related global flag
// variables that the run functions read. Call via defer.
func resetShellBgGlobals() func() {
	savedListJSON := shellBgListJSON
	savedGrace := shellBgGrace
	savedBPM := currentBPM
	savedBaseDirOverride := shellBgBaseDirOverride

	return func() {
		shellBgListJSON = savedListJSON
		shellBgGrace = savedGrace
		currentBPM = savedBPM
		shellBgBaseDirOverride = savedBaseDirOverride
	}
}

// shellBgStdoutCapture replaces os.Stdout with a pipe that drains into buf.
// Call Restore() before reading buf to ensure all data has been flushed.
type shellBgStdoutCapture struct {
	prev *os.File
	r    *os.File
	w    *os.File
	buf  *bytes.Buffer
	wg   sync.WaitGroup
}

func captureShellBgStdout(buf *bytes.Buffer) *shellBgStdoutCapture {
	s := &shellBgStdoutCapture{prev: os.Stdout, buf: buf}
	s.r, s.w, _ = os.Pipe()
	os.Stdout = s.w
	s.wg.Add(1)
	go func() {
		s.buf.ReadFrom(s.r)
		s.wg.Done()
	}()
	return s
}

func (s *shellBgStdoutCapture) Restore() {
	s.w.Close()
	os.Stdout = s.prev
	s.wg.Wait()
}

// setupShellBgTestDir creates a temp directory to use as the shell-bg baseDir
// and sets the override so runShellBg* functions find it.
func setupShellBgTestDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(tmpDir, 0o700))
	shellBgBaseDirOverride = tmpDir
	t.Cleanup(func() { shellBgBaseDirOverride = "" })
	return tmpDir
}

// Removed — writeTestPIDFileInt is the correct version below

// writeTestPIDFileInt writes a .pid file with proper integer formatting.
func writeTestPIDFileInt(t *testing.T, baseDir, sessionID string, pid int) {
	t.Helper()
	pidFile := filepath.Join(baseDir, sessionID+".pid")
	content := []byte(strconv.Itoa(pid) + "\n")
	require.NoError(t, os.WriteFile(pidFile, content, 0o600))
}

// generateTestSessionID creates a unique session ID for test isolation.
func generateTestSessionID(prefix string) string {
	b := make([]byte, 4)
	_, _ = randTestBytes(b)
	return prefix + "-" + hex.EncodeToString(b)
}

// Simple deterministic "random" for test IDs (avoids crypto/rand import).
func randTestBytes(b []byte) (int, error) {
	n := 0
	for i := range b {
		b[i] = byte(i*0x37 + 0x42)
		n++
	}
	return n, nil
}

// =============================================================================
// runShellBgList
// =============================================================================

func TestShellBgList_NoSessions(t *testing.T) {
	defer resetShellBgGlobals()
	shellBgListJSON = false
	currentBPM = nil

	_ = setupShellBgTestDir(t)
	// baseDir is empty — no .pid files

	buf := new(bytes.Buffer)
	cap := captureShellBgStdout(buf)

	err := runShellBgList()
	cap.Restore()
	require.NoError(t, err)

	// With no sessions, GlyphInfo writes to stderr (not stdout), so stdout
	// should be empty or contain only the info message.
	// The important thing is no error.
	_ = buf.String() // consumed to avoid unused variable
}

func TestShellBgList_NoSessions_JSON(t *testing.T) {
	defer resetShellBgGlobals()
	shellBgListJSON = true
	currentBPM = nil

	setupShellBgTestDir(t)

	buf := new(bytes.Buffer)
	cap := captureShellBgStdout(buf)

	err := runShellBgList()
	cap.Restore()
	require.NoError(t, err)

	got := strings.TrimSpace(buf.String())
	assert.Equal(t, "[]", got, "empty JSON array expected")
}

func TestShellBgList_WithPIDFiles(t *testing.T) {
	defer resetShellBgGlobals()
	shellBgListJSON = false
	currentBPM = nil

	baseDir := setupShellBgTestDir(t)

	// Write a .pid file with our own PID (alive process)
	sessionID := generateTestSessionID("bg-test-list")
	pidFile := filepath.Join(baseDir, sessionID+".pid")
	// Write PID as a proper integer string
	pidStr := strconv.Itoa(os.Getpid()) + "\n"
	require.NoError(t, os.WriteFile(pidFile, []byte(pidStr), 0o600))

	buf := new(bytes.Buffer)
	cap := captureShellBgStdout(buf)

	err := runShellBgList()
	cap.Restore()
	require.NoError(t, err)

	got := buf.String()
	assert.Contains(t, got, "SESSION")
	assert.Contains(t, got, sessionID)
}

func TestShellBgList_JSON_WithPIDFiles(t *testing.T) {
	defer resetShellBgGlobals()
	shellBgListJSON = true
	currentBPM = nil

	baseDir := setupShellBgTestDir(t)

	sessionID := generateTestSessionID("bg-test-json")
	pidFile := filepath.Join(baseDir, sessionID+".pid")
	pidStr := strconv.Itoa(os.Getpid()) + "\n"
	require.NoError(t, os.WriteFile(pidFile, []byte(pidStr), 0o600))

	buf := new(bytes.Buffer)
	cap := captureShellBgStdout(buf)

	err := runShellBgList()
	cap.Restore()
	require.NoError(t, err)

	got := strings.TrimSpace(buf.String())

	// Should be parseable as a JSON array
	var entries []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(got), &entries))
	assert.Len(t, entries, 1)
	assert.Equal(t, sessionID, entries[0]["session_id"])
	assert.Equal(t, "running", entries[0]["status"])
}

// =============================================================================
// runShellBgStatus
// =============================================================================

func TestShellBgStatus_UnknownSession(t *testing.T) {
	defer resetShellBgGlobals()
	currentBPM = nil

	_ = setupShellBgTestDir(t)
	// No .pid files written

	err := runShellBgStatus("nonexistent-session-xyz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestShellBgStatus_WithPIDFile(t *testing.T) {
	defer resetShellBgGlobals()
	currentBPM = nil

	baseDir := setupShellBgTestDir(t)

	sessionID := generateTestSessionID("bg-test-status")
	// Write .pid file
	pidFile := filepath.Join(baseDir, sessionID+".pid")
	pidStr := strconv.Itoa(os.Getpid()) + "\n"
	require.NoError(t, os.WriteFile(pidFile, []byte(pidStr), 0o600))

	// Write .output file
	outputFile := filepath.Join(baseDir, sessionID+".output")
	require.NoError(t, os.WriteFile(outputFile, []byte("hello from test\nline 2\n"), 0o600))

	buf := new(bytes.Buffer)
	cap := captureShellBgStdout(buf)

	err := runShellBgStatus(sessionID)
	cap.Restore()
	require.NoError(t, err)

	got := buf.String()
	assert.Contains(t, got, sessionID)
	assert.Contains(t, got, "hello from test")
	assert.Contains(t, got, "line 2")
	assert.Contains(t, got, "running")
}

// =============================================================================
// runShellBgStop
// =============================================================================

func TestShellBgStop_UnknownSession(t *testing.T) {
	defer resetShellBgGlobals()
	currentBPM = nil

	setupShellBgTestDir(t)

	err := runShellBgStop("nonexistent-session-abc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestShellBgStop_RealProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep command not available on this platform")
	}

	defer resetShellBgGlobals()
	currentBPM = nil
	shellBgGrace = 1 * time.Second // short grace for tests

	baseDir := setupShellBgTestDir(t)

	// Start a real sleep 30 subprocess
	cmd := exec.Command("sleep", "30")
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid

	// Ensure cleanup on failure
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// Write .pid file
	sessionID := generateTestSessionID("bg-test-stop")
	pidFile := filepath.Join(baseDir, sessionID+".pid")
	pidStr := strconv.Itoa(pid) + "\n"
	require.NoError(t, os.WriteFile(pidFile, []byte(pidStr), 0o600))

	// Write .output file
	outputFile := filepath.Join(baseDir, sessionID+".output")
	require.NoError(t, os.WriteFile(outputFile, []byte("sleeping...\n"), 0o600))

	// Verify process is alive before stop
	require.True(t, automate.IsProcessAlive(pid), "sleep process should be alive")

	err := runShellBgStop(sessionID)
	require.NoError(t, err)

	// Reap the zombie
	_ = cmd.Wait()

	// Verify process is dead
	assert.False(t, automate.IsProcessAlive(pid), "process should be dead after stop")

	// Verify files are cleaned up
	_, err = os.Stat(pidFile)
	assert.True(t, os.IsNotExist(err), ".pid file should be removed")
	_, err = os.Stat(outputFile)
	assert.True(t, os.IsNotExist(err), ".output file should be removed")
}

// =============================================================================
// runShellBgStopAll
// =============================================================================

func TestShellBgStopAll_NoSessions(t *testing.T) {
	defer resetShellBgGlobals()
	currentBPM = nil

	setupShellBgTestDir(t)

	err := runShellBgStopAll()
	require.NoError(t, err)
}

func TestShellBgStopAll_NonTTY_SkipsPrompt(t *testing.T) {
	// Replace stdin with a pipe (non-TTY) so the confirmation prompt is skipped.
	defer resetShellBgGlobals()
	currentBPM = nil

	baseDir := setupShellBgTestDir(t)

	// Write a .pid file with a dead PID (should just clean up)
	sessionID := generateTestSessionID("bg-test-stopall")
	pidFile := filepath.Join(baseDir, sessionID+".pid")
	// Use a PID that definitely doesn't exist
	require.NoError(t, os.WriteFile(pidFile, []byte("99999\n"), 0o600))
	outputFile := filepath.Join(baseDir, sessionID+".output")
	require.NoError(t, os.WriteFile(outputFile, []byte("output\n"), 0o600))

	// Replace stdin with a non-TTY pipe so isStdinTTY() returns false
	r, w, err := os.Pipe()
	require.NoError(t, err)
	prevStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = prevStdin
		r.Close()
		w.Close()
	})

	err = runShellBgStopAll()
	require.NoError(t, err)

	// Files should be cleaned up
	_, err = os.Stat(pidFile)
	assert.True(t, os.IsNotExist(err), ".pid file should be removed after stop-all")
	_, err = os.Stat(outputFile)
	assert.True(t, os.IsNotExist(err), ".output file should be removed after stop-all")
}

// =============================================================================
// discoverFromPIDFiles
// =============================================================================

func TestDiscoverFromPIDFiles_EmptyDir(t *testing.T) {
	baseDir := setupShellBgTestDir(t)

	entries, err := discoverFromPIDFiles(baseDir)
	require.NoError(t, err)
	assert.Len(t, entries, 0)
}

func TestDiscoverFromPIDFiles_MultipleSessions(t *testing.T) {
	baseDir := setupShellBgTestDir(t)

	// Write two .pid files with different session IDs
	for i := 0; i < 2; i++ {
		sessionID := fmt.Sprintf("bg-test-multi-%d-4279b0e7", i)
		pidFile := filepath.Join(baseDir, sessionID+".pid")
		pidStr := strconv.Itoa(os.Getpid()) + "\n"
		require.NoError(t, os.WriteFile(pidFile, []byte(pidStr), 0o600))
	}

	entries, err := discoverFromPIDFiles(baseDir)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

// =============================================================================
// loadProcessFromPIDFile
// =============================================================================

func TestLoadProcessFromPIDFile_Valid(t *testing.T) {
	baseDir := setupShellBgTestDir(t)

	sessionID := "bg-test-load-abc123"
	pidFile := filepath.Join(baseDir, sessionID+".pid")
	require.NoError(t, os.WriteFile(pidFile, []byte("12345\n"), 0o600))

	sid, pid, startedAt, err := loadProcessFromPIDFile(pidFile)
	require.NoError(t, err)
	assert.Equal(t, sessionID, sid)
	assert.Equal(t, 12345, pid)
	assert.False(t, startedAt.IsZero())
}

func TestLoadProcessFromPIDFile_Missing(t *testing.T) {
	_, _, _, err := loadProcessFromPIDFile("/tmp/nonexistent-pid-file-xyz.pid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read pid file")
}

func TestLoadProcessFromPIDFile_InvalidPID(t *testing.T) {
	baseDir := setupShellBgTestDir(t)

	sessionID := "bg-test-invalid-pid"
	pidFile := filepath.Join(baseDir, sessionID+".pid")
	require.NoError(t, os.WriteFile(pidFile, []byte("not-a-number\n"), 0o600))

	_, _, _, err := loadProcessFromPIDFile(pidFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse pid")
}

// =============================================================================
// getShellBgBaseDir
// =============================================================================

func TestGetShellBgBaseDir_Default(t *testing.T) {
	shellBgBaseDirOverride = ""
	dir := getShellBgBaseDir()
	// Should match the default from tools.GetBackgroundOutputBaseDir()
	expected := tools.GetBackgroundOutputBaseDir()
	assert.Equal(t, expected, dir)
}

func TestGetShellBgBaseDir_Override(t *testing.T) {
	shellBgBaseDirOverride = "/tmp/custom-sprout-bg"
	dir := getShellBgBaseDir()
	assert.Equal(t, "/tmp/custom-sprout-bg", dir)
	shellBgBaseDirOverride = ""
}

// =============================================================================
// BPM integration tests
// =============================================================================

func TestShellBgList_FromBPM(t *testing.T) {
	defer resetShellBgGlobals()
	shellBgListJSON = false

	// Create a BPM and set it as current
	bpm := tools.NewBackgroundProcessManager()
	currentBPM = bpm
	t.Cleanup(func() {
		currentBPM = nil
		if bpm != nil {
			bpm.Close()
		}
	})

	// Start a background process via BPM
	sessionID, err := bpm.Start(nil, "sleep 30", "")
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	cap := captureShellBgStdout(buf)

	err = runShellBgList()
	cap.Restore()
	require.NoError(t, err)

	got := buf.String()
	assert.Contains(t, got, sessionID)
	assert.Contains(t, got, "running")
}

func TestShellBgStatus_FromBPM(t *testing.T) {
	defer resetShellBgGlobals()

	// Create a BPM and set it as current
	bpm := tools.NewBackgroundProcessManager()
	currentBPM = bpm
	t.Cleanup(func() {
		currentBPM = nil
		bpm.Close()
	})

	// Start a long-running process via BPM so it's still alive when we check status.
	sessionID, err := bpm.Start(nil, "sleep 30", "")
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	cap := captureShellBgStdout(buf)

	err = runShellBgStatus(sessionID)
	cap.Restore()
	require.NoError(t, err)

	got := buf.String()
	assert.Contains(t, got, sessionID)
	assert.Contains(t, got, "sleep 30")
	assert.Contains(t, got, "running")
}

// =============================================================================
// isStdinTTY
// =============================================================================

func TestIsStdinTTY_InTest(t *testing.T) {
	// stdin may or may not be a TTY depending on the test runner environment.
	// Just verify the function doesn't panic.
	_ = isStdinTTY()
}

// =============================================================================
// Command wiring
// =============================================================================

func TestShellBgCmd_Structure(t *testing.T) {
	// Verify the command tree is properly structured
	assert.NotNil(t, shellBgCmd)
	assert.Equal(t, "shell-bg", shellBgCmd.Use)
	assert.NotEmpty(t, shellBgCmd.Short)
	assert.NotEmpty(t, shellBgCmd.Long)

	// Verify subcommands are registered
	subCmds := shellBgCmd.Commands()
	names := make([]string, 0, len(subCmds))
	for _, cmd := range subCmds {
		names = append(names, cmd.Use)
	}
	assert.Contains(t, names, "list [--json]")
	assert.Contains(t, names, "status <session_id>")
	assert.Contains(t, names, "stop <session_id> [--grace=10s]")
	assert.Contains(t, names, "stop-all")
}

func TestShellBgCmd_HelpOnNoArgs(t *testing.T) {
	// Calling the parent command with no args should show help (no error)
	err := shellBgCmd.RunE(shellBgCmd, []string{})
	require.NoError(t, err)
}

// =============================================================================
// shellBgEntry JSON serialization
// =============================================================================

func TestShellBgEntry_JSONSerialization(t *testing.T) {
	entry := shellBgEntry{
		SessionID:      "bg-test-abc123",
		PID:            12345,
		Command:        "sleep 30",
		StartedAt:      "2024-01-01T00:00:00Z",
		ElapsedSeconds: 120,
		Status:         "running",
	}

	data, err := json.Marshal(entry)
	require.NoError(t, err)

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "bg-test-abc123", decoded["session_id"])
	assert.Equal(t, float64(12345), decoded["pid"])
	assert.Equal(t, "sleep 30", decoded["command"])
	assert.Equal(t, "running", decoded["status"])
}
