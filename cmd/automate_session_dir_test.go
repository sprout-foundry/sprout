//go:build !js

package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isolateStateDir(t *testing.T) string {
	t.Helper()
	reg := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", reg)
	return reg
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(path, 0o700))
}

func TestDiscoverSproutSessionRoot_FindsNearestAncestor(t *testing.T) {
	isolateStateDir(t)
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, ".sprout", "automate"))
	mustMkdirAll(t, filepath.Join(root, "a", "b"))

	got := discoverSproutSessionRoot(filepath.Join(root, "a", "b"))
	assert.Equal(t, filepath.Join(root, ".sprout"), got)
}

func TestDiscoverSproutSessionRoot_NearestWins(t *testing.T) {
	isolateStateDir(t)
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, ".sprout", "automate"))
	mustMkdirAll(t, filepath.Join(root, "a", ".sprout", "automate"))
	mustMkdirAll(t, filepath.Join(root, "a", "b"))

	got := discoverSproutSessionRoot(filepath.Join(root, "a", "b"))
	assert.Equal(t, filepath.Join(root, "a", ".sprout"), got)
}

func TestDiscoverSproutSessionRoot_RegistryFallback(t *testing.T) {
	reg := isolateStateDir(t)
	mustMkdirAll(t, filepath.Join(reg, "automate"))

	// Fresh subtree with no .sprout anywhere up to the fixture root.
	startDir := filepath.Join(t.TempDir(), "sub", "deep")
	mustMkdirAll(t, startDir)

	got := discoverSproutSessionRoot(startDir)
	assert.Equal(t, reg, got)
}

func TestDiscoverSproutSessionRoot_RegistryIgnoredWhenAutomateAbsent(t *testing.T) {
	reg := isolateStateDir(t) // reg exists, but reg/automate does not

	startDir := filepath.Join(t.TempDir(), "sub")
	mustMkdirAll(t, startDir)

	got := discoverSproutSessionRoot(startDir)
	assert.Equal(t, filepath.Join(startDir, ".sprout"), got)
	assert.NoDirExists(t, filepath.Join(reg, "automate"),
		"lookup must not materialize the central registry")
}

func TestDiscoverSproutSessionRoot_NoRegistryFallsBackToStartDir(t *testing.T) {
	isolateStateDir(t) // empty temp dir, no automate/ subdir
	startDir := filepath.Join(t.TempDir(), "sub")
	mustMkdirAll(t, startDir)

	got := discoverSproutSessionRoot(startDir)
	assert.Equal(t, filepath.Join(startDir, ".sprout"), got)
}

func TestAutomateSessionRoot_ExplicitDirIsAbsolutized(t *testing.T) {
	defer resetAutomateGlobals()()
	isolateStateDir(t)

	wd := t.TempDir()
	mustMkdirAll(t, wd)
	t.Chdir(wd)

	automateSessionDir = "explicit-root"
	got, err := automateSessionRoot()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(wd, "explicit-root"), got)
}

func TestAutomateSessionRoot_DiscoveryFromSubdirectory(t *testing.T) {
	defer resetAutomateGlobals()()
	isolateStateDir(t)

	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, ".sprout", "automate"))
	mustMkdirAll(t, filepath.Join(root, "sub", "deep"))
	t.Chdir(filepath.Join(root, "sub", "deep"))

	got, err := automateSessionRoot()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, ".sprout"), got)
}

// =============================================================================
// Acceptance: status/logs/stop resolving sessions from a subdirectory
// =============================================================================

func writeAcceptanceSession(t *testing.T, sproutDir, sessionID string, pid int) {
	t.Helper()
	info := &automate.AutomateSessionInfo{
		Workflow:  "acceptance-wf",
		PID:       pid,
		StartedAt: time.Now().Add(-30 * time.Second),
		Kind:      "automate",
	}
	require.NoError(t, automate.WriteSessionFile(sproutDir, sessionID, info))
}

func TestAutomateSubdirDiscovery_StatusLogsStop(t *testing.T) {
	defer resetAutomateGlobals()()
	automateStatusAll = false
	automateStatusJSON = false
	automateLogsFollow = false
	automateLogsLines = 0

	isolateStateDir(t)
	root := t.TempDir()
	sproutDir := filepath.Join(root, ".sprout")
	mustMkdirAll(t, filepath.Join(sproutDir, "automate"))
	mustMkdirAll(t, filepath.Join(root, "sub", "deep"))
	t.Chdir(filepath.Join(root, "sub", "deep"))

	logPath := filepath.Join(t.TempDir(), "session.log")
	require.NoError(t, os.WriteFile(logPath, []byte("subdir discovery line\n"), 0o600))
	require.NoError(t, automate.WriteSessionFile(sproutDir, "cli-automate-subdir-live", &automate.AutomateSessionInfo{
		Workflow:       "acceptance-wf",
		PID:            os.Getpid(),
		StartedAt:      time.Now().Add(-30 * time.Second),
		OutputFilePath: logPath,
		Kind:           "automate",
	}))
	writeAcceptanceSession(t, sproutDir, "cli-automate-subdir-dead", 99999)

	var buf bytes.Buffer
	cap := captureAutomateStdout(&buf)
	err := runAutomateStatus()
	cap.Restore()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "cli-automate-subdir-live")
	assert.Contains(t, buf.String(), "running")
	assert.Contains(t, buf.String(), "acceptance-wf")

	buf.Reset()
	cap = captureAutomateStdout(&buf)
	err = runAutomateLogs("cli-automate-subdir-live")
	cap.Restore()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "subdir discovery line")

	require.NoError(t, runAutomateStop("cli-automate-subdir-dead"))

	info, readErr := automate.ReadSessionFile(sproutDir, "cli-automate-subdir-dead")
	require.NoError(t, readErr)
	assert.Equal(t, "error", info.Status)
	require.NotNil(t, info.ExitCode)
	assert.Equal(t, -1, *info.ExitCode)
}

func TestAutomateSubdirDiscovery_DirOverride(t *testing.T) {
	defer resetAutomateGlobals()()
	automateStatusAll = false
	automateStatusJSON = false

	isolateStateDir(t)

	// Root A: cwd's nearest ancestor — must be ignored when --dir points at B.
	rootA := t.TempDir()
	mustMkdirAll(t, filepath.Join(rootA, ".sprout", "automate"))
	mustMkdirAll(t, filepath.Join(rootA, "sub"))
	t.Chdir(filepath.Join(rootA, "sub"))
	writeAcceptanceSession(t, filepath.Join(rootA, ".sprout"), "session-at-root-a", os.Getpid())

	// Root B: the --dir target.
	rootB := t.TempDir()
	sproutDirB := filepath.Join(rootB, ".sprout")
	mustMkdirAll(t, filepath.Join(sproutDirB, "automate"))
	writeAcceptanceSession(t, sproutDirB, "session-at-root-b", os.Getpid())

	automateSessionDir = sproutDirB

	var buf bytes.Buffer
	cap := captureAutomateStdout(&buf)
	err := runAutomateStatus()
	cap.Restore()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "session-at-root-b")
	assert.NotContains(t, buf.String(), "session-at-root-a")
}

// =============================================================================
// runWorkflowByPath: session record must land in the discovered root, not cwd
// =============================================================================

func TestAutomateRunFromSubdir_WritesSessionToDiscoveredRoot(t *testing.T) {
	if !fileExists("/bin/sh") {
		t.Skip("/bin/sh not available")
	}

	defer resetAutomateGlobals()()
	automateAssumeYes = true
	automateDetach = true
	// Memory floor is a guard for real agents, not a /bin/sh stand-in.
	t.Setenv("SPROUT_AUTOMATE_MIN_MEM_MB", "0")
	isolateStateDir(t)

	savedFn := buildAgentCommandFn
	buildAgentCommandFn = func(string, []string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", "echo done")
	}
	defer func() { buildAgentCommandFn = savedFn }()

	root := t.TempDir()
	sproutDir := filepath.Join(root, ".sprout")
	mustMkdirAll(t, filepath.Join(sproutDir, "automate"))
	mustMkdirAll(t, filepath.Join(root, "sub"))

	wfPath := filepath.Join(t.TempDir(), "wf.json")
	require.NoError(t, os.WriteFile(wfPath, []byte(`{"description":"d"}`), 0o600))

	t.Chdir(filepath.Join(root, "sub"))
	require.NoError(t, runWorkflowByPath(wfPath))

	entries, err := os.ReadDir(filepath.Join(sproutDir, "automate"))
	require.NoError(t, err)
	var sessionFiles []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "cli-automate-") && strings.HasSuffix(e.Name(), ".json") {
			sessionFiles = append(sessionFiles, e.Name())
		}
	}
	require.Len(t, sessionFiles, 1, "session record must land in the discovered root")

	info, readErr := automate.ReadSessionFile(sproutDir, strings.TrimSuffix(sessionFiles[0], ".json"))
	require.NoError(t, readErr)

	// macOS t.TempDir() returns /var/... while os.Getwd() yields the
	// canonical /private/var/... form — resolve both sides before comparing.
	gotLog, lerr := filepath.EvalSymlinks(info.OutputFilePath)
	require.NoError(t, lerr)
	wantPrefix, perr := filepath.EvalSymlinks(filepath.Join(sproutDir, "automate", "logs"))
	require.NoError(t, perr)
	assert.True(t, strings.HasPrefix(gotLog, wantPrefix),
		"OutputFilePath must be under the discovered root's logs/, got %q", info.OutputFilePath)

	// Nothing may be written where the user stands.
	assert.NoDirExists(t, filepath.Join(root, "sub", ".sprout"))
}
