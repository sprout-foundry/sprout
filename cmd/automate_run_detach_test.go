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

// TestOpenDetachLogFile verifies the detach-mode log wiring: the directory
// is created under <sproutDir>/automate/logs/, the file is writable, and
// the returned path points at it. This is the mechanism that decouples a
// detached workflow child's stdio from the launcher's lifetime — the
// SIGPIPE-death fix for `sprout automate run --detach`.
func TestOpenDetachLogFile(t *testing.T) {
	sproutDir := filepath.Join(t.TempDir(), ".sprout")
	sessionID := "cli-automate-deadbeefdeadbeef"

	f, path, err := openDetachLogFile(sproutDir, sessionID)
	if err != nil {
		t.Fatalf("openDetachLogFile: %v", err)
	}
	defer f.Close()

	wantDir := filepath.Join(sproutDir, "automate", "logs")
	if fi, err := os.Stat(wantDir); err != nil || !fi.IsDir() {
		t.Fatalf("log dir not created at %s: %v", wantDir, err)
	}
	wantPath := filepath.Join(wantDir, sessionID+".log")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	if _, err := f.WriteString("probe\n"); err != nil {
		t.Fatalf("log file not writable: %v", err)
	}

	// Second call with the same session truncates rather than appending —
	// matches os.O_TRUNC semantics and keeps restarts from stacking logs.
	f2, _, err := openDetachLogFile(sproutDir, sessionID)
	if err != nil {
		t.Fatalf("second openDetachLogFile: %v", err)
	}
	defer f2.Close()
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("expected truncate-on-reopen, got %d bytes", len(data))
	}
}

// TestAutomateDetachFlagDefaults pins the flag default so the attached
// streaming behavior stays the default and --detach is opt-in.
func TestAutomateDetachFlagDefaults(t *testing.T) {
	if automateDetach {
		t.Fatal("automateDetach should default to false")
	}
	flag := automateCmd.PersistentFlags().Lookup("detach")
	if flag == nil {
		t.Fatal("--detach flag not registered on automate command group")
	}
	if flag.DefValue != "false" {
		t.Fatalf("--detach default = %q, want \"false\"", flag.DefValue)
	}
}

// TestAppendDetachedSessionFileArg pins the launcher-side half of child-side
// self-finalization: --detach adds --automate-session-file with the exact
// record path, attached mode adds nothing (its launcher-side deferred
// FinalizeSessionFile already owns the record — two writers would race).
func TestAppendDetachedSessionFileArg(t *testing.T) {
	defer resetAutomateGlobals()()
	base := []string{"agent", "--workflow-config", "automate/wf.json", "--skip-prompt", "--no-web-ui"}

	automateDetach = false
	assert.Equal(t, base, appendDetachedSessionFileArg(base, "/root/.sprout", "cli-automate-aa"),
		"attached runs must not receive --automate-session-file")
	assert.Equal(t, base, []string{"agent", "--workflow-config", "automate/wf.json", "--skip-prompt", "--no-web-ui"},
		"attached runs must leave the base args untouched")

	automateDetach = true
	withFlag := appendDetachedSessionFileArg(base, "/root/.sprout", "cli-automate-aa")
	assert.Equal(t, append(append([]string{}, base...), "--automate-session-file", "/root/.sprout/automate/cli-automate-aa.json"), withFlag)
	assert.Equal(t, "/root/.sprout/automate/cli-automate-aa.json", detachedSessionFilePath("/root/.sprout", "cli-automate-aa"),
		"flag path must match the launcher's own PID-file location")
}

// captureAgentArgs installs a buildAgentCommandFn stand-in whose child writes
// one argument per line to capturePath, so a test can assert on the argv the
// launcher really handed to the agent subprocess. The temp-file + rename
// makes the capture atomic: once capturePath exists its content is complete,
// which matters in detach mode where the launcher returns before the child
// has run a single instruction.
func captureAgentArgs(t *testing.T, capturePath string) {
	t.Helper()
	savedFn := buildAgentCommandFn
	buildAgentCommandFn = func(_ string, args []string) *exec.Cmd {
		script := `printf '%s\n' "$@" > "$ARGV_CAPTURE.tmp" && mv "$ARGV_CAPTURE.tmp" "$ARGV_CAPTURE"`
		c := exec.Command("/bin/sh", append([]string{"-c", script, "sprout-standin"}, args...)...)
		c.Env = append(os.Environ(), "ARGV_CAPTURE="+capturePath)
		return c
	}
	t.Cleanup(func() { buildAgentCommandFn = savedFn })
}

// readCapturedArgs reads back the argv written by the capture stand-in,
// polling briefly first: a detached launcher returns before its child has
// necessarily executed a single instruction.
func readCapturedArgs(t *testing.T, capturePath string) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(capturePath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stand-in child never wrote its argv to %s", capturePath)
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, err := os.ReadFile(capturePath)
	require.NoError(t, err)
	var args []string
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line != "" {
			args = append(args, line)
		}
	}
	return args
}

// TestAutomateRun_Detach_PassesSessionFileFlag runs the real detach launch
// machinery against an argv-capturing stand-in child and asserts the child is
// told which record to finalize: --automate-session-file followed by the
// launcher's own <sproutDir>/automate/<sessionID>.json path.
func TestAutomateRun_Detach_PassesSessionFileFlag(t *testing.T) {
	if !fileExists("/bin/sh") {
		t.Skip("/bin/sh not available")
	}

	defer resetAutomateGlobals()()
	automateAssumeYes = true
	automateDetach = true
	t.Setenv("SPROUT_AUTOMATE_MIN_MEM_MB", "0")

	capturePath := filepath.Join(t.TempDir(), "argv")
	captureAgentArgs(t, capturePath)

	sproutDir := setupTestSproutDir(t)
	wfPath := filepath.Join(t.TempDir(), "wf.json")
	require.NoError(t, os.WriteFile(wfPath, []byte(`{"description":"argv capture"}`), 0o600))

	require.NoError(t, runWorkflowByPath(wfPath))

	sessionID := findOnlySessionID(t, sproutDir)
	wantPath := filepath.Join(sproutDir, "automate", sessionID+".json")
	if resolved, rerr := filepath.EvalSymlinks(sproutDir); rerr == nil {
		wantPath = filepath.Join(resolved, "automate", sessionID+".json")
	}

	args := readCapturedArgs(t, capturePath)
	idx := -1
	for i, a := range args {
		if a == "--automate-session-file" {
			idx = i
			break
		}
	}
	require.GreaterOrEqual(t, idx, 0, "detached child argv must contain --automate-session-file, got %v", args)
	require.Len(t, args, idx+2, "flag must be followed by its value, got %v", args)
	assert.Equal(t, wantPath, args[idx+1],
		"--automate-session-file must point at the record the launcher itself writes")

	// The stand-in child ignores the flag, so the launcher-side no-finalize
	// contract still holds: only a real agent child writes its end state.
	info, err := automate.ReadSessionFile(sproutDir, sessionID)
	require.NoError(t, err)
	require.Nil(t, info.EndedAt, "launcher must not finalize in detach mode")
	require.Nil(t, info.ExitCode, "launcher must not record an exit code in detach mode")
}

// TestAutomateRun_Attach_DoesNotPassSessionFileFlag is the attach-mode guard
// for the same seam: attached children get no finalization flag, and the
// launcher's own deferred FinalizeSessionFile (post-Wait) still owns the
// record — exactly the pre-AUTOM-4 attached behavior.
func TestAutomateRun_Attach_DoesNotPassSessionFileFlag(t *testing.T) {
	if !fileExists("/bin/sh") {
		t.Skip("/bin/sh not available")
	}

	defer resetAutomateGlobals()()
	automateAssumeYes = true
	automateDetach = false
	t.Setenv("SPROUT_AUTOMATE_MIN_MEM_MB", "0")

	capturePath := filepath.Join(t.TempDir(), "argv")
	captureAgentArgs(t, capturePath)

	sproutDir := setupTestSproutDir(t)
	wfPath := filepath.Join(t.TempDir(), "wf.json")
	require.NoError(t, os.WriteFile(wfPath, []byte(`{"description":"attach argv capture"}`), 0o600))

	require.NoError(t, runWorkflowByPath(wfPath), "attached run of an exiting stand-in child succeeds")

	args := readCapturedArgs(t, capturePath)
	assert.NotContains(t, args, "--automate-session-file",
		"attached child argv must not contain --automate-session-file, got %v", args)

	sessionID := findOnlySessionID(t, sproutDir)
	info, err := automate.ReadSessionFile(sproutDir, sessionID)
	require.NoError(t, err)
	require.NotNil(t, info.EndedAt, "attached launcher must still finalize via its deferred FinalizeSessionFile")
	require.NotNil(t, info.ExitCode)
	assert.Equal(t, 0, *info.ExitCode)
	assert.Equal(t, "success", info.Status)
	assert.Zero(t, info.PID)
}

// TestAutomateRun_Detach_ReapsFastExit pins the AUTOM-3 reaper: a detached
// child that exits immediately must not linger as a zombie in a
// long-lived launcher process. kill(pid,0) succeeds against zombies, so
// without the background cmd.Wait() in the detach branch, IsProcessAlive
// (the exact probe `sprout automate status` uses) would keep reporting
// "running" for a dead workflow for as long as this test process lives.
// The e2e detach test can't catch this — its child sleeps 5s, so it never
// observes the post-exit window this test targets.
//
// Platform notes: the /proc/<pid>/stat zombie-state check is Linux-only
// (returns "" elsewhere); the IsProcessAlive fallback covers other Unixes
// where a reaped PID must be gone (ESRCH). Windows has no zombie state
// and its liveness probe (GetExitCodeProcess) already reports exited
// children, so this test skips there via the /bin/sh guard.
func TestAutomateRun_Detach_ReapsFastExit(t *testing.T) {
	if !fileExists("/bin/sh") {
		t.Skip("/bin/sh not available")
	}

	defer resetAutomateGlobals()()
	automateAssumeYes = true
	automateDetach = true
	// Memory floor is a guard for real agents, not a /bin/sh stand-in.
	t.Setenv("SPROUT_AUTOMATE_MIN_MEM_MB", "0")

	// Stand-in child that exits immediately. The production machinery
	// under test (start, async reaper, PID-file write) is real; only the
	// binary+args are the stand-in, same seam as the e2e detach test.
	savedFn := buildAgentCommandFn
	buildAgentCommandFn = func(string, []string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", "exit 0")
	}
	defer func() { buildAgentCommandFn = savedFn }()

	sproutDir := setupTestSproutDir(t)

	wfPath := filepath.Join(t.TempDir(), "wf.json")
	require.NoError(t, os.WriteFile(wfPath, []byte(`{"description":"reap"}`), 0o600))

	require.NoError(t, runWorkflowByPath(wfPath))

	// Recover the child PID from the session record — the only durable
	// handle on the child the launcher retains.
	sessionID := findOnlySessionID(t, sproutDir)
	info, err := automate.ReadSessionFile(sproutDir, sessionID)
	require.NoError(t, err)
	require.NotZero(t, info.PID)
	pid := info.PID

	// The child must be fully reaped: not a zombie, and gone from the
	// PID table. The reaper goroutine races with the return from
	// runWorkflowByPath, so poll briefly for the settled state — with
	// the reaper, "settled" means dead; without it, the child stays a
	// zombie for this process's lifetime and this loop times out.
	var reaped bool
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !automate.IsProcessAlive(pid) {
			reaped = true
			break
		}
		if state := procState(t, pid); strings.Contains(state, "Z") {
			// Zombie means the child exited but our reaper has not run
			// yet — keep polling; it must not stay this way.
			time.Sleep(10 * time.Millisecond)
			continue
		}
		// Alive and not a zombie: the child is still running (/bin/sh
		// "exit 0" finishes in ~1ms, but stay robust to slow CI).
		time.Sleep(10 * time.Millisecond)
	}
	require.True(t, reaped,
		"detached child PID %d must be reaped (no zombie): IsProcessAlive stayed true for 5s — "+
			"the detach branch's background cmd.Wait() is missing or never ran", pid)

	// End-state semantics are the child's job, not the reaper's: this
	// stand-in ignores --automate-session-file, so the record must still
	// be unfinalized — which also pins that the launcher never finalizes
	// in detach mode (only the real agent child self-finalizes).
	fresh, err := automate.ReadSessionFile(sproutDir, sessionID)
	require.NoError(t, err)
	require.Nil(t, fresh.EndedAt, "reaper must not finalize the session record (child-side finalization)")
	require.Nil(t, fresh.ExitCode, "reaper must not record an exit code (child-side finalization)")
	require.Equal(t, "running", fresh.Status, "unfinalized record keeps its launch-time status")

	// Status output must not regress: the dead unfinalized session renders
	// as "exited" (PID-liveness fallback), not "running". Slice the table
	// at this session's ID to check its row (the status column follows the
	// session-ID column).
	var buf bytes.Buffer
	cap := captureAutomateStdout(&buf)
	statusErr := runAutomateStatus()
	cap.Restore()
	require.NoError(t, statusErr)
	_, row, found := strings.Cut(buf.String(), sessionID)
	require.True(t, found, "status table must list the session %s", sessionID)
	assert.Contains(t, row, "exited", "status row for reaped session must show exited, got %q", row)
	assert.NotContains(t, row, "running", "status row for reaped session must not show running")
}

// findOnlySessionID returns the single cli-automate-* session in sproutDir.
func findOnlySessionID(t *testing.T, sproutDir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(sproutDir, "automate"))
	require.NoError(t, err)
	var ids []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "cli-automate-") && strings.HasSuffix(e.Name(), ".json") {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	require.Len(t, ids, 1, "expected exactly one cli-automate-* session record")
	return ids[0]
}

// procState reads the process state field (column 3) of /proc/<pid>/stat,
// returning "" when the PID is already gone (ESRCH) or /proc is unavailable
// (macOS). A "Z" prefix means zombie: exited but not yet reaped.
func procState(t *testing.T, pid int) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("/proc", strconvItoa(pid), "stat"))
	if err != nil {
		return ""
	}
	// Fields after the comm field (which may contain spaces, in parens):
	// find the last ')' then read the next whitespace-delimited token.
	s := string(data)
	if i := strings.LastIndex(s, ")"); i >= 0 && i+2 <= len(s) {
		return strings.TrimSpace(s[i+1:])
	}
	return ""
}
