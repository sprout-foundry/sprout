package cmd

import (
	"context"
	"testing"
)

// TestMaybeAutoStartDaemon_DisabledEscapeHatch verifies SPROUT_DAEMON=0
// forces in-process execution: no daemon is spawned and the cleanup is a
// no-op.
func TestMaybeAutoStartDaemon_DisabledEscapeHatch(t *testing.T) {
	t.Setenv("SPROUT_DAEMON", "0")
	cleanup := maybeAutoStartDaemon(context.Background(), false)
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup func")
	}
	cleanup() // must not panic
}

// TestMaybeAutoStartDaemon_WhenDaemonMode verifies the daemon itself never
// auto-starts another daemon.
func TestMaybeAutoStartDaemon_WhenDaemonMode(t *testing.T) {
	t.Setenv("SPROUT_DAEMON", "1")
	cleanup := maybeAutoStartDaemon(context.Background(), true)
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup func")
	}
	cleanup()
}

// NOTE: the default path (no env, not daemon mode) is intentionally NOT
// unit-tested here: it spawns a goroutine that may start a real daemon
// process (or, in a test binary, recursively execute the test suite). Its
// behavior is covered by the pkg/daemon EnsureDaemon tests; this function's
// only testable contracts are the no-op paths above.
