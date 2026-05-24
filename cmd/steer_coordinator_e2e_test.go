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

func TestSteerCoordinator_QueueWithNilAgentDropsSilently(t *testing.T) {
	// Defensive: a nil-agent coordinator (e.g. non-TTY run with no
	// session) should swallow queue submissions without panicking.
	c := &SteerCoordinator{agent: nil}
	c.handleQueueSubmit("anything")
	// no assertion — survival without panic is the contract.
}
