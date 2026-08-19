//go:build !js

package cmd

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// These tests exercise the SteerCoordinator's callbacks against a real
// Agent (the deferred queue half — the InjectInputContext path
// requires a running ProcessQuery + seed bridge which can't be unit-
// tested without a provider). They catch regressions where the
// coordinator's queue callback gets wired to the wrong channel or the
// agent's queue API drifts.

func TestSteerCoordinator_QueueSubmitGoesToDeferredQueue(t *testing.T) {
	a := &agent.Agent{}
	c := &SteerCoordinator{agent: a}

	c.handleQueueSubmit("focus on perf")
	c.handleQueueSubmit("then write tests")

	queued := a.DrainDeferredMessages()
	if len(queued) != 2 {
		t.Fatalf("expected 2 queued messages, got %d", len(queued))
	}
	if queued[0] != "focus on perf" || queued[1] != "then write tests" {
		t.Fatalf("queue order wrong: %v", queued)
	}
}

func TestSteerCoordinator_QueueSubmitEmptyIsNoop(t *testing.T) {
	a := &agent.Agent{}
	c := &SteerCoordinator{agent: a}

	c.handleQueueSubmit("")
	if got := a.DeferredMessageCount(); got != 0 {
		t.Fatalf("empty queue submit should be a no-op, got %d items", got)
	}
}

func TestSteerCoordinator_QueueRejectsREPLExit(t *testing.T) {
	a := &agent.Agent{}
	c := &SteerCoordinator{agent: a}

	// A queued "exit"/"quit" would auto-run into the REPL's exit branch
	// at turn end and terminate the session — enqueue must reject it.
	for _, text := range []string{"exit", "quit", "  EXIT  ", "Quit"} {
		c.handleQueueSubmit(text)
	}
	if got := a.DeferredMessageCount(); got != 0 {
		t.Fatalf("exit/quit variants must be rejected, got %d queued", got)
	}
}

func TestSteerCoordinator_QueueWithNilAgentDropsSilently(t *testing.T) {
	// Defensive: a nil-agent coordinator (e.g. non-TTY run with no
	// session) should swallow queue submissions without panicking.
	c := &SteerCoordinator{agent: nil}
	c.handleQueueSubmit("anything")
	// no assertion — survival without panic is the contract.
}

func TestSteerCoordinator_RetractPullsBackStagedSteer(t *testing.T) {
	a := &agent.Agent{}
	c := &SteerCoordinator{agent: a}

	if err := a.InjectInputContext("fix typo plz"); err != nil {
		t.Fatalf("stage steer: %v", err)
	}

	text, ok := c.handleSteerRetract()
	if !ok || text != "fix typo plz" {
		t.Fatalf("expected staged steer pulled back, got %q ok=%v", text, ok)
	}
	if _, ok := c.handleSteerRetract(); ok {
		t.Fatal("second retract should find nothing pending")
	}
}

func TestSteerCoordinator_RetractFallsBackToDeferredQueue(t *testing.T) {
	a := &agent.Agent{}
	c := &SteerCoordinator{agent: a}

	c.handleQueueSubmit("queued for next turn")

	text, ok := c.handleSteerRetract()
	if !ok || text != "queued for next turn" {
		t.Fatalf("expected queued message pulled back, got %q ok=%v", text, ok)
	}
	if got := a.DeferredMessageCount(); got != 0 {
		t.Fatalf("queue should be empty after retract, got %d", got)
	}
}

func TestSteerCoordinator_RetractPrefersSteerOverQueue(t *testing.T) {
	a := &agent.Agent{}
	c := &SteerCoordinator{agent: a}

	_ = a.InjectInputContext("staged steer")
	c.handleQueueSubmit("queued message")

	text, ok := c.handleSteerRetract()
	if !ok || text != "staged steer" {
		t.Fatalf("steer staging should win over queue, got %q ok=%v", text, ok)
	}
}

func TestSteerCoordinator_RetractNilAgentSafe(t *testing.T) {
	c := &SteerCoordinator{agent: nil}
	if _, ok := c.handleSteerRetract(); ok {
		t.Fatal("nil-agent retract must return false")
	}
}

func TestDrainPendingInput_ReturnsRawMessages(t *testing.T) {
	a := &agent.Agent{}
	c := &SteerCoordinator{agent: a}

	c.handleQueueSubmit("focus on perf")
	c.handleQueueSubmit("then write tests")

	pi := c.DrainPendingInput()
	if len(pi.QueuedMessages) != 2 {
		t.Fatalf("expected 2 queued messages, got %d", len(pi.QueuedMessages))
	}
	if pi.QueuedMessages[0] != "focus on perf" || pi.QueuedMessages[1] != "then write tests" {
		t.Fatalf("queue order wrong: %v", pi.QueuedMessages)
	}
	if pi.QueuedCount != 2 {
		t.Fatalf("expected QueuedCount == 2, got %d", pi.QueuedCount)
	}
	if pi.InitialContent != "" {
		t.Fatalf("expected InitialContent to be empty, got %q", pi.InitialContent)
	}
}

func TestDrainPendingInput_NoMessages(t *testing.T) {
	a := &agent.Agent{}
	c := &SteerCoordinator{agent: a}

	pi := c.DrainPendingInput()
	if len(pi.QueuedMessages) != 0 {
		t.Fatalf("expected 0 queued messages, got %d", len(pi.QueuedMessages))
	}
	if pi.QueuedCount != 0 {
		t.Fatalf("expected QueuedCount == 0, got %d", pi.QueuedCount)
	}
}

func TestDrainPendingInput_NilAgent(t *testing.T) {
	c := &SteerCoordinator{agent: nil}
	pi := c.DrainPendingInput()
	if len(pi.QueuedMessages) != 0 {
		t.Fatalf("expected 0 queued messages for nil agent, got %d", len(pi.QueuedMessages))
	}
	if pi.QueuedCount != 0 {
		t.Fatalf("expected QueuedCount == 0 for nil agent, got %d", pi.QueuedCount)
	}
}
