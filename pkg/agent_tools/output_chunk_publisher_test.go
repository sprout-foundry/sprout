//go:build !js

package tools

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ============================================================================
// OutputChunkPublisher — Threshold Tests
// ============================================================================

// TestOutputChunkPublisher_4KBThreshold writes exactly 4096 bytes in a single
// Write() call and verifies that an automate.output_chunk event is published
// immediately with offset=0 and chunk_len=4096.
func TestOutputChunkPublisher_4KBThreshold(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	pub := NewOutputChunkPublisher("test-session", eb)

	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 256)
	}

	n, err := pub.Write(data)
	require.NoError(t, err)
	assert.Equal(t, 4096, n)

	// Event should arrive on the subscriber channel immediately
	ev := requireEvent(t, sub, "4KB threshold trigger")
	assert.Equal(t, events.EventTypeAutomateOutputChunk, ev.Type)
	d := ev.Data.(map[string]interface{})
	assert.Equal(t, "test-session", d["session_id"])
	assert.Equal(t, 0, d["offset"])
	assert.Equal(t, 4096, d["chunk_len"])
}

// TestOutputChunkPublisher_Over4KB writes 5000 bytes (more than the 4KB
// threshold) and verifies an event is published with chunk_len=5000 and
// offset=0.
func TestOutputChunkPublisher_Over4KB(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	pub := NewOutputChunkPublisher("over4kb-session", eb)

	data := make([]byte, 5000)
	for i := range data {
		data[i] = byte(i % 256)
	}

	n, err := pub.Write(data)
	require.NoError(t, err)
	assert.Equal(t, 5000, n)

	ev := requireEvent(t, sub, "over 4KB trigger")
	assert.Equal(t, events.EventTypeAutomateOutputChunk, ev.Type)
	d := ev.Data.(map[string]interface{})
	assert.Equal(t, "over4kb-session", d["session_id"])
	assert.Equal(t, 0, d["offset"])
	assert.Equal(t, 5000, d["chunk_len"])
}

// ============================================================================
// OutputChunkPublisher — Time Threshold Tests
// ============================================================================

// TestOutputChunkPublisher_TimeThreshold first writes 4KB to trigger an
// initial size-based publish (which sets lastPublish), then sleeps past
// the 250ms coalesce interval, then writes 100 more bytes. The second
// write should trigger a time-based publish of the 100 bytes.
func TestOutputChunkPublisher_TimeThreshold(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	pub := NewOutputChunkPublisher("time-session", eb)

	// First write: 4KB — triggers size-based publish and sets lastPublish
	data1 := make([]byte, 4096)
	for i := range data1 {
		data1[i] = 'a'
	}
	n, err := pub.Write(data1)
	require.NoError(t, err)
	assert.Equal(t, 4096, n)

	// Consume the first event (size-based publish)
	ev1 := requireEvent(t, sub, "initial size-based publish")
	d1 := ev1.Data.(map[string]interface{})
	assert.Equal(t, 0, d1["offset"])
	assert.Equal(t, 4096, d1["chunk_len"])

	// Sleep past the coalesce interval (250ms)
	time.Sleep(300 * time.Millisecond)

	// Second write: 100 bytes — should trigger time-based publish
	data2 := make([]byte, 100)
	for i := range data2 {
		data2[i] = 'b'
	}
	n, err = pub.Write(data2)
	require.NoError(t, err)
	assert.Equal(t, 100, n)

	// Event should appear with the 100 bytes at offset 4096
	ev2 := requireEvent(t, sub, "time threshold trigger")
	d2 := ev2.Data.(map[string]interface{})
	assert.Equal(t, "time-session", d2["session_id"])
	assert.Equal(t, 4096, d2["offset"])
	assert.Equal(t, 100, d2["chunk_len"])
}

// TestOutputChunkPublisher_BelowBothThresholds writes 100 bytes and verifies
// that NO event is published — confirming that the buffering behavior is
// working correctly.
func TestOutputChunkPublisher_BelowBothThresholds(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	pub := NewOutputChunkPublisher("below-session", eb)

	data := make([]byte, 100)
	for i := range data {
		data[i] = 'x'
	}
	n, err := pub.Write(data)
	require.NoError(t, err)
	assert.Equal(t, 100, n)

	// No event should be published within a short window
	assertNoEvent(t, sub, 150*time.Millisecond, "below both thresholds")
}

// ============================================================================
// OutputChunkPublisher — Flush Tests
// ============================================================================

// TestOutputChunkPublisher_Flush writes 200 bytes (below both thresholds)
// then calls Flush() and verifies that an event is published with the
// remaining buffered data.
func TestOutputChunkPublisher_Flush(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	pub := NewOutputChunkPublisher("flush-session", eb)

	data := make([]byte, 200)
	for i := range data {
		data[i] = byte(i % 256)
	}
	n, err := pub.Write(data)
	require.NoError(t, err)
	assert.Equal(t, 200, n)

	// No event yet — below thresholds
	assertNoEvent(t, sub, 50*time.Millisecond, "before flush")

	// Flush should publish the remaining data
	pub.Flush()

	ev := requireEvent(t, sub, "flush trigger")
	assert.Equal(t, events.EventTypeAutomateOutputChunk, ev.Type)
	d := ev.Data.(map[string]interface{})
	assert.Equal(t, "flush-session", d["session_id"])
	assert.Equal(t, 0, d["offset"])
	assert.Equal(t, 200, d["chunk_len"])
}

// TestOutputChunkPublisher_Flush_Empty calls Flush() on a publisher that
// has had no writes. It must not panic and must not publish any event.
func TestOutputChunkPublisher_Flush_Empty(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	pub := NewOutputChunkPublisher("empty-session", eb)

	// This must not panic
	assert.NotPanics(t, func() {
		pub.Flush()
	})

	// No event should be published
	assertNoEvent(t, sub, 100*time.Millisecond, "empty flush")
}

// TestOutputChunkPublisher_Flush_AfterPublish writes 5000 bytes (which
// triggers an automatic publish at offset 0), then writes 100 more bytes
// and flushes, verifying the second event has offset=5000 and chunk_len=100.
func TestOutputChunkPublisher_Flush_AfterPublish(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	pub := NewOutputChunkPublisher("flush-after-session", eb)

	// First write: 5000 bytes — triggers auto-publish at offset 0
	data1 := make([]byte, 5000)
	for i := range data1 {
		data1[i] = 'a'
	}
	_, err := pub.Write(data1)
	require.NoError(t, err)

	ev1 := requireEvent(t, sub, "first 5000 bytes")
	d1 := ev1.Data.(map[string]interface{})
	assert.Equal(t, 0, d1["offset"])
	assert.Equal(t, 5000, d1["chunk_len"])

	// Second write: 100 bytes — should NOT trigger (below thresholds)
	data2 := make([]byte, 100)
	for i := range data2 {
		data2[i] = 'b'
	}
	_, err = pub.Write(data2)
	require.NoError(t, err)

	// Flush should publish the remaining 100 bytes at offset 5000
	pub.Flush()

	ev2 := requireEvent(t, sub, "flush after publish")
	d2 := ev2.Data.(map[string]interface{})
	assert.Equal(t, 5000, d2["offset"])
	assert.Equal(t, 100, d2["chunk_len"])
}

// ============================================================================
// OutputChunkPublisher — Offset Tracking Tests
// ============================================================================

// TestOutputChunkPublisher_OffsetTracking writes multiple batches that each
// exceed the 4KB threshold and verifies that offsets are cumulative
// (0, 5000, 10000).
func TestOutputChunkPublisher_OffsetTracking(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	pub := NewOutputChunkPublisher("offset-session", eb)

	offsets := []int{}

	// Three sequential writes of 5000 bytes each
	for i := 0; i < 3; i++ {
		data := make([]byte, 5000)
		for j := range data {
			data[j] = byte(i % 256)
		}
		_, err := pub.Write(data)
		require.NoError(t, err)

		ev := requireEvent(t, sub, "publish batch")
		d := ev.Data.(map[string]interface{})
		offsets = append(offsets, d["offset"].(int))
	}

	assert.Equal(t, []int{0, 5000, 10000}, offsets)
}

// ============================================================================
// OutputChunkPublisher — Edge Case Tests
// ============================================================================

// TestOutputChunkPublisher_ZeroWrite writes an empty byte slice and verifies
// it returns (0, nil) without publishing any event.
func TestOutputChunkPublisher_ZeroWrite(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	pub := NewOutputChunkPublisher("zero-session", eb)

	n, err := pub.Write([]byte{})
	assert.NoError(t, err)
	assert.Equal(t, 0, n)

	// No event should be published
	assertNoEvent(t, sub, 100*time.Millisecond, "zero write")
}

// TestOutputChunkPublisher_NilWrite writes a nil byte slice and verifies
// it returns (0, nil) without publishing any event.
func TestOutputChunkPublisher_NilWrite(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	pub := NewOutputChunkPublisher("nil-session", eb)

	n, err := pub.Write(nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, n)

	assertNoEvent(t, sub, 100*time.Millisecond, "nil write")
}

// ============================================================================
// OutputChunkPublisher — Event Payload Field Tests
// ============================================================================

// TestOutputChunkPublisher_EventFields verifies that the published event
// payload contains all expected fields with correct types and values.
func TestOutputChunkPublisher_EventFields(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	pub := NewOutputChunkPublisher("my-session-id", eb)

	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 256)
	}
	_, err := pub.Write(data)
	require.NoError(t, err)

	ev := requireEvent(t, sub, "event fields")
	assert.Equal(t, events.EventTypeAutomateOutputChunk, ev.Type)
	assert.NotEmpty(t, ev.ID)
	assert.NotZero(t, ev.Timestamp)

	d := ev.Data.(map[string]interface{})

	// session_id
	assert.Equal(t, "my-session-id", d["session_id"])

	// offset
	assert.Equal(t, 0, d["offset"])

	// chunk_len
	assert.Equal(t, 4096, d["chunk_len"])

	// timestamp is a string in RFC3339 format
	timestamp, ok := d["timestamp"].(string)
	require.True(t, ok, "timestamp should be a string")
	_, err = time.Parse(time.RFC3339, timestamp)
	assert.NoError(t, err, "timestamp should be valid RFC3339: %q", timestamp)
}

// ============================================================================
// OutputChunkPublisher — Concurrent Write Tests
// ============================================================================

// TestOutputChunkPublisher_ConcurrentWrites spawns 10 goroutines each
// writing 1000 bytes and verifies that totalWritten matches, no panic
// occurs, and the test is race-safe. Run with -race to detect data races.
func TestOutputChunkPublisher_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	pub := NewOutputChunkPublisher("concurrent-session", eb)

	const numGoroutines = 10
	const bytesPerGoroutine = 1000
	const totalBytes = numGoroutines * bytesPerGoroutine

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			data := make([]byte, bytesPerGoroutine)
			for j := range data {
				data[j] = byte(j % 256)
			}
			n, err := pub.Write(data)
			if err != nil {
				t.Errorf("goroutine write failed: %v", err)
				return
			}
			if n != bytesPerGoroutine {
				t.Errorf("expected %d bytes written, got %d", bytesPerGoroutine, n)
			}
		}()
	}

	wg.Wait()

	// Drain events from the subscriber channel — there should be at least one
	// since 10 * 1000 = 10000 > 4096 threshold
	evs := drainEvents(t, sub, 1, 2*time.Second)
	require.GreaterOrEqual(t, len(evs), 1)

	// Total bytes across all events should equal totalBytes
	totalChunkLen := 0
	for _, ev := range evs {
		d := ev.Data.(map[string]interface{})
		totalChunkLen += d["chunk_len"].(int)
	}
	// The events may only cover the portion that triggered publishes.
	// Flush to get the remainder.
	pub.Flush()
	evsAfterFlush := drainEvents(t, sub, 1, 2*time.Second)
	for _, ev := range evsAfterFlush {
		d := ev.Data.(map[string]interface{})
		totalChunkLen += d["chunk_len"].(int)
	}

	assert.Equal(t, totalBytes, totalChunkLen, "total chunk_len across all events should equal total bytes written")
}

// ============================================================================
// StartWithOptions — Integration Tests
// ============================================================================

// TestStartWithOptions_WithEventBus_Automate starts a background "automate"
// process with an event bus, verifies that output_chunk events appear on the
// subscriber channel as the process produces output.
func TestStartWithOptions_WithEventBus_Automate(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Command that produces enough output to trigger the 4KB threshold
	cmd := `printf '%0.s=' $(seq 1 5000); echo`

	opts := &StartOptions{
		EventBus: eb,
	}

	sessionID, err := bpm.StartWithOptions(context.Background(), cmd, "", "automate", opts)
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)

	// Wait for the event to appear on the subscriber channel
	ev := requireEvent(t, sub, "automate output_chunk event")
	assert.Equal(t, events.EventTypeAutomateOutputChunk, ev.Type)
	d := ev.Data.(map[string]interface{})
	assert.Equal(t, sessionID, d["session_id"])
	assert.Greater(t, d["chunk_len"].(int), 0)

	// Wait for process to finish
	require.Eventually(t, func() bool {
		_, status, err := bpm.CheckOutput(sessionID)
		return err == nil && status == "exited"
	}, 5*time.Second, 100*time.Millisecond)

	// Drain any remaining events from the Flush that happens after process exit.
	// Use a short timeout — if the process exited, Flush has already run.
	drainEvents(t, sub, 5, 500*time.Millisecond)
}

// TestStartWithOptions_NoEventBus starts a process with nil StartOptions and
// verifies the process starts fine with no publisher created and no events
// published.
func TestStartWithOptions_NoEventBus(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	sessionID, err := bpm.StartWithOptions(context.Background(), "echo hello", "", "automate", nil)
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)

	// Wait for process to finish
	require.Eventually(t, func() bool {
		_, status, err := bpm.CheckOutput(sessionID)
		return err == nil && status == "exited"
	}, 5*time.Second, 100*time.Millisecond)

	// No events should have been published on our channel
	assertNoEvent(t, sub, 300*time.Millisecond, "nil opts")
}

// TestStartWithOptions_ShellKind_IgnoresEventBus starts a process with
// kind="shell" and a non-nil event bus, verifying that no output_chunk
// events are published (shell sessions don't get the chunk publisher).
func TestStartWithOptions_ShellKind_IgnoresEventBus(t *testing.T) {
	t.Parallel()

	eb := events.NewEventBus()
	sub := eb.Subscribe("test")

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Even with a large output command, shell kind should NOT publish events
	cmd := `printf '%0.s=' $(seq 1 5000); echo`

	opts := &StartOptions{
		EventBus: eb,
	}

	sessionID, err := bpm.StartWithOptions(context.Background(), cmd, "", "shell", opts)
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)

	// Wait for process to finish to give plenty of time for any potential events
	require.Eventually(t, func() bool {
		_, status, err := bpm.CheckOutput(sessionID)
		return err == nil && status == "exited"
	}, 5*time.Second, 100*time.Millisecond)

	// No events should have been published — shell kind ignores EventBus
	assertNoEvent(t, sub, 300*time.Millisecond, "shell kind should not publish")
}

// ============================================================================
// Test Helpers
// ============================================================================

// requireEvent reads an event from the subscriber channel with a timeout.
// Fails the test if no event is received within the timeout.
func requireEvent(t *testing.T, ch <-chan events.UIEvent, msg string) events.UIEvent {
	t.Helper()

	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatalf("expected %s event but none received within 2s", msg)
		return events.UIEvent{}
	}
}

// assertNoEvent verifies that no event arrives on the subscriber channel
// within the given timeout.
func assertNoEvent(t *testing.T, ch <-chan events.UIEvent, timeout time.Duration, msg string) {
	t.Helper()

	select {
	case ev := <-ch:
		t.Fatalf("unexpected event received (%s): type=%s", msg, ev.Type)
	case <-time.After(timeout):
		// Good — no event
	}
}

// drainEvents reads up to maxEvents from the subscriber channel with a
// timeout per event.
func drainEvents(t *testing.T, ch <-chan events.UIEvent, maxEvents int, perEventTimeout time.Duration) []events.UIEvent {
	t.Helper()

	var evs []events.UIEvent
	for i := 0; i < maxEvents; i++ {
		select {
		case ev := <-ch:
			evs = append(evs, ev)
		case <-time.After(perEventTimeout):
			return evs
		}
	}
	return evs
}
