//go:build !js

package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDaemonFullLifecycle is the SP-136 Phase-2 gate test.
//
// It drives the complete daemon lifecycle in a clean, hermetic environment:
//
//  1. START: EnsureDaemon elects a leader and spawns the (fake) daemon,
//     waiting until /health reports OK.
//  2. CONNECT + QUERY: the client checks health through the daemon ("query"),
//     confirming the daemon is usable.
//  3. DISCONNECT: the client stops using the daemon (no more requests).
//  4. REAP: the daemon notices the idle window and shuts itself down; the
//     helper process exits, proving the daemon is gone.
//
// The fake daemon (TestDaemonHelperProcess with SPROUT_DAEMON_HELPER_IDLE_MS)
// models the real daemon's SPROUT_DAEMON_IDLE_TIMEOUT reaping behavior.
func TestDaemonFullLifecycle(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "spawns.txt")

	// Fake daemon reaps itself after 1.5s with no requests.
	t.Setenv("SPROUT_DAEMON_HELPER_IDLE_MS", "1500")

	spec := helperSpec(t, tmpDir, port, 100, markerFile)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. START: EnsureDaemon spawns the daemon and waits for health.
	already, err := EnsureDaemon(ctx, spec)
	require.NoError(t, err, "EnsureDaemon must start the daemon")
	assert.False(t, already, "clean environment must require a fresh start")

	// 2. CONNECT + QUERY: exercise the daemon like a client would.
	status, err := CheckHealth(ctx, spec.DaemonURL, 2*time.Second)
	require.NoError(t, err, "client health check through daemon must succeed")
	require.NotNil(t, status)
	assert.Equal(t, "ok", status.Status)

	// A second "query" to keep the daemon busy right up to the disconnect.
	_, err = CheckHealth(ctx, spec.DaemonURL, 2*time.Second)
	require.NoError(t, err)

	// 3. DISCONNECT: stop issuing requests entirely. NOTE: we must NOT poll
	// here — every health check is a request that resets the fake daemon's
	// idle timer. Sleep past the idle window (1.5s) with zero traffic.
	time.Sleep(2500 * time.Millisecond)

	// 4. REAP: the daemon should have noticed the idle window and exited.
	//
	// Assert on the helper PROCESS exiting, not on "the port no longer
	// serves /health". On a busy machine a freed ephemeral port can be
	// rebound by an unrelated process (or a recycled PID) within the check
	// window, which makes the port-based assertion flaky. The lifecycle
	// invariant is that the daemon process terminates itself after idle —
	// that is what we verify here.
	helperPIDs := readHelperPIDs(t, markerFile)
	require.NotEmpty(t, helperPIDs, "marker file must record the spawned helper")
	requireProcessesExit(t, helperPIDs, 10*time.Second, filepath.Join(tmpDir, "daemon.log"))

	// The marker file still records the spawn; nothing more to clean up —
	// the helper already exited (killHelperByMarker is a no-op on dead PIDs).
}

// readHelperPIDs parses the helper marker file (one PID per line).
func readHelperPIDs(t *testing.T, markerFile string) []int {
	t.Helper()
	content, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("read marker file: %v", err)
	}
	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(line, "%d", &pid); err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// requireProcessesExit polls each PID until it is terminated or the
// deadline passes. "Terminated" means the process is gone OR is a zombie
// (state 'Z'): the detached helper's parent never reaps it (by design it
// survives the caller), so after its idle reaper calls os.Exit the process
// lingers as a zombie. A zombie's file descriptors are closed — the daemon
// has fully shut down — which is the lifecycle invariant under test.
func requireProcessesExit(t *testing.T, pids []int, timeout time.Duration, logPath string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for _, pid := range pids {
		for {
			state := processState(pid) // "", "Z", "S", "R", ...
			if state == "" || strings.Contains(state, "Z") {
				break // gone, or zombie (terminated but not yet reaped)
			}
			if time.Now().After(deadline) {
				if b, err := os.ReadFile(logPath); err == nil {
					t.Logf("daemon.log at timeout:\n%s", string(b))
				}
				t.Fatalf("helper pid %d did not terminate within %v (state %q; idle reaper did not fire)", pid, timeout, state)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// processState returns the process state character via `ps -o stat=`,
// or "" when the process no longer exists.
func processState(pid int) string {
	out, err := exec.Command("ps", "-o", "stat=", "-p", fmt.Sprintf("%d", pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
