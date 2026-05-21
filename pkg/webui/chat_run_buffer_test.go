//go:build !js

package webui

import (
	"strings"
	"sync"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestChatRunRingBuffer_AppendAssignsMonotonicSeq(t *testing.T) {
	b := newChatRunRingBufferWithCaps(100, 1024*1024)

	got := []int64{
		b.Append(events.UIEvent{Type: "stream_chunk", Data: "a"}),
		b.Append(events.UIEvent{Type: "stream_chunk", Data: "b"}),
		b.Append(events.UIEvent{Type: "stream_chunk", Data: "c"}),
	}
	want := []int64{1, 2, 3}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Append #%d seq = %d, want %d", i, got[i], want[i])
		}
	}
	if got := b.LastSeq(); got != 3 {
		t.Errorf("LastSeq = %d, want 3", got)
	}
}

func TestChatRunRingBuffer_AfterReturnsTailInOrder(t *testing.T) {
	b := newChatRunRingBufferWithCaps(100, 1024*1024)
	for _, payload := range []string{"a", "b", "c", "d"} {
		b.Append(events.UIEvent{Type: "stream_chunk", Data: payload})
	}

	replay, gap := b.After(2)
	if gap {
		t.Error("After(2) reported gap, but seq 2 is still in buffer")
	}
	if len(replay) != 2 {
		t.Fatalf("After(2) returned %d events, want 2", len(replay))
	}
	if replay[0].Data != "c" || replay[1].Data != "d" {
		t.Errorf("After(2) returned wrong tail: %+v", replay)
	}

	// After(LastSeq) returns nothing — caller is already up to date.
	if replay, _ := b.After(b.LastSeq()); replay != nil {
		t.Errorf("After(LastSeq) should be empty, got %d events", len(replay))
	}
}

func TestChatRunRingBuffer_AfterReportsGapAfterEviction(t *testing.T) {
	// Cap at 3 events to force eviction.
	b := newChatRunRingBufferWithCaps(3, 1024*1024)
	for _, payload := range []string{"a", "b", "c", "d", "e"} {
		b.Append(events.UIEvent{Type: "stream_chunk", Data: payload})
	}
	// Buffer now holds seq 3,4,5.

	// Caller is asking for events after seq 1 — but we evicted seq 2 and
	// 3 along with 1. We DID retain seq 3-5, so the question is whether
	// "after 1" can be confidently answered from seq 3+ onwards.
	//
	// The expected contract: oldest seq is 3; afterSeq = 1 is strictly
	// before oldest-1 (= 2), so we flag a gap. If the caller asked for
	// after seq 2 or 3, we'd return seq 3+ (or 4+) with no gap.
	replay, gap := b.After(1)
	if !gap {
		t.Errorf("After(1) on buffer holding seq 3..5 should report gap, replay=%+v", replay)
	}

	replay, gap = b.After(3)
	if gap {
		t.Error("After(3) should not report gap (3 is still in buffer)")
	}
	if len(replay) != 2 {
		t.Errorf("After(3) returned %d events, want 2 (seq 4 and 5)", len(replay))
	}
}

func TestChatRunRingBuffer_EvictsByCountCap(t *testing.T) {
	b := newChatRunRingBufferWithCaps(3, 1024*1024)
	for i := 0; i < 10; i++ {
		b.Append(events.UIEvent{Type: "stream_chunk", Data: "x"})
	}
	if got := b.Len(); got != 3 {
		t.Errorf("Len after 10 Appends with maxEvents=3: %d, want 3", got)
	}
	if got := b.LastSeq(); got != 10 {
		t.Errorf("LastSeq after 10 Appends: %d, want 10", got)
	}
}

func TestChatRunRingBuffer_EvictsByByteCap(t *testing.T) {
	// Very small byte cap to force size-based eviction.
	b := newChatRunRingBufferWithCaps(1000, 500)

	// Each event is ~150 bytes — chunk overhead + 50 chars of content.
	big := strings.Repeat("x", 50)
	for i := 0; i < 20; i++ {
		b.Append(events.UIEvent{Type: "stream_chunk", Data: big})
	}

	if b.Bytes() > 500 {
		// We always keep at least 1 event even if a single event exceeds
		// the cap (the "len > 1" guard in Append). For 50-char strings
		// that should never happen, but assert generously.
		if b.Len() != 1 {
			t.Errorf("Bytes %d > maxBytes 500 and Len %d != 1 — eviction broken", b.Bytes(), b.Len())
		}
	}
	if b.Len() == 0 {
		t.Error("byte-cap eviction emptied the buffer entirely — should always keep at least one event")
	}
}

func TestChatRunRingBuffer_Reset(t *testing.T) {
	b := newChatRunRingBufferWithCaps(100, 1024*1024)
	for i := 0; i < 5; i++ {
		b.Append(events.UIEvent{Type: "stream_chunk", Data: "x"})
	}
	preSeq := b.LastSeq()
	b.Reset()

	if got := b.Len(); got != 0 {
		t.Errorf("Len after Reset = %d, want 0", got)
	}
	if got := b.Bytes(); got != 0 {
		t.Errorf("Bytes after Reset = %d, want 0", got)
	}
	if got := b.LastSeq(); got != preSeq {
		t.Errorf("LastSeq after Reset = %d, want %d (seq should not roll back)", got, preSeq)
	}
	// New appends continue from the prior seq.
	if next := b.Append(events.UIEvent{Type: "stream_chunk", Data: "y"}); next != preSeq+1 {
		t.Errorf("Append after Reset returned seq %d, want %d", next, preSeq+1)
	}
}

func TestChatRunRingBuffer_ConcurrentAppendAndRead(t *testing.T) {
	b := newChatRunRingBufferWithCaps(1000, 4*1024*1024)

	var wg sync.WaitGroup
	const writers = 8
	const perWriter = 500

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				b.Append(events.UIEvent{Type: "stream_chunk", Data: "x"})
			}
		}()
	}
	// Concurrent readers.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_ = b.LastSeq()
				_, _ = b.After(0)
			}
		}()
	}
	wg.Wait()

	if got := b.LastSeq(); got != int64(writers*perWriter) {
		t.Errorf("LastSeq after %d concurrent Appends = %d, want %d", writers*perWriter, got, writers*perWriter)
	}
}

func TestNewChatRunRingBuffer_DefaultCaps(t *testing.T) {
	b := newChatRunRingBuffer()
	if b.maxEvents != defaultRunBufferMaxEvents {
		t.Errorf("default maxEvents = %d, want %d", b.maxEvents, defaultRunBufferMaxEvents)
	}
	if b.maxBytes != defaultRunBufferMaxBytes {
		t.Errorf("default maxBytes = %d, want %d", b.maxBytes, defaultRunBufferMaxBytes)
	}
}

func TestNewChatRunRingBufferWithCaps_FallsBackForBadInput(t *testing.T) {
	b := newChatRunRingBufferWithCaps(0, -1)
	if b.maxEvents != defaultRunBufferMaxEvents {
		t.Errorf("0 maxEvents should fall back to default, got %d", b.maxEvents)
	}
	if b.maxBytes != defaultRunBufferMaxBytes {
		t.Errorf("-1 maxBytes should fall back to default, got %d", b.maxBytes)
	}
}
