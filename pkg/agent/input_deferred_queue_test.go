package agent

import (
	"sync"
	"testing"
)

// The deferred queue is a session-scoped FIFO of steer messages held
// for the next user-prompted turn (SP-055 Phase 3b). These tests
// cover the small Enqueue/Drain/Count surface with concurrent-safe
// behavior since the CLI's steer goroutine and the REPL loop hit it
// from different threads.

func TestDeferredQueue_EnqueueAndDrain(t *testing.T) {
	a := &Agent{}
	a.EnqueueDeferredMessage("first")
	a.EnqueueDeferredMessage("second")
	a.EnqueueDeferredMessage("third")

	if got := a.DeferredMessageCount(); got != 3 {
		t.Fatalf("expected 3 queued, got %d", got)
	}
	drained := a.DrainDeferredMessages()
	if len(drained) != 3 {
		t.Fatalf("expected 3 drained, got %d", len(drained))
	}
	if drained[0] != "first" || drained[2] != "third" {
		t.Fatalf("FIFO order broken: %v", drained)
	}
	if got := a.DeferredMessageCount(); got != 0 {
		t.Fatalf("drain should empty queue, got %d remaining", got)
	}
}

func TestDeferredQueue_EmptyDrainReturnsNil(t *testing.T) {
	a := &Agent{}
	if got := a.DrainDeferredMessages(); got != nil {
		t.Fatalf("expected nil on empty drain, got %v", got)
	}
}

func TestDeferredQueue_EmptyStringIgnored(t *testing.T) {
	a := &Agent{}
	a.EnqueueDeferredMessage("")
	a.EnqueueDeferredMessage("  ") // whitespace is preserved (no auto-trim here)
	a.EnqueueDeferredMessage("real")

	got := a.DrainDeferredMessages()
	// Empty string is dropped at the Enqueue boundary; whitespace passes
	// through because trimming is the caller's responsibility.
	if len(got) != 2 {
		t.Fatalf("expected 2 (ws + real), got %d: %v", len(got), got)
	}
	if got[1] != "real" {
		t.Fatalf("expected 'real' as second entry, got %q", got[1])
	}
}

func TestDeferredQueue_NilAgentSafe(t *testing.T) {
	var a *Agent
	a.EnqueueDeferredMessage("anything")
	if got := a.DrainDeferredMessages(); got != nil {
		t.Fatalf("nil agent should drain nil, got %v", got)
	}
	if got := a.DeferredMessageCount(); got != 0 {
		t.Fatalf("nil agent count should be 0, got %d", got)
	}
}

func TestDeferredQueue_BoundedAtCap(t *testing.T) {
	a := &Agent{}
	for i := 0; i < deferredQueueCap+10; i++ {
		a.EnqueueDeferredMessage("msg")
	}
	// "msg" is the same string repeatedly so the queue holds N copies
	// — there's no dedupe; only the cap clamps.
	if got := a.DeferredMessageCount(); got != deferredQueueCap {
		t.Fatalf("expected cap=%d, got %d", deferredQueueCap, got)
	}
}

func TestDeferredQueue_ConcurrentSafe(t *testing.T) {
	// Many goroutines enqueueing while one drains — no race / no panic.
	a := &Agent{}
	var wg sync.WaitGroup
	wg.Add(3)
	for g := 0; g < 3; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				a.EnqueueDeferredMessage("x")
			}
		}()
	}
	wg.Wait()
	// Total enqueued: 300, but cap=32 → final count clamped to cap.
	got := a.DeferredMessageCount()
	if got != deferredQueueCap {
		t.Fatalf("expected cap, got %d", got)
	}
	drained := a.DrainDeferredMessages()
	if len(drained) != deferredQueueCap {
		t.Fatalf("drain should match count, got %d", len(drained))
	}
}
