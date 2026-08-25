package events

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPublish_NoGoroutinePerPublish is the regression test for the SP-128
// macOS freeze: at high publish rates, goroutine count must NOT scale with
// the number of publishes. Coalescing is intentionally a no-op at the bus
// level (see coalesceBatch); the WS write path in pkg/webui/stream_coalesce.go
// performs coalescing when draining its receive channel, which is the
// right place to batch (events have already accumulated by then).
func TestPublish_NoGoroutinePerPublish(t *testing.T) {
	eb := NewEventBus()
	defer cleanupBus(eb)

	ch := eb.Subscribe("sub")
	drainAll(ch) // discard initial state

	runtime.GC()
	base := runtime.NumGoroutine()

	const N = 500
	for i := 0; i < N; i++ {
		eb.Publish(EventTypeStreamChunk, StreamChunkEvent("x", "assistant_text"))
	}

	// Allow the dispatcher to drain.
	for i := 0; i < N; i++ {
		<-ch
	}

	runtime.GC()
	peak := runtime.NumGoroutine() - base
	// Historical code created 1 goroutine per (publish × subscriber).
	// After SP-128, the bus has 1 dispatcher + 1 worker per subscriber,
	// total = 2 extra goroutines regardless of N.
	if peak > 10 {
		t.Errorf("goroutine count grew too much during streaming (%d extra goroutines for %d publishes); SP-128 design should cap at ~1 dispatcher + 1 worker per subscriber", peak, N)
	}
}

// TestPublish_HighRateDoesNotStall measures publish latency at the
// historical stall trigger rate (200 events/sec across 4 subscribers).
// With SP-128, no individual publish should exceed a few milliseconds.
func TestPublish_HighRateDoesNotStall(t *testing.T) {
	eb := NewEventBus()
	defer cleanupBus(eb)

	const numSubs = 4
	for i := 0; i < numSubs; i++ {
		ch := eb.Subscribe(string(rune('a' + i)))
		go func(c <-chan UIEvent) {
			for range c {
			}
		}(ch)
	}

	const N = 200
	var maxLatency int64
	var wg sync.WaitGroup
	wg.Add(N)
	start := time.Now()
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			t0 := time.Now()
			eb.Publish(EventTypeStreamChunk, StreamChunkEvent("x", "assistant_text"))
			d := time.Since(t0).Microseconds()
			if d > atomic.LoadInt64(&maxLatency) {
				atomic.StoreInt64(&maxLatency, d)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	avgPerPublish := elapsed / N
	t.Logf("avg=%v max=%vµs (over %d publishes across %d subscribers)", avgPerPublish, maxLatency, N, numSubs)
	if maxLatency > 50*1000 { // 50ms — anything worse suggests the stall pathology
		t.Errorf("max publish latency %dµs is in the stall-trigger range (>50ms); expected sub-millisecond", maxLatency)
	}
}

// TestPublish_CriticalEventStillDrains verifies the critical-event
// drain-replace path on the worker's receive channel still works after
// the SP-128 redesign. Critical events are NEVER silently dropped when
// the channel is full.
func TestPublish_CriticalEventStillDrains(t *testing.T) {
	eb := NewEventBus()
	defer cleanupBus(eb)

	// Bare channel manually attached (mimics the historical test shape):
	// the worker pool fallback path uses forwardToReceive which applies
	// the drain-replace policy.
	ch := make(chan UIEvent, 1)
	eb.mutex.Lock()
	eb.subscribers["raw"] = ch
	eb.mutex.Unlock()

	// Fill the buffer with a non-critical event.
	eb.Publish(EventTypeAgentMessage, AgentMessageEvent("info", "old", nil))

	// Critical must drain and deliver itself.
	eb.Publish(EventTypeSecurityApprovalRequest, SecurityApprovalRequestEvent("req-1", "tool", "high", "reason", nil))

	select {
	case ev := <-ch:
		if ev.Type != EventTypeSecurityApprovalRequest {
			t.Fatalf("expected critical event to drain a non-critical slot; got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("critical event did not arrive within 1s")
	}
}

// TestPublish_UnsubscribeStopsWorker verifies that Unsubscribe terminates
// the worker's goroutine cleanly without leaking.
func TestPublish_UnsubscribeStopsWorker(t *testing.T) {
	eb := NewEventBus()
	runtime.GC()
	base := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		ch := eb.Subscribe(string(rune('a' + i)))
		drainAll(ch)
	}
	for i := 0; i < 20; i++ {
		eb.Unsubscribe(string(rune('a' + i)))
	}

	// Allow goroutines to wind down.
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	after := runtime.NumGoroutine() - base

	// Dispatcher (1) plus a small slack. Workers must have exited.
	if after > 5 {
		t.Errorf("workers leaked: %d goroutines remain after Unsubscribe", after)
	}
}

// --- helpers ---

func drainAll(ch <-chan UIEvent) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func cleanupBus(eb *EventBus) {
	eb.mutex.Lock()
	for name := range eb.subscribers {
		delete(eb.subscribers, name)
	}
	for name, w := range eb.workers {
		close(w.done)
		delete(eb.workers, name)
	}
	eb.mutex.Unlock()
}
