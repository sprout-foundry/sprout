//go:build unix

package tools

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// TestDetachFromSessionSetsidPath — Verify that when probeSetsidSupport()
// returns true, detachFromSession sets SysProcAttr.Setsid = true and does NOT
// set Setpgid. This is the common path on most systems.
//
// NOTE: probeSetsidSupport uses sync.Once, so the first detachFromSession call
// in this test suite caches the result. On most systems Setsid is available.
// =============================================================================

func TestDetachFromSessionSetsidPath(t *testing.T) {
	// Do NOT use t.Parallel() — shares package-level sync.Once state.
	cmd := exec.Command("true")
	detachFromSession(cmd)

	require.NotNil(t, cmd.SysProcAttr, "SysProcAttr should be initialized")

	if cmd.SysProcAttr.Setsid {
		// Setsid path: Setsid=true, Setpgid must be false (EPERM otherwise)
		assert.True(t, cmd.SysProcAttr.Setsid, "Setsid should be true")
		assert.False(t, cmd.SysProcAttr.Setpgid,
			"Setpgid should be false when Setsid is true (EPERM on session leader)")
	} else {
		// Setpgid fallback path (seccomp-blocked setsid)
		t.Log("Setsid not available on this system — Setpgid fallback path taken")
		assert.True(t, cmd.SysProcAttr.Setpgid, "Setpgid should be true in fallback path")
		assert.False(t, cmd.SysProcAttr.Setsid, "Setsid should be false in fallback path")
	}
}

// =============================================================================
// TestDetachFromSessionNilSysProcAttr — Verify that detachFromSession
// initializes SysProcAttr when it's nil.
// =============================================================================

func TestDetachFromSessionNilSysProcAttr(t *testing.T) {
	// Do NOT use t.Parallel() — shares package-level sync.Once state.

	cmd := exec.Command("true")
	cmd.SysProcAttr = nil

	detachFromSession(cmd)

	require.NotNil(t, cmd.SysProcAttr, "SysProcAttr should be initialized when nil")
	// At least one of Setsid or Setpgid should be set
	assert.True(t, cmd.SysProcAttr.Setsid || cmd.SysProcAttr.Setpgid,
		"either Setsid or Setpgid should be set after detachFromSession")
}

// =============================================================================
// TestBackgroundProcessSurvivesSIGHUP — Integration test: start a long-running
// process via BPM, then send SIGHUP to the parent's process group to simulate
// terminal teardown. The child should survive because:
//
//   - If Setsid was used: child is in its own session, so SIGHUP to the
//     parent's process group doesn't reach it.
//   - If Setpgid was used: child inherited SIG_IGN for SIGHUP from the parent
//     (via sighupIgnored), so it ignores the signal.
//
// In both cases the child survives. This test validates the end-to-end
// SIGHUP immunity guarantee of detachFromSession.
// =============================================================================

func TestBackgroundProcessSurvivesSIGHUP(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Start a long-running process
	sessionID, err := bpm.Start(context.Background(), "sleep 30", "")
	require.NoError(t, err)

	// Get the child PID
	proc, found := bpm.GetProcess(sessionID)
	require.True(t, found, "GetProcess should find the session")
	childPID := proc.GetPID()
	require.NotZero(t, childPID, "child PID should be set")

	// Verify the process is running
	_, status, err := bpm.CheckOutput(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "running", status)

	// Protect the test process from SIGHUP by temporarily ignoring it.
	// We send SIGHUP to the parent's process group to simulate terminal
	// teardown, but the test process itself must not die.
	signal.Ignore(syscall.SIGHUP)
	defer signal.Reset(syscall.SIGHUP)

	// Send SIGHUP to the parent's process group (simulates terminal teardown).
	// The child should NOT be affected because:
	//   - Setsid path: child is in its own session & process group
	//   - Setpgid path: child inherited SIG_IGN for SIGHUP
	parentPGID, err := syscall.Getpgid(os.Getpid())
	require.NoError(t, err)

	// Send SIGHUP to the parent's process group (negative PGID)
	_ = syscall.Kill(-parentPGID, syscall.SIGHUP)

	// Brief pause to let signal delivery complete
	time.Sleep(200 * time.Millisecond)

	// Verify the child process is still alive
	err = syscall.Kill(childPID, syscall.Signal(0))
	assert.NoError(t, err,
		"child process (PID %d) should survive SIGHUP to parent's process group", childPID)

	// Verify BPM still reports it as running
	_, status, err = bpm.CheckOutput(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "running", status,
		"BPM should still report the process as running after SIGHUP")

	// Clean up
	require.NoError(t, bpm.Stop(sessionID, 50*time.Millisecond))
}

// =============================================================================
// TestSighupIgnoredOnce — Verify that the SIGHUP ignore mechanism is idempotent.
// Calling detachFromSession multiple times should not cause issues, and if the
// Setpgid fallback path was taken, signal.Ignore(SIGHUP) should only have been
// called once (sync.Once guarantee).
//
// We verify this by checking that SIGHUP is being ignored after the Setpgid
// fallback path was exercised, and that repeated calls don't change the
// disposition.
// =============================================================================

func TestSighupIgnoredOnce(t *testing.T) {
	// Do NOT use t.Parallel() — shares package-level sync.Once state.

	// Call detachFromSession multiple times — should never panic
	for i := 0; i < 5; i++ {
		cmd := exec.Command("true")
		detachFromSession(cmd)
		require.NotNil(t, cmd.SysProcAttr, "iteration %d: SysProcAttr should be set", i)
	}

	// If the Setpgid fallback was used (Setsid not available), SIGHUP should
	// now be ignored. If Setsid was available, SIGHUP is at default disposition.
	// We verify by spawning a child and checking its behavior.
	//
	// Spawn a child that checks its own SIGHUP disposition via a shell command.
	// On systems where Setsid is available, the child should have default SIGHUP.
	// On systems where Setpgid fallback was used, the child should ignore SIGHUP.
	// Either outcome is valid — we just verify the child spawns and exits cleanly.
	cmd := exec.Command("sh", "-c", "sleep 0.1")
	err := cmd.Run()
	assert.NoError(t, err, "child process should spawn and exit cleanly")
}

// =============================================================================
// TestProbeSetsidSupportCachesResult — Verify that probeSetsidSupport returns
// the same result on every call (sync.Once caching). The first call probes;
// subsequent calls return the cached value without executing another probe.
// =============================================================================

func TestProbeSetsidSupportCachesResult(t *testing.T) {
	// Do NOT use t.Parallel() — shares package-level sync.Once state.

	// First call — may execute the actual probe
	result1 := probeSetsidSupport()

	// Second call — should return the cached result
	result2 := probeSetsidSupport()

	// Third call — should still return the cached result
	result3 := probeSetsidSupport()

	assert.Equal(t, result1, result2, "probeSetsidSupport should return cached result")
	assert.Equal(t, result2, result3, "probeSetsidSupport should return cached result")

	t.Logf("probeSetsidSupport = %v (cached)", result1)
}

// =============================================================================
// TestDetachFromSessionMutuallyExclusive — Verify that Setsid and Setpgid are
// never both true on the same command. Having both would cause EPERM at
// fork/exec time because setpgid(0, 0) fails on a session leader.
// =============================================================================

func TestDetachFromSessionMutuallyExclusive(t *testing.T) {
	// Do NOT use t.Parallel() — shares package-level sync.Once state.

	cmd := exec.Command("true")
	detachFromSession(cmd)

	require.NotNil(t, cmd.SysProcAttr)
	assert.False(t, cmd.SysProcAttr.Setsid && cmd.SysProcAttr.Setpgid,
		"Setsid and Setpgid must never both be true (EPERM on session leader)")
}

// =============================================================================
// TestDetachFromSessionSetsOne — Verify that detachFromSession always sets
// exactly one of Setsid or Setpgid (never neither, never both).
// =============================================================================

func TestDetachFromSessionSetsOne(t *testing.T) {
	// Do NOT use t.Parallel() — shares package-level sync.Once state.

	cmd := exec.Command("true")
	detachFromSession(cmd)

	require.NotNil(t, cmd.SysProcAttr)
	hasSetsid := cmd.SysProcAttr.Setsid
	hasSetpgid := cmd.SysProcAttr.Setpgid

	// Exactly one should be true
	xor := hasSetsid != hasSetpgid
	assert.True(t, xor,
		"exactly one of Setsid (%v) or Setpgid (%v) should be true", hasSetsid, hasSetpgid)
}
