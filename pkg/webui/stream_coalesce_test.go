//go:build !js

package webui

import (
	"reflect"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func chunk(text, ct, client, chat string) events.UIEvent {
	return events.UIEvent{Type: events.EventTypeStreamChunk, Data: map[string]interface{}{
		"chunk": text, "content_type": ct, "client_id": client, "chat_id": chat,
	}}
}

func TestCoalesceStreamChunks_MergesAdjacentSameRoute(t *testing.T) {
	in := []events.UIEvent{
		chunk("Hel", "assistant_text", "c1", "ch1"),
		chunk("lo ", "assistant_text", "c1", "ch1"),
		chunk("world", "assistant_text", "c1", "ch1"),
	}
	out := coalesceStreamChunks(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 merged event, got %d", len(out))
	}
	if got := streamField(out[0], "chunk"); got != "Hello world" {
		t.Errorf("merged chunk = %q, want %q", got, "Hello world")
	}
	// Must not mutate the input events' shared maps.
	if got := streamField(in[0], "chunk"); got != "Hel" {
		t.Errorf("input event 0 was mutated: chunk = %q", got)
	}
}

func TestCoalesceStreamChunks_PreservesBoundaries(t *testing.T) {
	other := events.UIEvent{Type: events.EventTypeAgentMessage, Data: map[string]interface{}{"message": "tool"}}
	in := []events.UIEvent{
		chunk("a", "assistant_text", "c1", "ch1"),
		chunk("b", "reasoning", "c1", "ch1"),      // different content_type → separate
		chunk("c", "assistant_text", "c2", "ch1"), // different client → separate
		other, // non-stream → separate, in order
		chunk("d", "assistant_text", "c1", "ch1"),
		chunk("e", "assistant_text", "c1", "ch1"), // merges with d
	}
	out := coalesceStreamChunks(in)
	if len(out) != 5 {
		t.Fatalf("expected 5 events, got %d", len(out))
	}
	if out[3].Type != events.EventTypeAgentMessage {
		t.Errorf("order not preserved: out[3] = %s", out[3].Type)
	}
	if got := streamField(out[4], "chunk"); got != "de" {
		t.Errorf("last merged chunk = %q, want %q", got, "de")
	}
}

// milestone returns a flat progress_milestone event for scope, all sharing a
// single run so a coalesced batch's top-level run_id is well-defined.
func milestone(scope string) events.UIEvent {
	return events.UIEvent{
		Type: events.EventTypeProgressMilestone,
		Data: map[string]interface{}{
			"run_id":        "run-1",
			"plan_revision": 3,
			"scope_id":      scope,
			"phase":         "finished",
			"elapsed_ms":    42000,
		},
	}
}

// progressEvent returns a non-milestone progress event of type typ carrying a
// marker, used to verify that question/verification/complete are never
// coalesced and break milestone runs.
func progressEvent(typ, marker string) events.UIEvent {
	return events.UIEvent{
		Type: typ,
		Data: map[string]interface{}{"run_id": "run-1", "marker": marker},
	}
}

// batchMilestones returns the scope_ids carried by a batched milestone
// event's "milestones" array.
func batchMilestones(t *testing.T, ev events.UIEvent) []map[string]interface{} {
	t.Helper()
	data, ok := ev.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("event Data is not a map: %T", ev.Data)
	}
	ms, ok := data["milestones"].([]interface{})
	if !ok {
		t.Fatalf("event has no milestones array: %v", data)
	}
	out := make([]map[string]interface{}, 0, len(ms))
	for i, m := range ms {
		plat, ok := m.(map[string]interface{})
		if !ok {
			t.Fatalf("milestones[%d] is not a map: %T", i, m)
		}
		out = append(out, plat)
	}
	return out
}

func scopeIDs(t *testing.T, ev events.UIEvent) []string {
	t.Helper()
	ids := make([]string, 0)
	for _, plat := range batchMilestones(t, ev) {
		ids = append(ids, plat["scope_id"].(string))
	}
	return ids
}

func TestCoalesceProgressMilestones_MergesAdjacent(t *testing.T) {
	in := []events.UIEvent{milestone("a"), milestone("b"), milestone("c")}
	out := coalesceProgressMilestones(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 batched event, got %d", len(out))
	}
	if out[0].Type != events.EventTypeProgressMilestone {
		t.Fatalf("batched event type = %s, want %s", out[0].Type, events.EventTypeProgressMilestone)
	}
	data, _ := out[0].Data.(map[string]interface{})
	if got := data["run_id"]; got != "run-1" {
		t.Errorf("batch run_id = %v, want run-1 (the first milestone's)", got)
	}
	ids := scopeIDs(t, out[0])
	if len(ids) != 3 || ids[0] != "a" || ids[1] != "b" || ids[2] != "c" {
		t.Errorf("milestones = %v, want [a b c]", ids)
	}
}

func TestCoalesceProgressMilestones_SingleStaysFlat(t *testing.T) {
	m := milestone("a")
	out := coalesceProgressMilestones([]events.UIEvent{m})
	if len(out) != 1 {
		t.Fatalf("expected 1 event, got %d", len(out))
	}
	if isMilestoneBatch(out[0].Data) {
		t.Fatalf("single milestone must stay flat, got a batch")
	}
	data, _ := out[0].Data.(map[string]interface{})
	if _, has := data["milestones"]; has {
		t.Errorf("single milestone must have no milestones key, got %v", data["milestones"])
	}
	if got := data["scope_id"]; got != "a" {
		t.Errorf("scope_id = %v, want a", got)
	}
}

func TestCoalesceProgressMilestones_SeparatedNotMerged(t *testing.T) {
	// A question between two milestones breaks the run: both stay flat.
	in := []events.UIEvent{milestone("a"), progressEvent(events.EventTypeProgressQuestion, "q"), milestone("b")}
	out := coalesceProgressMilestones(in)
	if len(out) != 3 {
		t.Fatalf("expected 3 events, got %d", len(out))
	}
	if out[1].Type != events.EventTypeProgressQuestion {
		t.Errorf("order not preserved: out[1] = %s", out[1].Type)
	}
	if isMilestoneBatch(out[0].Data) || isMilestoneBatch(out[2].Data) {
		t.Errorf("separated milestones must stay flat: out[0] batch=%v out[2] batch=%v",
			isMilestoneBatch(out[0].Data), isMilestoneBatch(out[2].Data))
	}
}

func TestCoalesceProgressMilestones_NonMilestonesNeverCoalesced(t *testing.T) {
	// Runs of question / verification / complete pass through verbatim,
	// preserving count, types, and order, and never gain a milestones key.
	cases := []struct {
		name string
		in   []events.UIEvent
	}{
		{"question", []events.UIEvent{
			progressEvent(events.EventTypeProgressQuestion, "q1"),
			progressEvent(events.EventTypeProgressQuestion, "q2"),
		}},
		{"verification", []events.UIEvent{
			progressEvent(events.EventTypeProgressVerification, "v1"),
			progressEvent(events.EventTypeProgressVerification, "v2"),
		}},
		{"complete", []events.UIEvent{
			progressEvent(events.EventTypeProgressComplete, "c1"),
			progressEvent(events.EventTypeProgressComplete, "c2"),
		}},
	}
	for _, tc := range cases {
		out := coalesceProgressMilestones(tc.in)
		if len(out) != len(tc.in) {
			t.Errorf("%s run: expected %d events, got %d", tc.name, len(tc.in), len(out))
		}
		for i, ev := range out {
			if isMilestoneBatch(ev.Data) {
				t.Errorf("%s run event %d gained a milestones key", tc.name, i)
			}
		}
	}

	// Mixed: two milestones separated by the three other progress types stay
	// separate and flat, in order.
	in := []events.UIEvent{
		milestone("a"),
		progressEvent(events.EventTypeProgressQuestion, "q"),
		progressEvent(events.EventTypeProgressVerification, "v"),
		progressEvent(events.EventTypeProgressComplete, "c"),
		milestone("b"),
	}
	out := coalesceProgressMilestones(in)
	if len(out) != 5 {
		t.Fatalf("mixed: expected 5 events, got %d", len(out))
	}
	want := []string{
		events.EventTypeProgressMilestone,
		events.EventTypeProgressQuestion,
		events.EventTypeProgressVerification,
		events.EventTypeProgressComplete,
		events.EventTypeProgressMilestone,
	}
	for i := range want {
		if out[i].Type != want[i] {
			t.Errorf("mixed order[%d] = %s, want %s", i, out[i].Type, want[i])
		}
	}
	if isMilestoneBatch(out[0].Data) || isMilestoneBatch(out[4].Data) {
		t.Errorf("separated milestones must stay flat")
	}
}

func TestCoalesceProgressMilestones_MixedBatch(t *testing.T) {
	in := []events.UIEvent{
		milestone("m1"),
		milestone("m2"),
		progressEvent(events.EventTypeProgressQuestion, "q"),
		milestone("m3"),
		progressEvent(events.EventTypeProgressVerification, "v"),
		milestone("m4"),
		milestone("m5"),
		progressEvent(events.EventTypeProgressComplete, "c"),
	}
	out := coalesceProgressMilestones(in)
	// [batch(m1,m2), question, m3(flat), verification, batch(m4,m5), complete]
	if len(out) != 6 {
		t.Fatalf("expected 6 events, got %d", len(out))
	}
	wantTypes := []string{
		events.EventTypeProgressMilestone, // batch(m1,m2)
		events.EventTypeProgressQuestion,
		events.EventTypeProgressMilestone, // m3 flat
		events.EventTypeProgressVerification,
		events.EventTypeProgressMilestone, // batch(m4,m5)
		events.EventTypeProgressComplete,
	}
	for i := range wantTypes {
		if out[i].Type != wantTypes[i] {
			t.Errorf("type[%d] = %s, want %s", i, out[i].Type, wantTypes[i])
		}
	}

	if ids := scopeIDs(t, out[0]); len(ids) != 2 || ids[0] != "m1" || ids[1] != "m2" {
		t.Errorf("first batch scopes = %v, want [m1 m2]", ids)
	}
	if isMilestoneBatch(out[2].Data) {
		t.Errorf("out[2] (m3) must be flat, got a batch")
	}
	if got := out[2].Data.(map[string]interface{})["scope_id"]; got != "m3" {
		t.Errorf("out[2].scope_id = %v, want m3", got)
	}
	if ids := scopeIDs(t, out[4]); len(ids) != 2 || ids[0] != "m4" || ids[1] != "m5" {
		t.Errorf("second batch scopes = %v, want [m4 m5]", ids)
	}
}

// mapPtr returns the underlying pointer of a map value so two map values can
// be compared for identity (Go forbids == on maps). Two maps made by make
// have distinct pointers; the same map shared by reference has one. A
// non-map argument yields 0.
func mapPtr(m interface{}) uintptr {
	if mp, ok := m.(map[string]interface{}); ok {
		return reflect.ValueOf(mp).Pointer()
	}
	return 0
}

func TestCoalesceProgressMilestones_FreshMaps(t *testing.T) {
	m1 := milestone("a")
	m2 := milestone("b")
	in := []events.UIEvent{m1, m2}
	out := coalesceProgressMilestones(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 batched event, got %d", len(out))
	}
	batchData, _ := out[0].Data.(map[string]interface{})
	if mapPtr(batchData) == mapPtr(m1.Data) {
		t.Errorf("batch Data map is the same pointer as input m1.Data")
	}
	if mapPtr(batchData) == mapPtr(m2.Data) {
		t.Errorf("batch Data map is the same pointer as input m2.Data")
	}
	ms := batchMilestones(t, out[0])
	if len(ms) != 2 {
		t.Fatalf("expected 2 milestones, got %d", len(ms))
	}
	if mapPtr(ms[0]) == mapPtr(m1.Data) {
		t.Errorf("milestones[0] is the same pointer as input m1.Data")
	}
	if mapPtr(ms[1]) == mapPtr(m2.Data) {
		t.Errorf("milestones[1] is the same pointer as input m2.Data")
	}
	// Mutating an input map after coalescing must not leak into the batch.
	if m, ok := m1.Data.(map[string]interface{}); ok {
		m["scope_id"] = "MUTATED"
	}
	if got := ms[0]["scope_id"]; got != "a" {
		t.Errorf("batch milestone[0] was mutated by input mutation: scope_id = %v", got)
	}
}
