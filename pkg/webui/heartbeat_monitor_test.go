//go:build !js

package webui

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// newTestMonitorServer creates a ReactWebServer with an event bus for
// heartbeat monitor tests.
func newTestMonitorServer(t *testing.T) *ReactWebServer {
	t.Helper()
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return server
}

// TestCheckStaleConnectionsNoStaleClients verifies that a client with a
// recent LastSeenAt is NOT considered stale even if it has an active query.
func TestCheckStaleConnectionsNoStaleClients(t *testing.T) {
	server := newTestMonitorServer(t)

	ctx := &webClientContext{
		LastSeenAt:  time.Now(),
		ActiveQuery: true,
	}
	server.clientContexts["fresh-client"] = ctx

	server.checkStaleConnections()

	// ActiveQuery should remain true — this client is not stale
	if !ctx.ActiveQuery {
		t.Error("expected ActiveQuery to remain true for fresh client")
	}
}

// TestCheckStaleConnectionsStaleClientWithActiveQuery verifies that a client
// with an old LastSeenAt AND an active query is detected as stale and has
// its query cancelled.
func TestCheckStaleConnectionsStaleClientWithActiveQuery(t *testing.T) {
	server := newTestMonitorServer(t)

	ctx := &webClientContext{
		LastSeenAt:  time.Now().Add(-61 * time.Second),
		ActiveQuery: true,
	}
	server.clientContexts["stale-client"] = ctx

	server.checkStaleConnections()

	// ActiveQuery should be false after cancellation
	if ctx.ActiveQuery {
		t.Error("expected ActiveQuery to be false after stale client cancellation")
	}
}

// TestCheckStaleConnectionsStaleClientNoActiveQuery verifies that a stale
// client WITHOUT an active query is NOT cancelled. Only stale clients with
// active queries should be affected.
func TestCheckStaleConnectionsStaleClientNoActiveQuery(t *testing.T) {
	server := newTestMonitorServer(t)

	ctx := &webClientContext{
		LastSeenAt:  time.Now().Add(-61 * time.Second),
		ActiveQuery: false,
	}
	server.clientContexts["stale-no-query"] = ctx

	server.checkStaleConnections()

	// ActiveQuery should remain false — nothing should have changed
	if ctx.ActiveQuery {
		t.Error("expected ActiveQuery to remain false for stale client without active query")
	}
}

// TestCheckStaleConnectionsMultipleClients verifies that when multiple clients
// exist, only the ones that are both stale AND have an active query get cancelled.
func TestCheckStaleConnectionsMultipleClients(t *testing.T) {
	server := newTestMonitorServer(t)

	// Client 1: fresh with active query — should NOT be cancelled
	server.clientContexts["fresh-active"] = &webClientContext{
		LastSeenAt:  time.Now(),
		ActiveQuery: true,
	}

	// Client 2: stale with active query — SHOULD be cancelled
	staleActive := &webClientContext{
		LastSeenAt:  time.Now().Add(-61 * time.Second),
		ActiveQuery: true,
	}
	server.clientContexts["stale-active"] = staleActive

	// Client 3: stale without active query — should NOT be cancelled
	server.clientContexts["stale-inactive"] = &webClientContext{
		LastSeenAt:  time.Now().Add(-61 * time.Second),
		ActiveQuery: false,
	}

	server.checkStaleConnections()

	// Fresh + active should remain active
	if !server.clientContexts["fresh-active"].ActiveQuery {
		t.Error("expected fresh-active client to remain active")
	}

	// Stale + active should be cancelled
	if staleActive.ActiveQuery {
		t.Error("expected stale-active client to be cancelled")
	}

	// Stale + inactive should remain unchanged
	if server.clientContexts["stale-inactive"].ActiveQuery {
		t.Error("expected stale-inactive client to remain inactive")
	}
}

// TestCancelQueryForStaleClientNilContext verifies that cancelling a query
// for a nonexistent client does not panic.
func TestCancelQueryForStaleClientNilContext(t *testing.T) {
	server := newTestMonitorServer(t)

	// Should not panic when client context doesn't exist
	server.cancelQueryForStaleClient("nonexistent-client")
}

// TestCancelQueryForStaleClientNilAgent verifies that a stale client with
// no Agent still gets its query decremented and event published. This is
// the common case when a client's WebSocket drops before an agent is created.
func TestCancelQueryForStaleClientNilAgent(t *testing.T) {
	server := newTestMonitorServer(t)

	ctx := &webClientContext{
		LastSeenAt:  time.Now().Add(-61 * time.Second),
		ActiveQuery: true,
		Agent:       nil, // No agent — should still clean up gracefully
	}
	server.clientContexts["no-agent-client"] = ctx

	// Set up event bus subscription to verify the query_cancelled event
	eventCh := server.eventBus.Subscribe("test-cleanup")
	defer server.eventBus.Unsubscribe("test-cleanup")

	server.cancelQueryForStaleClient("no-agent-client")

	// ActiveQuery should be false after decrementActiveQueries
	if ctx.ActiveQuery {
		t.Error("expected ActiveQuery to be false after cancellation")
	}

	// Should receive the query_cancelled event
	select {
	case event := <-eventCh:
		if event.Type != "query_cancelled" {
			t.Errorf("expected event type query_cancelled, got %s", event.Type)
		}
		if data, ok := event.Data.(map[string]interface{}); ok {
			if data["reason"] != "heartbeat_timeout" {
				t.Errorf("expected reason=heartbeat_timeout, got %v", data["reason"])
			}
			if data["message"] != "Query cancelled: no heartbeat received for 60 seconds" {
				t.Errorf("unexpected message: %v", data["message"])
			}
		} else {
			t.Error("expected event data to be map[string]interface{}")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for query_cancelled event")
	}
}

// TestStartHeartbeatMonitorStopsOnContextCancel verifies that the heartbeat
// monitor goroutine exits cleanly when its context is cancelled, preventing
// goroutine leaks.
func TestStartHeartbeatMonitorStopsOnContextCancel(t *testing.T) {
	server := newTestMonitorServer(t)

	ctx, cancel := context.WithCancel(context.Background())

	// Start the heartbeat monitor in a goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		server.startHeartbeatMonitor(ctx)
	}()

	// Cancel the context immediately — the monitor should exit
	cancel()

	// Wait for the goroutine to finish with a timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Monitor exited cleanly
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat monitor did not stop after context cancellation")
	}
}

// TestCancelQueryForStaleClientNoLongerStale verifies that a client that
// becomes fresh between the scan in checkStaleConnections and the call to
// cancelQueryForStaleClient is NOT cancelled. This catches the TOCTOU
// race where a client sends a heartbeat just after being flagged as stale.
func TestCancelQueryForStaleClientNoLongerStale(t *testing.T) {
	server := newTestMonitorServer(t)

	ctx := &webClientContext{
		LastSeenAt:  time.Now().Add(-61 * time.Second),
		ActiveQuery: true,
	}
	server.clientContexts["race-client"] = ctx

	// Simulate client sending heartbeat between scan and cancel
	ctx.LastSeenAt = time.Now()

	server.cancelQueryForStaleClient("race-client")

	// ActiveQuery should remain true since client is no longer stale
	if !ctx.ActiveQuery {
		t.Error("expected ActiveQuery to remain true — client was no longer stale")
	}
}
