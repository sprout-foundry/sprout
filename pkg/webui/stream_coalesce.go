//go:build !js

package webui

import (
	"github.com/sprout-foundry/sprout/pkg/events"
)

// maxCoalesceDrain bounds how many already-queued events the websocket writer
// pulls in one opportunistic, non-blocking drain before flushing. It only ever
// batches events that are ALREADY waiting in the channel, so it adds no latency
// when events trickle in one at a time — it only kicks in under a backlog,
// which is exactly when stream chunks would otherwise be dropped.
const maxCoalesceDrain = 256

// coalesceStreamChunks merges runs of adjacent stream_chunk events that share
// the same content_type and routing (client_id/chat_id) into a single event,
// concatenating their chunk text. A fast char/token-level stream produces
// hundreds of tiny events per second; collapsing a backlog of them into a few
// larger writes lets the per-subscriber channel drain far faster, so it stops
// silently dropping chunks under backpressure (a backgrounded/laggy tab, a
// burst, a slow link). The browser's stream handler appends chunk text, so a
// merged chunk renders identically to the sum of its parts.
//
// Non-stream events, and stream chunks with different content_type/routing, are
// passed through untouched and in order. A merged event always gets a FRESH
// Data map: the input events' maps are shared with other subscribers and the
// replay ring buffer, so they must never be mutated in place.
func coalesceStreamChunks(in []events.UIEvent) []events.UIEvent {
	if len(in) < 2 {
		return in
	}
	out := make([]events.UIEvent, 0, len(in))
	for _, ev := range in {
		if ev.Type == events.EventTypeStreamChunk && len(out) > 0 {
			last := out[len(out)-1]
			if last.Type == events.EventTypeStreamChunk && sameStreamRoute(last, ev) {
				out[len(out)-1] = mergeStreamChunks(last, ev)
				continue
			}
		}
		out = append(out, ev)
	}
	return out
}

func streamField(ev events.UIEvent, key string) string {
	if m, ok := ev.Data.(map[string]interface{}); ok {
		if v, ok := m[key].(string); ok {
			return v
		}
	}
	return ""
}

// sameStreamRoute reports whether two stream chunks can be merged: same content
// kind (assistant_text vs reasoning) and same destination tab. Merging across
// routes would mis-deliver text.
func sameStreamRoute(a, b events.UIEvent) bool {
	return streamField(a, "content_type") == streamField(b, "content_type") &&
		streamField(a, "client_id") == streamField(b, "client_id") &&
		streamField(a, "chat_id") == streamField(b, "chat_id")
}

// mergeStreamChunks returns a new event whose chunk is a+b, with a fresh Data
// map (never mutating the shared input maps) and the later event's timestamp.
func mergeStreamChunks(a, b events.UIEvent) events.UIEvent {
	data := make(map[string]interface{})
	if am, ok := a.Data.(map[string]interface{}); ok {
		for k, v := range am {
			data[k] = v
		}
	}
	data["chunk"] = streamField(a, "chunk") + streamField(b, "chunk")

	merged := a
	merged.Data = data
	merged.Timestamp = b.Timestamp
	return merged
}

// coalesceProgressMilestones collapses runs of ADJACENT progress_milestone
// events that share the same route (client_id/chat_id/user_id) into a single
// batched event whose Data is
// {"run_id": <first milestone's run_id>, "milestones": [ <m1 payload>, <m2
// payload>, ... ], "client_id"/"chat_id"/"user_id": <route keys>} so consumers
// of a long run read progress at a human rate. A single
// (non-adjacent) milestone keeps its flat payload with NO "milestones" key.
//
// Only milestones on the SAME route are merged: two adjacent milestones with
// different client_id/chat_id/user_id each stay separate, because merging
// across routes would mis-deliver the batch. question, verification,
// completion and every other event type are NEVER coalesced: they pass through
// untouched and in order, and any one of them breaks a milestone run. Like
// coalesceStreamChunks, a batched event always gets a FRESH Data map and FRESH
// per-milestone payload maps — the input events' maps are shared with other
// subscribers and the replay ring, so they must never be mutated in place. The
// route keys are copied onto the batch so it still reaches its destination
// through the per-connection forwarder, which filters on them.
func coalesceProgressMilestones(in []events.UIEvent) []events.UIEvent {
	if len(in) < 2 {
		return in
	}
	out := make([]events.UIEvent, 0, len(in))
	for _, ev := range in {
		if ev.Type == events.EventTypeProgressMilestone && len(out) > 0 {
			last := out[len(out)-1]
			if last.Type == events.EventTypeProgressMilestone && sameMilestoneRoute(last, ev) {
				out[len(out)-1] = mergeMilestones(last, ev)
				continue
			}
		}
		out = append(out, ev)
	}
	return out
}

// milestoneRouteKeys are the routing keys a progress_milestone event carries so
// the websocket forwarder can filter it per connection. They are the same keys
// sameStreamRoute compares, plus user_id.
var milestoneRouteKeys = []string{"client_id", "chat_id", "user_id"}

// sameMilestoneRoute reports whether two milestones can be merged: they must
// share every route key (client_id/chat_id/user_id), with an empty value on one
// side matching an empty value on the other. Merging across routes would
// mis-deliver the batched event.
func sameMilestoneRoute(a, b events.UIEvent) bool {
	for _, key := range milestoneRouteKeys {
		if streamField(a, key) != streamField(b, key) {
			return false
		}
	}
	return true
}

// mergeMilestones returns a new batched milestone event combining last and
// cur. If last is already a batch, cur's payload is appended to a FRESH copy
// of its "milestones" slice; otherwise the batch starts as [last, cur]. The
// batch keeps last's identity (ID/type) but takes the later (cur) timestamp,
// mirroring mergeStreamChunks. The route keys (client_id/chat_id/user_id) are
// copied onto the batch Data from last — the same-route gate guarantees every
// milestone in the run shares them, so reading them from last is correct in
// both the fresh-batch and extend-existing-batch cases. Only keys that are
// present and non-empty are copied, matching how flat payloads carry them.
// Fresh maps throughout — the inputs' maps are never mutated.
func mergeMilestones(last, cur events.UIEvent) events.UIEvent {
	milestones := make([]interface{}, 0, 3)
	if existing, ok := milestoneSlice(last.Data); ok {
		for _, m := range existing {
			milestones = append(milestones, copyMilestonePayload(m))
		}
	} else {
		milestones = append(milestones, milestonePayload(last))
	}
	milestones = append(milestones, milestonePayload(cur))

	batch := last
	batch.Timestamp = cur.Timestamp
	data := map[string]interface{}{"milestones": milestones}
	// run_id is carried at the top level of both flat and batch payloads, so
	// reading it from last.Data is correct in either case.
	if m, ok := last.Data.(map[string]interface{}); ok {
		if rid, ok := m["run_id"].(string); ok {
			data["run_id"] = rid
		}
		for _, key := range milestoneRouteKeys {
			if v, ok := m[key].(string); ok && v != "" {
				data[key] = v
			}
		}
	}
	batch.Data = data
	return batch
}

// isMilestoneBatch reports whether data is a coalesced milestone batch — a map
// carrying a non-nil "milestones" slice.
func isMilestoneBatch(data interface{}) bool {
	_, ok := milestoneSlice(data)
	return ok
}

// milestoneSlice returns the "milestones" payload slice from a batched
// milestone Data map, reporting ok=false when the map is not a batch (i.e. it
// is a flat milestone).
func milestoneSlice(data interface{}) ([]interface{}, bool) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return nil, false
	}
	s, ok := m["milestones"].([]interface{})
	if !ok || s == nil {
		return nil, false
	}
	return s, true
}

// milestonePayload returns a FRESH copy of ev's flat milestone payload map.
// The input map is shared with other subscribers and the replay ring, so it
// is never returned by reference; a non-map Data yields an empty map.
func milestonePayload(ev events.UIEvent) map[string]interface{} {
	src, _ := ev.Data.(map[string]interface{})
	out := make(map[string]interface{}, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// copyMilestonePayload returns a fresh copy of a milestone payload map stored
// inside an existing batch's "milestones" slice, so neither the stored maps
// nor any input maps are mutated when the batch is extended.
func copyMilestonePayload(m interface{}) map[string]interface{} {
	src, _ := m.(map[string]interface{})
	out := make(map[string]interface{}, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
