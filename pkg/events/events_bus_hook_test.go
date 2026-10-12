package events

import (
	"testing"
	"time"
)

func TestRunBoundaryEventsAreCritical(t *testing.T) {
	for _, typ := range []string{EventTypeQueryStarted, EventTypeQueryCompleted, EventTypeError, EventTypeWorkspaceStopping} {
		if !isCriticalEvent(typ) {
			t.Errorf("isCriticalEvent(%q) = false, want true", typ)
		}
	}
	if isCriticalEvent(EventTypeStreamChunk) {
		t.Errorf("stream_chunk must stay droppable")
	}
}

// A full subscriber channel must not swallow query_completed: the critical
// drain-replace path evicts an older event to make room for it.
func TestQueryCompletedSurvivesFullSubscriber(t *testing.T) {
	eb := NewEventBus()
	ch := make(chan UIEvent, 1)
	eb.mutex.Lock()
	eb.subscribers["full"] = ch
	eb.mutex.Unlock()
	t.Cleanup(func() {
		eb.mutex.Lock()
		delete(eb.subscribers, "full")
		eb.mutex.Unlock()
		close(ch)
	})

	eb.Publish(EventTypeStreamChunk, StreamChunkEvent("old", "assistant_text"))
	eb.Publish(EventTypeQueryCompleted, QueryCompletedEvent("q", "done", 0, 0, time.Second))

	select {
	case ev := <-ch:
		if ev.Type != EventTypeQueryCompleted {
			t.Fatalf("got %q, want query_completed", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("query_completed was dropped")
	}
}

func TestPublishHookRewritesPayload(t *testing.T) {
	eb := NewEventBus()
	defer cleanupBus(eb)
	ch := eb.Subscribe("sub")

	var seenType string
	eb.SetPublishHook(func(eventType string, data any) any {
		seenType = eventType
		m := data.(map[string]any)
		out := make(map[string]any, len(m)+1)
		for k, v := range m {
			out[k] = v
		}
		out["stamped"] = true
		return out
	})

	eb.Publish(EventTypeToolStart, map[string]any{"tool": "x"})
	select {
	case ev := <-ch:
		data := ev.Data.(map[string]any)
		if data["stamped"] != true || data["tool"] != "x" {
			t.Fatalf("hook output not delivered: %#v", data)
		}
	case <-time.After(time.Second):
		t.Fatal("event not delivered")
	}
	if seenType != EventTypeToolStart {
		t.Fatalf("hook saw type %q", seenType)
	}

	eb.SetPublishHook(nil)
	eb.Publish(EventTypeToolStart, map[string]any{"tool": "y"})
	select {
	case ev := <-ch:
		if _, ok := ev.Data.(map[string]any)["stamped"]; ok {
			t.Fatal("hook still applied after removal")
		}
	case <-time.After(time.Second):
		t.Fatal("event not delivered")
	}
}
