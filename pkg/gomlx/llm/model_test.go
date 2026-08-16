//go:build arm64 && cgo && (darwin || (linux && ggml))

package llm

import "testing"

// TestBestPrefixSlot_PicksLongestFullMatch guards the multi-slot prefix
// cache's core safety property: a slot only qualifies if its ENTIRE token
// sequence is a prefix of the new prompt (not just a longest-common-prefix
// substring). Partial overlaps must never be picked, since restoring a
// DeltaNet slot's recurrent state from the wrong position corrupts the
// linear-attention layers — see bestPrefixSlot's doc comment.
func TestBestPrefixSlot_PicksLongestFullMatch(t *testing.T) {
	m := &Model{
		prefixSlots: []*prefixSlot{
			{tokens: []int{1, 2, 3}},          // conversation A, short match
			{tokens: []int{1, 2, 3, 4, 5}},    // conversation B, full continuation
			{tokens: []int{1, 2, 9}},          // conversation C, diverges — must not match
			{tokens: []int{1, 2, 3, 4, 5, 6}}, // longer than the new prompt — can't match
		},
	}

	shared, idx := m.bestPrefixSlot([]int{1, 2, 3, 4, 5, 7, 8})

	if idx != 1 {
		t.Fatalf("expected slot 1 (longest full-prefix match), got idx=%d", idx)
	}
	if shared != 5 {
		t.Fatalf("expected shared=5, got %d", shared)
	}
}

func TestBestPrefixSlot_NoMatch(t *testing.T) {
	m := &Model{
		prefixSlots: []*prefixSlot{
			{tokens: []int{9, 9, 9}},
		},
	}
	shared, idx := m.bestPrefixSlot([]int{1, 2, 3})
	if idx != -1 || shared != 0 {
		t.Fatalf("expected no match (idx=-1, shared=0), got idx=%d shared=%d", idx, shared)
	}
}

// TestStorePrefixSlot_EvictsLeastRecentlyUsed guards the concurrent-conversation
// case (parallel subagents sharing this Model, see subagent_runners.go
// RunParallel): once at capacity, adding a new conversation's slot must
// evict the least-recently-touched one, not an arbitrary or most-recently-used
// one — otherwise an active conversation loses its cache mid-session.
func TestStorePrefixSlot_EvictsLeastRecentlyUsed(t *testing.T) {
	const slotCap = maxPrefixSlotsCap
	m := &Model{prefixSlotsCapOverride: slotCap}
	for i := range slotCap {
		m.storePrefixSlot(-1, []int{i}, &KVCache{})
	}
	if len(m.prefixSlots) != slotCap {
		t.Fatalf("expected %d slots, got %d", slotCap, len(m.prefixSlots))
	}

	// Touch every slot except the first (tokens=[0]) by updating it in
	// place via a matched index, making it the new least-recently-used.
	for i := 1; i < slotCap; i++ {
		m.storePrefixSlot(i, []int{i, 100}, &KVCache{})
	}

	// Adding one more conversation should evict slot 0 (tokens=[0]),
	// the only one never re-touched.
	m.storePrefixSlot(-1, []int{999}, &KVCache{})

	if len(m.prefixSlots) != slotCap {
		t.Fatalf("expected slot count to stay at cap %d, got %d", slotCap, len(m.prefixSlots))
	}
	for _, slot := range m.prefixSlots {
		if len(slot.tokens) == 1 && slot.tokens[0] == 0 {
			t.Fatalf("least-recently-used slot (tokens=[0]) should have been evicted, slots=%v", dumpSlots(m.prefixSlots))
		}
	}
}

// TestStoreIndexFor_ProtectedSlotNotOverwritten guards the fix for a bug
// where Generate would overwrite a WarmSystemPrefix-created slot in place
// the moment any conversation's first turn matched it, replacing the short
// reusable system prefix with that one turn's full prompt — silently
// destroying the warm cache for every other conversation and even this same
// one's next turn, forcing a full system-prompt re-prefill (tens of
// seconds) on every single turn instead of once per session.
func TestStoreIndexFor_ProtectedSlotNotOverwritten(t *testing.T) {
	m := &Model{
		prefixSeq: 5,
		prefixSlots: []*prefixSlot{
			{tokens: []int{1, 2, 3}, protected: true, lastUsed: 1},
		},
	}

	got := m.storeIndexFor(0)

	if got != -1 {
		t.Fatalf("expected -1 (append new slot) for a protected match, got %d", got)
	}
	if m.prefixSlots[0].lastUsed <= 1 {
		t.Fatalf("expected protected slot's lastUsed to be bumped so LRU doesn't treat it as idle, got %d", m.prefixSlots[0].lastUsed)
	}
}

// TestStoreIndexFor_UnprotectedSlotUpdatedInPlace guards the normal case:
// a conversation's own prior-turn slot (not a warm base) should still be
// updated in place, as intended by storePrefixSlot's matchedIdx design.
func TestStoreIndexFor_UnprotectedSlotUpdatedInPlace(t *testing.T) {
	m := &Model{
		prefixSlots: []*prefixSlot{
			{tokens: []int{1, 2, 3}, protected: false},
		},
	}

	got := m.storeIndexFor(0)

	if got != 0 {
		t.Fatalf("expected 0 (update in place) for a non-protected match, got %d", got)
	}
}

// TestStoreIndexFor_NoMatch guards the no-match (-1) passthrough case.
func TestStoreIndexFor_NoMatch(t *testing.T) {
	m := &Model{prefixSlots: []*prefixSlot{{tokens: []int{1, 2, 3}}}}

	if got := m.storeIndexFor(-1); got != -1 {
		t.Fatalf("expected -1 passthrough for no match, got %d", got)
	}
}

func dumpSlots(slots []*prefixSlot) [][]int {
	out := make([][]int, len(slots))
	for i, s := range slots {
		out[i] = s.tokens
	}
	return out
}
