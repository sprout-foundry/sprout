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

func dumpSlots(slots []*prefixSlot) [][]int {
	out := make([][]int, len(slots))
	for i, s := range slots {
		out[i] = s.tokens
	}
	return out
}
