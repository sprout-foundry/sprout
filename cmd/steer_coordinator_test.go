//go:build !js

package cmd

import (
	"testing"
)

// SteerCoordinator's interesting behavior is its no-op contract when
// constructed with nil agent/footer — that's how tests and non-TTY
// runs are expected to use it. Full behavior is covered by
// SteerInputReader tests in pkg/console; here we only verify the
// guard rails that prevent crashes / panics.

func TestSteerCoordinator_NilAgentNoPanic(t *testing.T) {
	c := NewSteerCoordinator(nil, nil)
	if c == nil {
		t.Fatal("constructor should never return nil")
	}
	// Both lifecycle calls must be safe on a no-op coordinator.
	c.StartTurn()
	c.EndTurn()
}

func TestSteerCoordinator_NilReaderNoPanic(t *testing.T) {
	// When footer is nil the coordinator's reader is nil too; lifecycle
	// methods should short-circuit cleanly.
	c := NewSteerCoordinator(nil, nil)
	c.StartTurn()
	c.StartTurn() // double-start
	c.EndTurn()
	c.EndTurn() // double-stop
}

func TestSteerCoordinator_SubmitWithNilAgentNoPanic(t *testing.T) {
	// The callbacks must remain crash-safe even when the agent is nil
	// (defensive — shouldn't happen in real usage but cheap to guard).
	c := NewSteerCoordinator(nil, nil)
	c.handleSteerSubmit("anything")
	c.handleSteerInterrupt("")
}

func TestSteerCoordinator_RejectsSlashCommands(t *testing.T) {
	// Slash commands are rejected in both STEER and QUEUE modes. They
	// must not reach the injection / queue channels — the main-prompt
	// dispatch is the only correct path for them.
	a := newTestAgentForIntent(t)
	c := NewSteerCoordinator(a, nil)
	_ = a.DrainDeferredMessages()

	for _, in := range []string{"/commit"} {
		c.handleSteerSubmit(in)
		c.handleQueueSubmit(in)
	}

	if drained := a.DrainDeferredMessages(); len(drained) != 0 {
		t.Errorf("rejected slash command leaked into deferred queue: %v", drained)
	}
}

// TestSteerCoordinator_BangShell_SteerWithoutRegistryRejected verifies that
// a bang command in STEER mode falls back to rejection when no command
// registry is set on the agent (executeSteerShell returns false). The
// message must not leak into the deferred queue or the steering channel.
func TestSteerCoordinator_BangShell_SteerWithoutRegistryRejected(t *testing.T) {
	a := newTestAgentForIntent(t)
	c := NewSteerCoordinator(a, nil)
	_ = a.DrainDeferredMessages()

	c.handleSteerSubmit("!ls")

	if drained := a.DrainDeferredMessages(); len(drained) != 0 {
		t.Errorf("rejected bang command leaked into deferred queue: %v", drained)
	}
	select {
	case msg := <-a.SteeringChannel():
		t.Errorf("rejected bang command leaked into steering channel: %s", msg)
	default:
	}
}

// TestSteerCoordinator_BangShell_QueuedAndAutoRun verifies that a bang
// command in QUEUE mode is enqueued (not rejected) so it auto-runs as its
// own turn at turn end through the REPL dispatch.
func TestSteerCoordinator_BangShell_QueuedAndAutoRun(t *testing.T) {
	a := newTestAgentForIntent(t)
	c := NewSteerCoordinator(a, nil)
	_ = a.DrainDeferredMessages()

	c.handleQueueSubmit("!echo hi")

	drained := a.DrainDeferredMessages()
	if len(drained) != 1 || drained[0] != "!echo hi" {
		t.Errorf("expected bang command in queue, got %v", drained)
	}
}

func TestSteerCoordinator_AllowsFreeformText(t *testing.T) {
	// Plain text must still flow through to the queue (we can't easily
	// assert the steer injection without exercising the seed bridge,
	// but the queue path is a pure in-memory append we can verify).
	a := newTestAgentForIntent(t)
	c := NewSteerCoordinator(a, nil)
	_ = a.DrainDeferredMessages()

	c.handleQueueSubmit("please refactor the auth middleware")

	drained := a.DrainDeferredMessages()
	if len(drained) != 1 || drained[0] != "please refactor the auth middleware" {
		t.Errorf("expected freeform text in queue, got %v", drained)
	}
}
