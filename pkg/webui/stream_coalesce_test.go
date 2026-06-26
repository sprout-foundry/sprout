package webui

import (
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
