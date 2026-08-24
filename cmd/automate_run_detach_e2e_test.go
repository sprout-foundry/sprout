//go:build !js

package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAutomateRun_Detach_EndToEnd pins the --detach contract with a real
// child process: immediate return, PID file with OutputFilePath and no
// finalization fields, child stdio pointed at the session log file, and
// output landing in that file. The child is a /bin/sh stand-in swapped in
// via buildAgentSubprocessArgsFn — the launch machinery under test
// (exec.Command construction, file-backed stdio, start, PID-file write)
// is the real production code path.
func TestAutomateRun_Detach_EndToEnd(t *testing.T) {
	if !fileExists("/bin/sh") {
		t.Skip("/bin/sh not available")
	}

	defer resetAutomateGlobals()()
	automateAssumeYes = true
	automateDetach = true

	// The stand-in child is /bin/sh — the production memory floor (1.5 GB
	// available) is a guard against OOM-killing real workflow agents and
	// must not flake this test on memory-starved CI runners (macOS shared
	// runners report <1 GB "Pages free" under load). Same backstop as
	// pkg/automate/pid_file_lifecycle_test.go.
	t.Setenv("SPROUT_AUTOMATE_MIN_MEM_MB", "0")

	// Stand-in child: /bin/sh emitting a line every 100ms for ~5s — long
	// enough to inspect /proc fd targets and observe log content while
	// alive. The production machinery under test (exec.Command
	// construction, file-backed stdio, start, PID-file write) runs for real
	// via buildAgentCommandFn; only the binary+args are the stand-in.
	savedFn := buildAgentCommandFn
	buildAgentCommandFn = func(string, []string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", "for i in $(seq 50); do echo tick $i; sleep 0.1; done")
	}
	defer func() { buildAgentCommandFn = savedFn }()

	sproutDir := setupTestSproutDir(t)

	// Minimal workflow JSON so Summarize succeeds.
	wfPath := filepath.Join(t.TempDir(), "wf.json")
	require.NoError(t, os.WriteFile(wfPath, []byte(`{"description":"detach e2e"}`), 0o600))

	start := time.Now()
	require.NoError(t, runWorkflowByPath(wfPath))
	assert.Less(t, time.Since(start), 3*time.Second,
		"detach must return immediately, not wait for the child")

	// Exactly one session file should exist; read it back.
	entries, err := os.ReadDir(filepath.Join(sproutDir, "automate"))
	require.NoError(t, err)
	var sessionFile string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			sessionFile = filepath.Join(sproutDir, "automate", e.Name())
		}
	}
	require.NotEmpty(t, sessionFile, "PID file must be written")

	raw, err := os.ReadFile(sessionFile)
	require.NoError(t, err)
	var info automate.AutomateSessionInfo
	require.NoError(t, json.Unmarshal(raw, &info))
	require.NotZero(t, info.PID, "session must record the child PID")
	// macOS: t.TempDir() yields the unresolved /var/folders/... form, but
	// the production path builds the log path from os.Getwd(), which after
	// the chdir in setupTestSproutDir falls back to the syscall (stale
	// $PWD) and returns the canonical /private/var/folders/... form. Same
	// directory, different strings — resolve the expected prefix first.
	// On Linux both forms coincide, so this is a no-op.
	expectedPrefix := filepath.Join(sproutDir, "automate", "logs")
	if resolved, rerr := filepath.EvalSymlinks(sproutDir); rerr == nil {
		expectedPrefix = filepath.Join(resolved, "automate", "logs")
	}
	require.True(t, strings.HasPrefix(info.OutputFilePath, expectedPrefix),
		"OutputFilePath must point under .sprout/automate/logs/, got %q", info.OutputFilePath)
	require.Nil(t, info.EndedAt, "detached sessions are never finalized by the launcher")
	require.Nil(t, info.ExitCode, "detached sessions record no exit code at launch")

	// The durable property: the child's stdout IS the log file (not a
	// pipe owned by this test process). Linux-only inspection.
	fd1 := filepath.Join("/proc", strconvItoa(info.PID), "fd", "1")
	if link, lerr := os.Readlink(fd1); lerr == nil {
		assert.True(t, strings.HasSuffix(link, ".log"),
			"child stdout must be the session log file, got %q", link)
	} else {
		t.Logf("could not inspect child fd (exited early?): %v", lerr)
	}

	// Output must land in the log file while the child is alive.
	time.Sleep(400 * time.Millisecond)
	data, rerr := os.ReadFile(info.OutputFilePath)
	require.NoError(t, rerr)
	assert.Contains(t, string(data), "tick", "child output must land in the detach log file")

	// Cleanup: kill the stand-in child.
	if proc, perr := os.FindProcess(info.PID); perr == nil {
		_ = proc.Kill()
	}
}

func strconvItoa(i int) string { return strconv.Itoa(i) }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
