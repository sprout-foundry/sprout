//go:build !js

package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestParseAfterSeqQuery(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"0", 0},
		{"42", 42},
		{"-5", 0},        // negative → 0
		{"abc", 0},       // unparseable → 0
		{"9999999999", 9999999999},
	}
	for _, c := range cases {
		if got := parseAfterSeqQuery(c.in); got != c.want {
			t.Errorf("parseAfterSeqQuery(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// helper: stand up a minimal ReactWebServer with one chat that already
// has some buffered events.
func setupReplayFixture(t *testing.T) (ws *ReactWebServer, clientID, chatID string) {
	t.Helper()
	ws = &ReactWebServer{
		eventBus:       events.NewEventBus(),
		clientContexts: map[string]*webClientContext{},
	}
	clientID = "client-A"
	chatID = "chat-1"
	ws.clientContexts[clientID] = &webClientContext{
		ChatSessions:  map[string]*chatSession{chatID: newChatSession(chatID, "Chat 1")},
		DefaultChatID: chatID,
	}

	// Drive 3 buffered events.
	for _, body := range []string{"a", "b", "c"} {
		ws.publishClientEventWithChat(clientID, chatID, events.EventTypeStreamChunk, map[string]interface{}{
			"content": body,
		})
	}
	return ws, clientID, chatID
}

func TestBuildChatRunReplayMessages_FullReplay(t *testing.T) {
	ws, clientID, chatID := setupReplayFixture(t)

	msgs := ws.buildChatRunReplayMessages(clientID, chatID, 0)
	// 1 restored frame + 3 events
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages (1 restored + 3 events), got %d", len(msgs))
	}
	restored := msgs[0]
	if restored["type"] != wsMessageTypeChatRunRestored {
		t.Errorf("first msg type = %v, want %s", restored["type"], wsMessageTypeChatRunRestored)
	}
	data := restored["data"].(map[string]interface{})
	if data["chat_id"] != chatID {
		t.Errorf("restored chat_id = %v, want %s", data["chat_id"], chatID)
	}
	if data["after_seq"] != int64(0) {
		t.Errorf("restored after_seq = %v, want 0", data["after_seq"])
	}
	if data["last_seq"] != int64(3) {
		t.Errorf("restored last_seq = %v, want 3", data["last_seq"])
	}
	if data["missed_chunks_count"] != 3 {
		t.Errorf("restored missed_chunks_count = %v, want 3", data["missed_chunks_count"])
	}
	if data["gap"] != false {
		t.Errorf("restored gap = %v, want false (nothing was evicted)", data["gap"])
	}

	// Each subsequent message should be a stream_chunk with the same shape
	// the live publish path would have produced.
	for i, body := range []string{"a", "b", "c"} {
		m := msgs[i+1]
		if m["type"] != events.EventTypeStreamChunk {
			t.Errorf("msg[%d] type = %v, want stream_chunk", i+1, m["type"])
		}
		d := m["data"].(map[string]interface{})
		if d["content"] != body {
			t.Errorf("msg[%d] data.content = %v, want %s", i+1, d["content"], body)
		}
		if d["__seq"] != int64(i+1) {
			t.Errorf("msg[%d] data.__seq = %v, want %d", i+1, d["__seq"], i+1)
		}
	}
}

func TestBuildChatRunReplayMessages_PartialReplay(t *testing.T) {
	ws, clientID, chatID := setupReplayFixture(t)

	msgs := ws.buildChatRunReplayMessages(clientID, chatID, 1)
	// 1 restored frame + 2 events (seq 2 and 3)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (1 restored + 2 events), got %d", len(msgs))
	}
	data := msgs[0]["data"].(map[string]interface{})
	if data["missed_chunks_count"] != 2 {
		t.Errorf("missed_chunks_count = %v, want 2", data["missed_chunks_count"])
	}
	if data["gap"] != false {
		t.Errorf("gap = %v, want false", data["gap"])
	}
}

func TestBuildChatRunReplayMessages_EmptyReplay(t *testing.T) {
	ws, clientID, chatID := setupReplayFixture(t)

	// Caller is already up to date.
	msgs := ws.buildChatRunReplayMessages(clientID, chatID, 3)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (restored frame only), got %d", len(msgs))
	}
	data := msgs[0]["data"].(map[string]interface{})
	if data["missed_chunks_count"] != 0 {
		t.Errorf("missed_chunks_count = %v, want 0", data["missed_chunks_count"])
	}
}

func TestBuildChatRunReplayMessages_GapAfterEviction(t *testing.T) {
	ws, clientID, chatID := setupReplayFixture(t)

	// Force eviction: shrink the buffer cap and push more events.
	cs := ws.clientContexts[clientID].ChatSessions[chatID]
	cs.mu.Lock()
	cs.runBuffer = newChatRunRingBufferWithCaps(2, 1024*1024)
	cs.mu.Unlock()
	for _, body := range []string{"d", "e", "f"} {
		ws.publishClientEventWithChat(clientID, chatID, events.EventTypeStreamChunk, map[string]interface{}{
			"content": body,
		})
	}
	// Buffer now holds seq 2, 3 (only — capacity 2). Asking for after seq 0
	// is a confidence problem: we evicted seq 1.

	msgs := ws.buildChatRunReplayMessages(clientID, chatID, 0)
	data := msgs[0]["data"].(map[string]interface{})
	if data["gap"] != true {
		t.Errorf("expected gap=true when afterSeq is older than oldest retained, got %v", data["gap"])
	}
}

func TestBuildChatRunReplayMessages_UnknownClient(t *testing.T) {
	ws := &ReactWebServer{
		eventBus:       events.NewEventBus(),
		clientContexts: map[string]*webClientContext{},
	}
	msgs := ws.buildChatRunReplayMessages("unknown", "chat-1", 0)
	// Always emit at least the restored frame so the client doesn't hang.
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (empty restored), got %d", len(msgs))
	}
	data := msgs[0]["data"].(map[string]interface{})
	if data["last_seq"] != int64(0) || data["missed_chunks_count"] != 0 {
		t.Errorf("expected empty replay metadata for unknown client, got %+v", data)
	}
}

func TestBuildChatRunReplayMessages_ChatWithNoBufferYet(t *testing.T) {
	ws := &ReactWebServer{
		eventBus:       events.NewEventBus(),
		clientContexts: map[string]*webClientContext{},
	}
	ws.clientContexts["c"] = &webClientContext{
		ChatSessions:  map[string]*chatSession{"chat-1": newChatSession("chat-1", "Chat 1")},
		DefaultChatID: "chat-1",
	}
	// runBuffer is nil because no events have been published yet.
	msgs := ws.buildChatRunReplayMessages("c", "chat-1", 0)
	if len(msgs) != 1 {
		t.Fatalf("expected just the restored frame, got %d msgs", len(msgs))
	}
}
