package webui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestExecuteCommandAndWait_OutrunsSubscriberBuffer pins the fix for a bug
// where a command producing more output chunks than the subscriber buffer
// holds (10000) had its session reported as terminated mid-command.
//
// broadcast() evicts a subscriber whose channel is full, which is right for a
// WebSocket that stopped reading — it lets the reader goroutine keep going and
// the socket reconnect and replay the ring. It is wrong for
// ExecuteCommandAndWait, whose consumer is internal and is waiting on exactly
// that output: eviction closed its channel, the wait loop read the close as
// "PTY session terminated", and the command was reported as failed while it
// was still running and about to succeed.
//
// A 2 MB stream is ~22k chunks against a 10k buffer, so this reliably outran
// the buffer before the fix (failing roughly 6 runs in 10).
func TestExecuteCommandAndWait_OutrunsSubscriberBuffer(t *testing.T) {
	dir := t.TempDir()
	tm := newTestTerminalManager(t, dir)

	session := createAndReadySession(t, tm, "exec-outrun")
	defer tm.CloseSession("exec-outrun")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// ~2 MB: comfortably more chunks than the subscriber buffer holds.
	const lines = 40000
	cmd := fmt.Sprintf(`i=0; while [ $i -lt %d ]; do echo "0123456789012345678901234567890123456789012345678"; i=$((i+1)); done; echo DONE-MARKER`, lines)

	output, exitCode, err := tm.ExecuteCommandAndWait(ctx, session, cmd)
	if err != nil {
		t.Fatalf("ExecuteCommandAndWait failed on a large-output command: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if !strings.Contains(output, "DONE-MARKER") {
		t.Error("output lost the tail marker; the capture should retain the end of the stream")
	}
}

// TestBroadcast_EvictsStalledWebSocketSubscriber verifies the eviction that
// protects the reader is still in place for ordinary subscribers: a
// WebSocket that stops reading must be dropped (and marked) rather than
// blocking the PTY reader forever.
func TestBroadcast_EvictsStalledWebSocketSubscriber(t *testing.T) {
	session := &TerminalSession{ID: "evict-test", ring: newSessRing()}

	sub := session.subscribe()
	defer session.unsubscribe(sub)

	// Fill the buffer without draining, then push one more chunk.
	for i := 0; i < cap(sub.ch); i++ {
		session.broadcast([]byte("x"))
	}
	session.broadcast([]byte("overflow"))

	// The channel must be closed and marked evicted, not left full.
	select {
	case _, ok := <-sub.ch:
		// Drain what is buffered; the close is what we are looking for.
		for range sub.ch { //nolint:revive // draining to observe the close
		}
		_ = ok
	default:
	}

	session.subsMu.Lock()
	evicted := sub.evicted
	session.subsMu.Unlock()
	if !evicted {
		t.Error("a full non-mandatory subscriber should be marked evicted")
	}
}

// TestBroadcast_MandatorySubscriberIsNotEvicted verifies the other half: a
// mandatory subscriber (the ExecuteCommandAndWait consumer) is never dropped
// for being full — the reader waits for it, bounded, instead.
func TestBroadcast_MandatorySubscriberIsNotEvicted(t *testing.T) {
	session := &TerminalSession{ID: "mandatory-test", ring: newSessRing()}

	sub := session.subscribeMandatory()
	defer session.unsubscribe(sub)

	// Drain continuously in the background, as the real consumer does.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range sub.ch {
		}
	}()

	// Push far more than the buffer holds.
	for i := 0; i < cap(sub.ch)+500; i++ {
		session.broadcast([]byte("x"))
	}

	session.subsMu.Lock()
	evicted := sub.evicted
	stillSubscribed := len(session.subs) == 1
	session.subsMu.Unlock()

	if evicted {
		t.Error("a mandatory subscriber must never be marked evicted")
	}
	if !stillSubscribed {
		t.Error("a mandatory subscriber must stay subscribed while it drains")
	}

	session.unsubscribe(sub)
	close(sub.ch)
	<-done
}
