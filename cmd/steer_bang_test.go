//go:build !js

package cmd

import (
	"testing"
	"time"

	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
)

// TestSteerBangShell_ExecutesMidTurn verifies that a bang command in STEER
// mode is dispatched through the command registry (which translates
// "!cmd" → exec cmd) rather than being rejected or injected into the
// agent's steer channel. Execution happens in a goroutine, so we assert
// the contract: the message must not leak into the deferred queue or the
// steering channel.
func TestSteerBangShell_ExecutesMidTurn(t *testing.T) {
	a := newTestAgent(t)
	a.SetSlashCommands(agent_commands.DefaultRegistry())
	c := NewSteerCoordinator(a, nil)
	_ = a.DrainDeferredMessages()

	c.handleSteerSubmit("!echo hi")

	// Execution runs in a goroutine; give it a moment to settle so the
	// leak assertions below check the post-execution state, not a race.
	time.Sleep(100 * time.Millisecond)
	if drained := a.DrainDeferredMessages(); len(drained) != 0 {
		t.Errorf("steer-executed bang command leaked into deferred queue: %v", drained)
	}
	select {
	case msg := <-a.SteeringChannel():
		t.Errorf("steer-executed bang command leaked into steering channel: %s", msg)
	default:
	}
}

// TestSteerBangShell_GuardedCommandDoesNotPanic verifies that a bang
// command blocked by the exec guards (git checkout) does not panic and
// stays off the agent's channels — the guard's error is printed by the
// execution goroutine.
func TestSteerBangShell_GuardedCommandDoesNotPanic(t *testing.T) {
	a := newTestAgent(t)
	a.SetSlashCommands(agent_commands.DefaultRegistry())
	c := NewSteerCoordinator(a, nil)
	_ = a.DrainDeferredMessages()

	c.handleSteerSubmit("!git checkout main")

	time.Sleep(100 * time.Millisecond)
	if drained := a.DrainDeferredMessages(); len(drained) != 0 {
		t.Errorf("guarded bang command leaked into deferred queue: %v", drained)
	}
	select {
	case msg := <-a.SteeringChannel():
		t.Errorf("guarded bang command leaked into steering channel: %s", msg)
	default:
	}
}

// TestSteerBangShell_QueuedModeEnqueues verifies that a bang command in
// QUEUE mode is enqueued verbatim so the REPL's auto-run path can
// dispatch it through registry.Execute at turn end.
func TestSteerBangShell_QueuedModeEnqueues(t *testing.T) {
	a := newTestAgent(t)
	c := NewSteerCoordinator(a, nil)
	_ = a.DrainDeferredMessages()

	c.handleQueueSubmit("!echo hi")

	drained := a.DrainDeferredMessages()
	if len(drained) != 1 || drained[0] != "!echo hi" {
		t.Errorf("expected queued bang command verbatim, got %v", drained)
	}
}

// TestSteerBangShell_QueuedModeStillRejectsSlash verifies that slash
// commands remain rejected in QUEUE mode (only the bang rejection was
// lifted).
func TestSteerBangShell_QueuedModeStillRejectsSlash(t *testing.T) {
	a := newTestAgent(t)
	c := NewSteerCoordinator(a, nil)
	_ = a.DrainDeferredMessages()

	c.handleQueueSubmit("/commit")

	if drained := a.DrainDeferredMessages(); len(drained) != 0 {
		t.Errorf("rejected slash command leaked into deferred queue: %v", drained)
	}
}

// TestSteerBangShell_QueuedModeRejectsBangExit verifies that "!exit" /
// "!quit" are rejected in QUEUE mode: they dispatch as exec subshells
// (no-ops) rather than ending the session, so queueing them would
// silently do nothing the user meant.
func TestSteerBangShell_QueuedModeRejectsBangExit(t *testing.T) {
	a := newTestAgent(t)
	c := NewSteerCoordinator(a, nil)
	_ = a.DrainDeferredMessages()

	c.handleQueueSubmit("!exit")
	c.handleQueueSubmit("!quit")

	if drained := a.DrainDeferredMessages(); len(drained) != 0 {
		t.Errorf("bang exit/quit leaked into deferred queue: %v", drained)
	}
}

// TestSteerBangShell_SteerModeNoRegistryFallsBack verifies that a bang
// command in STEER mode with no registry on the agent falls back to the
// rejection path (nothing leaks into the channels).
func TestSteerBangShell_SteerModeNoRegistryFallsBack(t *testing.T) {
	a := newTestAgent(t)
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
