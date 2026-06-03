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

func TestSteerCoordinator_RejectsCommandIntents(t *testing.T) {
	// Slash, bang, and direct fast-path inputs must NOT reach the
	// agent's injection / queue channels — the main-prompt dispatch
	// is the only correct path for them, and silently routing them
	// here would either echo the command to the LLM (steer) or wrap
	// it as blockquote text on the next turn (queue), both of which
	// drop the user's command semantics.
	a := newTestAgentForIntent(t)
	c := NewSteerCoordinator(a, nil)
	// Drain any pre-existing queue state so we can assert "zero"
	// after the rejection paths run.
	_ = a.DrainDeferredMessages()

	for _, in := range []string{"/commit", "!ls", "pwd", "git status"} {
		c.handleSteerSubmit(in)
		c.handleQueueSubmit(in)
	}

	if drained := a.DrainDeferredMessages(); len(drained) != 0 {
		t.Errorf("rejected command intents leaked into deferred queue: %v", drained)
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
