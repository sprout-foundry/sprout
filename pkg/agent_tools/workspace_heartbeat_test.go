// Package tools provides the interface-based tool system for the Sprout AI agent.
//
// This file tests the HeartbeatMonitor (SP-046-4) — timing-based integration
// tests that exercise the real ticker goroutine rather than mocked clocks.

package tools

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// mockPublisher captures events published by the monitor. Thread-safe for
// concurrent use from the monitor goroutine.
type mockPublisher struct {
	mu     sync.Mutex
	events []struct {
		eventType string
		data      any
	}
}

func (m *mockPublisher) Publish(eventType string, data any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, struct {
		eventType string
		data      any
	}{eventType, data})
}

func (m *mockPublisher) getEvents() []struct {
	eventType string
	data      any
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]struct {
		eventType string
		data      any
	}, len(m.events))
	copy(out, m.events)
	return out
}

func TestHeartbeat_SessionAliveWithRegularBeats(t *testing.T) {
	publisher := &mockPublisher{}
	monitor := NewHeartbeatMonitor(publisher)
	defer monitor.Stop()

	var terminated atomic.Bool
	var terminatedID atomic.Value // string

	monitor.StartMonitor(20*time.Millisecond, 50*time.Millisecond)

	monitor.RegisterJob("test-1", func(id string) {
		terminated.Store(true)
		terminatedID.Store(id)
	})

	// Send heartbeats every 10ms for 100ms — well within the 50ms threshold.
	stopBeats := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopBeats:
				return
			case <-ticker.C:
				monitor.RecordHeartbeat("test-1", time.Now())
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)
	close(stopBeats)
	monitor.Stop()

	if monitor.GetActiveCount() != 1 {
		t.Fatalf("GetActiveCount() = %d; want 1 (session should still be alive)", monitor.GetActiveCount())
	}

	if terminated.Load() {
		t.Errorf("terminate callback was called; it should NOT have been called for a healthy session")
	}

	evts := publisher.getEvents()
	if len(evts) > 0 {
		t.Errorf("expected no published events; got %d events", len(evts))
	}
}

func TestHeartbeat_MissedHeartbeatTriggersTermination(t *testing.T) {
	publisher := &mockPublisher{}
	monitor := NewHeartbeatMonitor(publisher)
	defer monitor.Stop()

	var terminated atomic.Bool
	var terminatedID atomic.Value // string

	monitor.StartMonitor(20*time.Millisecond, 50*time.Millisecond)

	monitor.RegisterJob("test-2", func(id string) {
		terminated.Store(true)
		terminatedID.Store(id)
	})

	// Send ONE heartbeat at t=0, then stop — simulate tab close.
	monitor.RecordHeartbeat("test-2", time.Now())

	// Wait for threshold (50ms) plus buffer for ticker alignment (3 ticks @ 20ms).
	// By 150ms the monitor has had several checks past the threshold window.
	time.Sleep(150 * time.Millisecond)

	monitor.Stop()

	if monitor.GetActiveCount() != 0 {
		t.Fatalf("GetActiveCount() = %d; want 0 (session should have been removed)", monitor.GetActiveCount())
	}

	if !terminated.Load() {
		t.Fatalf("terminate callback was NOT called; it should have been triggered after threshold")
	}

	idVal := terminatedID.Load()
	if idVal == nil || idVal.(string) != "test-2" {
		t.Errorf("terminate callback sessionID = %v; want %q", idVal, "test-2")
	}

	evts := publisher.getEvents()
	if len(evts) == 0 {
		t.Fatal("expected at least one workspace.heartbeat_lost event; got none")
	}

	found := false
	for _, e := range evts {
		if e.eventType == events.EventTypeWorkspaceHeartbeatLost {
			found = true
			if e.data == nil {
				t.Errorf("heartbeat_lost event has nil data payload")
			} else {
				payload := e.data.(map[string]interface{})
				if sid, ok := payload["session_id"].(string); ok && sid != "test-2" {
					t.Errorf("heartbeat_lost session_id = %q; want %q", sid, "test-2")
				} else if !ok {
					t.Errorf("heartbeat_lost payload missing session_id string")
				}
			}
			break
		}
	}
	if !found {
		t.Errorf("expected workspace.heartbeat_lost event; got event types: %v", func() []string {
			var types []string
			for _, e := range evts {
				types = append(types, e.eventType)
			}
			return types
		}())
	}
}

func TestHeartbeat_TransientBlipTolerated(t *testing.T) {
	publisher := &mockPublisher{}
	monitor := NewHeartbeatMonitor(publisher)
	defer monitor.Stop()

	var terminated atomic.Bool
	var terminatedID atomic.Value // string

	monitor.StartMonitor(20*time.Millisecond, 80*time.Millisecond)

	monitor.RegisterJob("test-3", func(id string) {
		terminated.Store(true)
		terminatedID.Store(id)
	})

	// Beat at t=0.
	monitor.RecordHeartbeat("test-3", time.Now())

	// Wait 50ms — a "blip" with no heartbeat, but within the 80ms threshold.
	time.Sleep(50 * time.Millisecond)

	// At this point the session should still be alive (50ms gap < 80ms threshold).
	// The monitor checks every 20ms so it may have checked a couple times already,
	// but the gap is still within threshold.
	if monitor.GetActiveCount() != 1 {
		t.Fatalf("after 50ms gap: GetActiveCount() = %d; want 1 (blip should be tolerated)", monitor.GetActiveCount())
	}
	if terminated.Load() {
		t.Fatalf("terminate callback called during blip; it should NOT have been called yet")
	}

	// Send another heartbeat at t=50ms (resets the clock).
	monitor.RecordHeartbeat("test-3", time.Now())

	// Now wait 200ms with no further heartbeats.
	// The total gap from the last heartbeat is 200ms > 80ms threshold,
	// so the monitor should detect and terminate.
	// Allow extra time for ticker alignment: 200ms wait + up to 40ms (2 ticks).
	time.Sleep(200 * time.Millisecond)

	// Give one more ticker cycle to ensure the check runs after threshold.
	time.Sleep(40 * time.Millisecond)

	monitor.Stop()

	if monitor.GetActiveCount() != 0 {
		t.Fatalf("GetActiveCount() = %d; want 0 (session should have been terminated after sustained gap)", monitor.GetActiveCount())
	}

	if !terminated.Load() {
		t.Fatalf("terminate callback was NOT called; it should have been triggered after sustained gap exceeds threshold")
	}

	idVal3 := terminatedID.Load()
	if idVal3 == nil || idVal3.(string) != "test-3" {
		t.Errorf("terminate callback sessionID = %v; want %q", idVal3, "test-3")
	}

	evts := publisher.getEvents()
	found := false
	for _, e := range evts {
		if e.eventType == events.EventTypeWorkspaceHeartbeatLost {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected workspace.heartbeat_lost event after sustained gap; got %d events total", len(evts))
	}
}
