//go:build !js

package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestPublishClientEventWithChat_AppendsStreamChunksToBuffer pins the
// SP-034-2b wire-up: when publishClientEventWithChat fires a reattach-
// relevant event type for a known chat, that event lands in the chat's
// runBuffer with a monotonic seq. Events for unknown chats or
// non-reattach-relevant types are NOT buffered.
func TestPublishClientEventWithChat_AppendsStreamChunksToBuffer(t *testing.T) {
	ws := &ReactWebServer{
		eventBus:       events.NewEventBus(),
		clientContexts: map[string]*webClientContext{},
	}

	const (
		clientID = "client-A"
		chatID   = "chat-1"
	)

	// Seed a client context with one chat session.
	ws.clientContexts[clientID] = &webClientContext{
		ChatSessions:  map[string]*chatSession{chatID: newChatSession(chatID, "Chat 1")},
		DefaultChatID: chatID,
	}

	// Stream chunk → buffered.
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeStreamChunk, map[string]interface{}{
		"content": "hello",
	})
	// Tool start → buffered.
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeToolStart, map[string]interface{}{
		"name": "read_file",
	})
	// File-changed → NOT buffered (not in reattachBufferedEventTypes).
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeFileChanged, map[string]interface{}{
		"path": "main.go",
	})

	cs := ws.clientContexts[clientID].ChatSessions[chatID]
	if cs.runBuffer == nil {
		t.Fatal("publishClientEventWithChat didn't lazy-create runBuffer on first reattach-relevant event")
	}
	if got := cs.runBuffer.Len(); got != 2 {
		t.Errorf("runBuffer.Len = %d, want 2 (stream_chunk + tool_start; file_changed should be skipped)", got)
	}
	if got := cs.runBuffer.LastSeq(); got != 2 {
		t.Errorf("runBuffer.LastSeq = %d, want 2", got)
	}
}

func TestPublishClientEventWithChat_NoChatIDSkipsBuffer(t *testing.T) {
	ws := &ReactWebServer{
		eventBus:       events.NewEventBus(),
		clientContexts: map[string]*webClientContext{},
	}
	// Empty chatID means the event is global to the client — nothing to
	// buffer because there's no per-chat replay surface to feed.
	ws.publishClientEventWithChat("client-X", "", events.EventTypeStreamChunk, map[string]interface{}{
		"content": "hello",
	})
	// No client context, no chat — should not panic and obviously nothing buffered.
}

func TestPublishClientEventWithChat_UnknownClientSkipsBuffer(t *testing.T) {
	ws := &ReactWebServer{
		eventBus:       events.NewEventBus(),
		clientContexts: map[string]*webClientContext{},
	}
	// A chatID with no matching client context: must not panic, must not
	// invent a client context, must not buffer.
	ws.publishClientEventWithChat("unknown", "chat-1", events.EventTypeStreamChunk, map[string]interface{}{
		"content": "hi",
	})
	if _, exists := ws.clientContexts["unknown"]; exists {
		t.Error("appendChatEventToRunBuffer should not implicitly create a client context")
	}
}

func TestPublishClientEventWithChat_StampsSeqOntoEventData(t *testing.T) {
	ws := &ReactWebServer{
		eventBus:       events.NewEventBus(),
		clientContexts: map[string]*webClientContext{},
	}
	const (
		clientID = "client-A"
		chatID   = "chat-1"
	)
	ws.clientContexts[clientID] = &webClientContext{
		ChatSessions:  map[string]*chatSession{chatID: newChatSession(chatID, "Chat 1")},
		DefaultChatID: chatID,
	}

	data := map[string]interface{}{"content": "hi"}
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeStreamChunk, data)

	seq, ok := data["__seq"]
	if !ok {
		t.Fatal("expected __seq to be stamped onto the event data after buffer append")
	}
	if seq != int64(1) {
		t.Errorf("__seq = %v, want 1", seq)
	}
}
