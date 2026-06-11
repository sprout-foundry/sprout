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
