//go:build !js

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Helpers
// =============================================================================

// resetAutomateGlobals saves then restores all automate-related global flag
// variables that the run functions read. Call via defer.
func resetAutomateGlobals() func() {
	savedAll := automateStatusAll
	savedJSON := automateStatusJSON
	savedStopAll := automateStopAll
	savedLogsFollow := automateLogsFollow
	savedLogsLines := automateLogsLines
	savedDir := automateDir
	savedAssumeYes := automateAssumeYes
	savedBudgetUSD := automateBudgetUSD
	savedBudgetWarn := automateBudgetWarn
	savedHeartbeat := automateHeartbeatSeconds

	return func() {
		automateStatusAll = savedAll
		automateStatusJSON = savedJSON
		automateStopAll = savedStopAll
		automateLogsFollow = savedLogsFollow
		automateLogsLines = savedLogsLines
		automateDir = savedDir
		automateAssumeYes = savedAssumeYes
		automateBudgetUSD = savedBudgetUSD
		automateBudgetWarn = savedBudgetWarn
		automateHeartbeatSeconds = savedHeartbeat
	}
}

// automateStdoutCapture replaces os.Stdout with a pipe that drains into buf.
// Call Restore() before reading buf to ensure all data has been flushed.
type automateStdoutCapture struct {
	prev *os.File
	r    *os.File
	w    *os.File
	buf  *bytes.Buffer
	wg   sync.WaitGroup
}

func captureAutomateStdout(buf *bytes.Buffer) *automateStdoutCapture {
	s := &automateStdoutCapture{prev: os.Stdout, buf: buf}
	s.r, s.w, _ = os.Pipe()
	os.Stdout = s.w
	s.wg.Add(1)
	go func() {
		s.buf.ReadFrom(s.r)
		s.wg.Done()
	}()
	return s
}

func (s *automateStdoutCapture) Restore() {
	s.w.Close()
	os.Stdout = s.prev
	s.wg.Wait()
}

// setupTestSproutDir creates a temp directory with .sprout/automate/ and
// changes the working directory into it so run* functions find the sessions.
func setupTestSproutDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	sproutDir := filepath.Join(tmpDir, ".sprout")
	require.NoError(t, os.MkdirAll(filepath.Join(sproutDir, "automate"), 0o700))

	// Change CWD so os.Getwd() returns tmpDir
	origWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { os.Chdir(origWd) })

	return sproutDir
}

func writeTestSession(t *testing.T, sproutDir, sessionID string, pid int) {
	t.Helper()
	info := &automate.AutomateSessionInfo{
		Workflow:  "test-workflow",
		PID:       pid,
		StartedAt: time.Now().Add(-30 * time.Second),
		Kind:      "automate",
	}
	require.NoError(t, automate.WriteSessionFile(sproutDir, sessionID, info))
}

func writeTestSessionWithOutput(t *testing.T, sproutDir, sessionID, outputFilePath string, pid int) {
	t.Helper()
	info := &automate.AutomateSessionInfo{
		Workflow:       "test-workflow",
		PID:            pid,
		StartedAt:      time.Now().Add(-30 * time.Second),
		OutputFilePath: outputFilePath,
		Kind:           "automate",
	}
	require.NoError(t, automate.WriteSessionFile(sproutDir, sessionID, info))
}

// =============================================================================
// runAutomateStatus
// =============================================================================

func TestAutomateStatus_NoSessions(t *testing.T) {
	defer resetAutomateGlobals()
	automateStatusAll = false
	automateStatusJSON = false

	sproutDir := setupTestSproutDir(t)
	// Remove the automate directory entirely — no sessions at all
	require.NoError(t, os.RemoveAll(filepath.Join(sproutDir, "automate")))

	// No sessions means no stdout output (GlyphInfo goes to stderr);
	// verify the function completes without error.
	err := runAutomateStatus()
	require.NoError(t, err)

	// Verify readAllSessions returns nil when dir is missing
	sessions, err := readAllSessions(sproutDir)
	require.NoError(t, err)
	assert.Nil(t, sessions)
}

func TestAutomateStatus_RunningSession(t *testing.T) {
	defer resetAutomateGlobals()
	automateStatusAll = false
	automateStatusJSON = false

	sproutDir := setupTestSproutDir(t)
	writeTestSession(t, sproutDir, "cli-automate-abc123", os.Getpid())

	buf := new(bytes.Buffer)
	cap := captureAutomateStdout(buf)

	err := runAutomateStatus()
	cap.Restore()
	require.NoError(t, err)

	got := buf.String()
	assert.Contains(t, got, "cli-automate-abc123")
	assert.Contains(t, got, "running")
	assert.Contains(t, got, "test-workflow")
}

func TestAutomateStatus_ExitedSession(t *testing.T) {
	defer resetAutomateGlobals()
	automateStatusAll = false
	automateStatusJSON = false

	sproutDir := setupTestSproutDir(t)
	writeTestSession(t, sproutDir, "cli-automate-dead99", 99999)

	// Exited-only sessions get filtered by the default (no --all) path.
	// runAutomateStatus returns nil but writes nothing to stdout (message
	// goes to stderr via GlyphInfo). Verify no error and that the session
	// would be filtered out.
	err := runAutomateStatus()
	require.NoError(t, err)

	// Confirm the filtering logic: only alive PIDs should remain
	sessions, err := readAllSessions(sproutDir)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.False(t, automate.IsProcessAlive(sessions[0].PID))
}

func TestAutomateStatus_AllFlag(t *testing.T) {
	defer resetAutomateGlobals()
	automateStatusAll = true
	automateStatusJSON = false

	sproutDir := setupTestSproutDir(t)
	writeTestSession(t, sproutDir, "cli-automate-live", os.Getpid())
	writeTestSession(t, sproutDir, "cli-automate-dead", 99999)

	buf := new(bytes.Buffer)
	cap := captureAutomateStdout(buf)

	err := runAutomateStatus()
	cap.Restore()
	require.NoError(t, err)

	got := buf.String()
	assert.Contains(t, got, "cli-automate-live")
	assert.Contains(t, got, "running")
	assert.Contains(t, got, "cli-automate-dead")
	assert.Contains(t, got, "exited")
}

func TestAutomateStatus_JsonOutput(t *testing.T) {
	defer resetAutomateGlobals()
	automateStatusAll = false
	automateStatusJSON = true

	sproutDir := setupTestSproutDir(t)
	writeTestSession(t, sproutDir, "cli-automate-json123", os.Getpid())

	buf := new(bytes.Buffer)
	cap := captureAutomateStdout(buf)

	err := runAutomateStatus()
	cap.Restore()
	require.NoError(t, err)

	got := strings.TrimSpace(buf.String())
	assert.NotEmpty(t, got)

	// Should be parseable as a JSON array
	var entries []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(got), &entries))
	assert.Len(t, entries, 1)
	assert.Equal(t, "cli-automate-json123", entries[0]["session_id"])
	assert.Equal(t, "running", entries[0]["status"])
}

// =============================================================================
// runAutomateStop
// =============================================================================

func TestAutomateStop_AlreadyDead(t *testing.T) {
	defer resetAutomateGlobals()

	sproutDir := setupTestSproutDir(t)
	writeTestSession(t, sproutDir, "cli-automate-stopped", 99999)

	err := runAutomateStop("cli-automate-stopped")
	require.NoError(t, err)

	// PID file should be cleaned up
	_, readErr := os.ReadFile(filepath.Join(sproutDir, "automate", "cli-automate-stopped.json"))
	assert.True(t, os.IsNotExist(readErr), "expected PID file to be removed")
}

func TestAutomateStop_UnknownSession(t *testing.T) {
	defer resetAutomateGlobals()

	setupTestSproutDir(t)

	err := runAutomateStop("nonexistent-session-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read session file")
}

// =============================================================================
// runAutomateLogs
// =============================================================================

func TestAutomateLogs_NoOutputFile(t *testing.T) {
	defer resetAutomateGlobals()
	automateLogsFollow = false
	automateLogsLines = 0

	sproutDir := setupTestSproutDir(t)
	writeTestSession(t, sproutDir, "cli-automate-no-output", os.Getpid())

	buf := new(bytes.Buffer)
	cap := captureAutomateStdout(buf)

	err := runAutomateLogs("cli-automate-no-output")
	cap.Restore()
	require.NoError(t, err)

	got := buf.String()
	assert.Contains(t, got, "No captured output")
	assert.Contains(t, got, "CLI sessions pipe to terminal")
}

func TestAutomateLogs_WithOutput(t *testing.T) {
	defer resetAutomateGlobals()
	automateLogsFollow = false
	automateLogsLines = 0

	sproutDir := setupTestSproutDir(t)

	// Create a temp output file with known content
	tmpFile, err := os.CreateTemp(t.TempDir(), "automate_output_*.log")
	require.NoError(t, err)
	_, err = tmpFile.WriteString("line 1\nline 2\nline 3\n")
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	writeTestSessionWithOutput(t, sproutDir, "cli-automate-with-output", tmpFile.Name(), os.Getpid())

	buf := new(bytes.Buffer)
	cap := captureAutomateStdout(buf)

	err = runAutomateLogs("cli-automate-with-output")
	cap.Restore()
	require.NoError(t, err)

	got := buf.String()
	assert.Contains(t, got, "line 1")
	assert.Contains(t, got, "line 2")
	assert.Contains(t, got, "line 3")
}

func TestAutomateLogs_LastNLines(t *testing.T) {
	defer resetAutomateGlobals()
	automateLogsFollow = false
	automateLogsLines = 2 // only last 2 lines

	sproutDir := setupTestSproutDir(t)

	// Create a temp output file with known content
	tmpFile, err := os.CreateTemp(t.TempDir(), "automate_tail_*.log")
	require.NoError(t, err)
	_, err = tmpFile.WriteString("line 1\nline 2\nline 3")
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	writeTestSessionWithOutput(t, sproutDir, "cli-automate-tail", tmpFile.Name(), os.Getpid())

	buf := new(bytes.Buffer)
	cap := captureAutomateStdout(buf)

	err = runAutomateLogs("cli-automate-tail")
	cap.Restore()
	require.NoError(t, err)

	got := buf.String()
	// With -n 2, should only see lines 2 and 3 (not line 1)
	assert.NotContains(t, got, "line 1")
	assert.Contains(t, got, "line 2")
	assert.Contains(t, got, "line 3")
}

func TestAutomateLogs_MissingSession(t *testing.T) {
	defer resetAutomateGlobals()
	automateLogsFollow = false
	automateLogsLines = 0

	setupTestSproutDir(t)

	err := runAutomateLogs("nonexistent-session-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read session file")
}

func TestAutomateLogs_MissingOutputFile(t *testing.T) {
	defer resetAutomateGlobals()
	automateLogsFollow = false
	automateLogsLines = 0

	sproutDir := setupTestSproutDir(t)

	// Create a session pointing to a file that doesn't exist
	writeTestSessionWithOutput(t, sproutDir, "cli-automate-missing-file", "/tmp/definitely-not-here-12345.log", os.Getpid())

	err := runAutomateLogs("cli-automate-missing-file")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read output file")
}

// =============================================================================
// followLogFile
// =============================================================================

func TestFollowLogFile_ProcessDead(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "follow_*.log")
	require.NoError(t, err)
	// Write initial data, then close. followLogFile will record this size as
	// the offset, so the final read finds nothing new — which is correct:
	// a dead process with no new data should just return.
	_, err = tmpFile.WriteString("existing line\n")
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	// Dead PID — should immediately exit the loop and return without error.
	// Since the file is static (no new data after the offset was recorded),
	// no output is expected.
	err = followLogFile(tmpFile.Name(), 99999)
	require.NoError(t, err)
}

func TestFollowLogFile_NoNewData(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "follow_empty_*.log")
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	// Dead PID + no data — should just exit cleanly with no error.
	// No stdout capture needed: dead PID exits immediately before writing.
	err = followLogFile(tmpFile.Name(), 99999)
	require.NoError(t, err)
}

// =============================================================================
// readAllSessions
// =============================================================================

func TestReadAllSessions_DirNotExists(t *testing.T) {
	tmpDir := t.TempDir() // no .sprout/automate dir created

	sessions, err := readAllSessions(filepath.Join(tmpDir, ".sprout"))
	require.NoError(t, err)
	assert.Nil(t, sessions)
}

func TestReadAllSessions_MultipleSessions(t *testing.T) {
	sproutDir := setupTestSproutDir(t)

	budget1 := 10.0
	budget2 := 25.0
	require.NoError(t, automate.WriteSessionFile(sproutDir, "sess-alpha", &automate.AutomateSessionInfo{
		Workflow:  "workflow-a",
		PID:       os.Getpid(),
		StartedAt: time.Now().Add(-60 * time.Second),
		BudgetUSD: &budget1,
		Kind:      "automate",
	}))
	require.NoError(t, automate.WriteSessionFile(sproutDir, "sess-beta", &automate.AutomateSessionInfo{
		Workflow:  "workflow-b",
		PID:       os.Getpid(),
		StartedAt: time.Now().Add(-30 * time.Second),
		BudgetUSD: &budget2,
		Kind:      "automate",
	}))

	sessions, err := readAllSessions(sproutDir)
	require.NoError(t, err)
	assert.Len(t, sessions, 2)

	// Check both are present
	names := make(map[string]bool)
	for _, s := range sessions {
		names[s.SessionID] = true
	}
	assert.True(t, names["sess-alpha"])
	assert.True(t, names["sess-beta"])
}

func TestReadAllSessions_SkipsNonJson(t *testing.T) {
	sproutDir := setupTestSproutDir(t)

	// Write a valid session
	writeTestSession(t, sproutDir, "valid-session", os.Getpid())

	// Write non-.json files
	automateDirPath := filepath.Join(sproutDir, "automate")
	require.NoError(t, os.WriteFile(filepath.Join(automateDirPath, "readme.txt"), []byte("just a note"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(automateDirPath, "data.yaml"), []byte("foo: bar"), 0o644))

	sessions, err := readAllSessions(sproutDir)
	require.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "valid-session", sessions[0].SessionID)
}

// =============================================================================
// printStatusTable
// =============================================================================

func TestPrintStatusTable_FormatsCorrectly(t *testing.T) {
	sproutDir := setupTestSproutDir(t)
	writeTestSession(t, sproutDir, "sess-table", os.Getpid())

	sessions, err := readAllSessions(sproutDir)
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	cap := captureAutomateStdout(buf)

	printStatusTable(sessions)

	cap.Restore()

	got := buf.String()
	assert.Contains(t, got, "SESSION")
	assert.Contains(t, got, "WORKFLOW")
	assert.Contains(t, got, "STATUS")
	assert.Contains(t, got, "PID")
	assert.Contains(t, got, "sess-table")
	assert.Contains(t, got, "running")
}

// =============================================================================
// printStatusJSON
// =============================================================================

func TestPrintStatusJSON_IsValidJson(t *testing.T) {
	sproutDir := setupTestSproutDir(t)
	writeTestSession(t, sproutDir, "sess-json", os.Getpid())

	sessions, err := readAllSessions(sproutDir)
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	cap := captureAutomateStdout(buf)

	err = printStatusJSON(sessions)
	cap.Restore()
	require.NoError(t, err)

	got := strings.TrimSpace(buf.String())
	var entries []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(got), &entries))
	assert.Len(t, entries, 1)
	assert.Equal(t, "sess-json", entries[0]["session_id"])
	assert.Equal(t, float64(os.Getpid()), entries[0]["pid"])
	assert.Equal(t, "running", entries[0]["status"])
}

// =============================================================================
// readAllSessions edge case: corrupt JSON files are skipped
// =============================================================================

func TestReadAllSessions_SkipsCorrupt(t *testing.T) {
	sproutDir := setupTestSproutDir(t)

	// Write a valid session
	writeTestSession(t, sproutDir, "good-session", os.Getpid())

	// Write an invalid JSON file
	automateDirPath := filepath.Join(sproutDir, "automate")
	require.NoError(t, os.WriteFile(filepath.Join(automateDirPath, "bad-session.json"), []byte("{not valid json}"), 0o644))

	sessions, err := readAllSessions(sproutDir)
	require.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "good-session", sessions[0].SessionID)
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestAutomateIntegration_LaunchStatusStop(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep command not available on this platform")
	}

	// Create a temp directory with .sprout/automate/ subdirectory
	tmpDir := t.TempDir()
	sproutDir := filepath.Join(tmpDir, ".sprout")
	require.NoError(t, os.MkdirAll(filepath.Join(sproutDir, "automate"), 0o700))

	// Create a small output file before starting the subprocess
	outputFile, err := os.CreateTemp(tmpDir, "integ_output_*.log")
	require.NoError(t, err)
	_, err = outputFile.WriteString("initial output\n")
	require.NoError(t, err)
	require.NoError(t, outputFile.Close())

	// Start a real subprocess
	cmd := exec.Command("sleep", "30")
	require.NoError(t, cmd.Start())

	// Ensure the process is cleaned up if we return early for any reason
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	pid := cmd.Process.Pid

	// Write a PID file
	require.NoError(t, automate.WriteSessionFile(sproutDir, "int-test-sleep", &automate.AutomateSessionInfo{
		Workflow:       "sleep-test",
		PID:            pid,
		StartedAt:      time.Now(),
		OutputFilePath: outputFile.Name(),
		Kind:           "automate",
	}))

	// Verify the session file is accessible and contains expected output file path
	info, err := automate.ReadSessionFile(sproutDir, "int-test-sleep")
	require.NoError(t, err)
	assert.Equal(t, outputFile.Name(), info.OutputFilePath)

	// Verify the output file is accessible
	data, err := os.ReadFile(info.OutputFilePath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "initial output")

	// List session files and verify it finds the session
	sessions, err := automate.ListSessionFiles(sproutDir)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "automate", sessions[0].Kind)
	assert.Equal(t, pid, sessions[0].PID)

	// Verify the process is alive
	assert.True(t, automate.IsProcessAlive(pid), "process should be alive before stop")

	// Stop the process
	_, err = automate.StopProcess(pid)
	require.NoError(t, err)

	// Reap the zombie so IsProcessAlive (which uses kill(pid,0)) sees it as gone.
	// StopProcess sends signals but without Wait() the child becomes a zombie that
	// still exists in the process table and responds to kill(pid,0).
	_ = cmd.Wait()

	// Verify the process is dead
	assert.False(t, automate.IsProcessAlive(pid), "process should be dead after stop and reap")

	// List sessions again — the file may still exist since sweep isn't auto-run
	sessions, err = automate.ListSessionFiles(sproutDir)
	require.NoError(t, err)
	assert.Len(t, sessions, 1, "session file should still exist before sweep")

	// Sweep stale sessions and verify the file is removed
	removed, err := automate.SweepStaleSessions(sproutDir)
	require.NoError(t, err)
	assert.Equal(t, 1, removed, "sweep should remove the stale session file")

	// Confirm the session file is gone
	sessions, err = automate.ListSessionFiles(sproutDir)
	require.NoError(t, err)
	assert.Len(t, sessions, 0, "no sessions should remain after sweep")
}

// =============================================================================
// Cross-Process Discovery Tests
// =============================================================================

func TestAutomateCrossProcess_Discovery(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep command not available on this platform")
	}

	// Create a temp directory with .sprout/automate/ subdirectory
	tmpDir := t.TempDir()
	sproutDir := filepath.Join(tmpDir, ".sprout")
	require.NoError(t, os.MkdirAll(filepath.Join(sproutDir, "automate"), 0o700))

	// Start a real background process using exec.Command("sleep", "30")
	cmd := exec.Command("sleep", "30")
	require.NoError(t, cmd.Start())

	// Ensure the process is cleaned up if we return early for any reason
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	pid := cmd.Process.Pid

	// Write a PID file for it
	require.NoError(t, automate.WriteSessionFile(sproutDir, "crossproc-sleep", &automate.AutomateSessionInfo{
		Workflow:  "cross-test",
		PID:       pid,
		StartedAt: time.Now(),
		Kind:      "automate",
	}))

	// From a separate goroutine (simulating a different process/terminal),
	// call automate.ListSessionFiles(sproutDir) to discover the session
	var discovered []automate.AutomateSessionInfo
	var discoverErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		discovered, discoverErr = automate.ListSessionFiles(sproutDir)
	}()
	wg.Wait()

	require.NoError(t, discoverErr)
	require.Len(t, discovered, 1, "should discover exactly one session")

	// Verify the discovered session has Kind == "automate" and the correct PID
	assert.Equal(t, "automate", discovered[0].Kind)
	assert.Equal(t, pid, discovered[0].PID)

	// Verify automate.IsProcessAlive(pid) returns true
	assert.True(t, automate.IsProcessAlive(pid), "process should be alive before stop")

	// Kill the process using automate.StopProcess(pid)
	_, err := automate.StopProcess(pid)
	require.NoError(t, err)

	// Reap the zombie so IsProcessAlive sees it as gone
	_ = cmd.Wait()

	// Verify automate.IsProcessAlive(pid) returns false after stop
	assert.False(t, automate.IsProcessAlive(pid), "process should be dead after stop")

	// Verify cleanup via sweep
	removed, err := automate.SweepStaleSessions(sproutDir)
	require.NoError(t, err)
	assert.Equal(t, 1, removed, "sweep should remove the stale session file")

	// Confirm the session file is gone
	sessions, err := automate.ListSessionFiles(sproutDir)
	require.NoError(t, err)
	assert.Len(t, sessions, 0, "no sessions should remain after sweep")
}
