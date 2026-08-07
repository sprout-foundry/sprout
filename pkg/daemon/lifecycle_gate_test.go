//go:build !js

package daemon

import (
	"context"
	"path/filepath"
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
//     client's health checks then fail, proving the daemon is gone.
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
	_, err = CheckHealth(context.Background(), spec.DaemonURL, 500*time.Millisecond)
	require.Error(t, err, "daemon must reap itself after the idle timeout (health check must fail)")

	// The marker file still records the spawn; nothing more to clean up —
	// the helper already exited (killHelperByMarker is a no-op on dead PIDs).
}
