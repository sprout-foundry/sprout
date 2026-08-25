//go:build !js

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AUTOM-6 regression tests: the status/stop/logs flag globals
// (automateStatusAll/--json, automateStopAll, automateLogsFollow/-Lines)
// must be bound to real cobra flags so `sprout automate status --all`,
// `status --json`, `stop --all`, and `logs -f`/`-n` parse and reach the
// run functions. Before the fix each invocation died with
// "unknown flag" before RunE ever ran.
//
// These execute the real registered command tree via rootCmd (cobra
// delegates subcommand Execute to the root anyway), which exercises the
// actual init() flag registrations — not a re-declared test copy. Each
// test asserts both that parsing succeeds and that the parsed values land
// in the globals the run functions read.

// executeAutomateCmd sets args on the shared root command and executes it.
// Callers must capture stdout (if needed) around the Execute call itself.
func executeAutomateCmd(t *testing.T, args ...string) error {
	t.Helper()
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	// The AUTOM-6 bug manifested as this exact error; surface it loudly if
	// it ever regresses rather than letting a generic assertion miss it.
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("flag no longer registered: %v", err)
	}
	return err
}

// =============================================================================
// automate status --all / --json
// =============================================================================

func TestAutomateStatusCmd_FlagsParse(t *testing.T) {
	defer resetAutomateGlobals()()
	automateStatusAll = false
	automateStatusJSON = false

	sproutDir := setupTestSproutDir(t)
	writeTestSession(t, sproutDir, "cli-automate-live", os.Getpid())
	writeTestSession(t, sproutDir, "cli-automate-dead", 99999)

	buf := new(bytes.Buffer)
	cap := captureAutomateStdout(buf)
	err := executeAutomateCmd(t, "automate", "status", "--all")
	cap.Restore()
	require.NoError(t, err)

	// Parsed values reach the run function's globals.
	assert.True(t, automateStatusAll, "--all must bind to automateStatusAll")
	assert.False(t, automateStatusJSON)

	// --all shows both the live and the dead session.
	got := buf.String()
	assert.Contains(t, got, "cli-automate-live")
	assert.Contains(t, got, "running")
	assert.Contains(t, got, "cli-automate-dead")
	assert.Contains(t, got, "exited")
}

func TestAutomateStatusCmd_JsonFlagParses(t *testing.T) {
	defer resetAutomateGlobals()()
	automateStatusAll = false
	automateStatusJSON = false

	sproutDir := setupTestSproutDir(t)
	writeTestSession(t, sproutDir, "cli-automate-jsonflag", os.Getpid())

	buf := new(bytes.Buffer)
	cap := captureAutomateStdout(buf)
	err := executeAutomateCmd(t, "automate", "status", "--json")
	cap.Restore()
	require.NoError(t, err)

	assert.True(t, automateStatusJSON, "--json must bind to automateStatusJSON")
	assert.False(t, automateStatusAll)

	var entries []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &entries))
	require.Len(t, entries, 1)
	assert.Equal(t, "cli-automate-jsonflag", entries[0]["session_id"])
}

// =============================================================================
// automate stop --all
// =============================================================================

func TestAutomateStopCmd_AllFlagParses(t *testing.T) {
	defer resetAutomateGlobals()()
	automateStopAll = false

	sproutDir := setupTestSproutDir(t)
	writeTestSession(t, sproutDir, "cli-automate-stopall-dead", 99999)

	err := executeAutomateCmd(t, "automate", "stop", "--all")
	require.NoError(t, err)

	assert.True(t, automateStopAll, "--all must bind to automateStopAll")

	// The flag routed execution to runAutomateStopAll: the dead-unfinalized
	// record is finalized for post-mortem (exit -1), not deleted.
	info, readErr := automate.ReadSessionFile(sproutDir, "cli-automate-stopall-dead")
	require.NoError(t, readErr)
	require.NotNil(t, info.EndedAt, "runAutomateStopAll must have finalized the record")
	require.NotNil(t, info.ExitCode)
	assert.Equal(t, -1, *info.ExitCode)
	assert.Equal(t, "error", info.Status)
}

// =============================================================================
// automate logs -f / --follow and -n / --lines
// =============================================================================

func TestAutomateLogsCmd_FollowAndLinesFlagsParse(t *testing.T) {
	defer resetAutomateGlobals()()
	automateLogsFollow = false
	automateLogsLines = 0

	sproutDir := setupTestSproutDir(t)

	logFile, err := os.CreateTemp(t.TempDir(), "automate_flags_*.log")
	require.NoError(t, err)
	_, err = logFile.WriteString("line 1\nline 2\nline 3")
	require.NoError(t, err)
	require.NoError(t, logFile.Close())

	// Dead PID: the follow loop sees the process gone and exits after one
	// final read, so exercising -f cannot hang the test.
	writeTestSessionWithOutput(t, sproutDir, "cli-automate-flags", logFile.Name(), 99999)

	buf := new(bytes.Buffer)
	cap := captureAutomateStdout(buf)
	err = executeAutomateCmd(t, "automate", "logs", "-n", "2", "-f", "cli-automate-flags")
	cap.Restore()
	require.NoError(t, err)

	// Parsed values reach the run function's globals.
	assert.True(t, automateLogsFollow, "-f must bind to automateLogsFollow")
	assert.Equal(t, 2, automateLogsLines, "-n must bind to automateLogsLines")

	// -n 2 kept only the last two lines.
	got := buf.String()
	assert.NotContains(t, got, "line 1")
	assert.Contains(t, got, "line 2")
	assert.Contains(t, got, "line 3")
}

func TestAutomateLogsCmd_LongFlagsParse(t *testing.T) {
	defer resetAutomateGlobals()()
	automateLogsFollow = false
	automateLogsLines = 0

	sproutDir := setupTestSproutDir(t)

	logFile, err := os.CreateTemp(t.TempDir(), "automate_longflags_*.log")
	require.NoError(t, err)
	_, err = logFile.WriteString("alpha\nbeta\ngamma\ndelta")
	require.NoError(t, err)
	require.NoError(t, logFile.Close())

	writeTestSessionWithOutput(t, sproutDir, "cli-automate-longflags", logFile.Name(), 99999)

	buf := new(bytes.Buffer)
	cap := captureAutomateStdout(buf)
	err = executeAutomateCmd(t, "automate", "logs", "--lines", "2", "--follow", "cli-automate-longflags")
	cap.Restore()
	require.NoError(t, err)

	assert.True(t, automateLogsFollow, "--follow must bind to automateLogsFollow")
	assert.Equal(t, 2, automateLogsLines, "--lines must bind to automateLogsLines")

	got := buf.String()
	assert.NotContains(t, got, "alpha")
	assert.NotContains(t, got, "beta")
	assert.Contains(t, got, "gamma")
	assert.Contains(t, got, "delta")
}
