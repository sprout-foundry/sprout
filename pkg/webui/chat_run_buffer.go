//go:build !js

package webui

import (
	"sync"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// Default caps for the per-chat stream replay buffer. These bound memory
// growth across long-running multi-million-token chats. Both ceilings
// apply: whichever fills first triggers eviction.
const (
	defaultRunBufferMaxEvents = 5000
	defaultRunBufferMaxBytes  = 4 * 1024 * 1024 // 4 MiB per chat
)

// chatRunRingBuffer holds the last N stream events for a chat so a
// reconnecting client can replay anything it missed during the
// disconnect.
//
// SP-034 Phase 2 (reattach): when a browser tab loses its WebSocket
// during an active query (network blip, sleep/wake, etc.), reconnecting
// with `?reattach=<chat-id>&after_seq=<n>` should yield the events with
// seq > n in order, then transparently resume the live stream. The ring
// buffer is the in-memory store backing that.
//
// Sequence numbers are monotonically increasing within a chat. Callers
// pass through their own seq if they want consistent IDs across the
// publish + replay path; otherwise Append assigns the next available.
//
// All methods are safe for concurrent use.
type chatRunRingBuffer struct {
	mu        sync.Mutex
	events    []chatRunBufferedEvent
	maxEvents int
	maxBytes  int
	curBytes  int
	nextSeq   int64
}

// chatRunBufferedEvent wraps a UIEvent with its monotonic seq + the
// approximate byte cost we charged against maxBytes. Storing the cost
// alongside the event avoids recomputing JSON size on eviction.
type chatRunBufferedEvent struct {
	Seq   int64
	Event events.UIEvent
	cost  int
}

// newChatRunRingBuffer constructs a buffer with the default caps.
func newChatRunRingBuffer() *chatRunRingBuffer {
	return &chatRunRingBuffer{
		maxEvents: defaultRunBufferMaxEvents,
		maxBytes:  defaultRunBufferMaxBytes,
	}
}

// newChatRunRingBufferWithCaps lets tests construct small buffers without
// reaching into unexported fields. Negative or zero caps fall back to the
// defaults so a misconfigured caller doesn't get an unbounded buffer.
func newChatRunRingBufferWithCaps(maxEvents, maxBytes int) *chatRunRingBuffer {
	b := &chatRunRingBuffer{maxEvents: maxEvents, maxBytes: maxBytes}
	if b.maxEvents <= 0 {
		b.maxEvents = defaultRunBufferMaxEvents
	}
	if b.maxBytes <= 0 {
		b.maxBytes = defaultRunBufferMaxBytes
	}
	return b
}

// Append records an event and returns its assigned seq. The caller
// usually publishes the same event to live subscribers — those
// subscribers see the event with its real seq via the UIEvent.ID, so
// reattach replay and live stream are consistent.
func (b *chatRunRingBuffer) Append(ev events.UIEvent) int64 {
	cost := estimateEventCost(ev)

	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextSeq++
	seq := b.nextSeq
	b.events = append(b.events, chatRunBufferedEvent{
		Seq:   seq,
		Event: ev,
		cost:  cost,
	})
	b.curBytes += cost

	// Evict oldest entries until both caps are satisfied. We trim by
	// count first since that's cheaper; the byte cap is checked after.
	for len(b.events) > b.maxEvents || (b.curBytes > b.maxBytes && len(b.events) > 1) {
		b.curBytes -= b.events[0].cost
		b.events = b.events[1:]
	}
	return seq
}

// After returns every buffered event with Seq > afterSeq, in order.
// Returns nil when there's nothing to replay (either the buffer is
// empty, or afterSeq is already at/past the latest seq).
//
// If afterSeq is older than the oldest retained event (we evicted it),
// the second return is true — the caller must treat the replay as
// incomplete and reset its state rather than splicing partial chunks
// onto a stale view.
func (b *chatRunRingBuffer) After(afterSeq int64) (replay []events.UIEvent, gap bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.events) == 0 {
		return nil, false
	}

	oldest := b.events[0].Seq
	if afterSeq < oldest-1 {
		// The caller wants events older than what we have. Signal a gap
		// so they know to do a hard refresh.
		gap = true
	}

	for _, e := range b.events {
		if e.Seq > afterSeq {
			replay = append(replay, e.Event)
		}
	}
	return replay, gap
}

// LastSeq returns the seq of the most recently appended event, or 0 if
// the buffer is empty. Used by the WS handshake to tell the client where
// the live stream is so it can ask for the right after_seq on reconnect.
func (b *chatRunRingBuffer) LastSeq() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.nextSeq
}

// Len reports the current number of events in the buffer. Test/diagnostic
// helper.
func (b *chatRunRingBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}

// Bytes reports the current byte cost. Test/diagnostic helper.
func (b *chatRunRingBuffer) Bytes() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.curBytes
}

// Reset clears the buffer (e.g. on run completion + TTL, per SP-034-2f).
// Sequence numbers continue from where they were so subscribers don't
// see seq go backwards if a new run starts.
func (b *chatRunRingBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = nil
	b.curBytes = 0
}

// estimateEventCost approximates the byte size of a UIEvent for the
// maxBytes cap. Doing the full JSON encode would double-cost CPU on
// every publish, so we estimate based on the data payload's likely
// serialization. Conservatively rounds up.
func estimateEventCost(ev events.UIEvent) int {
	// Fixed-shape overhead: id/type/timestamp + JSON braces/commas.
	cost := 100
	cost += len(ev.ID) + len(ev.Type)
	cost += estimateAnyCost(ev.Data)
	return cost
}

// estimateAnyCost handles the common shapes the agent emits: strings,
// maps with string values, slices of those. Anything more exotic falls
// back to a flat estimate; the maxBytes cap is a ceiling, not a
// precise meter.
func estimateAnyCost(v any) int {
	switch x := v.(type) {
	case nil:
		return 4
	case string:
		return len(x) + 2
	case map[string]any:
		total := 2
		for k, vv := range x {
			total += len(k) + 4 + estimateAnyCost(vv)
		}
		return total
	case []any:
		total := 2
		for _, vv := range x {
			total += estimateAnyCost(vv) + 1
		}
		return total
	case []string:
		total := 2
		for _, s := range x {
			total += len(s) + 3
		}
		return total
	default:
		// Numbers/bools/etc. — generous flat estimate.
		return 32
	}
}
