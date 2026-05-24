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
