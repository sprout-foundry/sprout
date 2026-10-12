//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sprout-foundry/sprout/pkg/events"
)

func newStampingServer(t *testing.T, clientID, chatID string) *ReactWebServer {
	t.Helper()
	ws := &ReactWebServer{
		eventBus:       events.NewEventBus(),
		clientContexts: map[string]*webClientContext{},
	}
	ws.clientContexts[clientID] = &webClientContext{
		ChatSessions:  map[string]*chatSession{chatID: newChatSession(chatID, "Chat")},
		DefaultChatID: chatID,
	}
	ws.installRunBufferStamping()
	t.Cleanup(func() { ws.eventBus.SetPublishHook(nil) })
	return ws
}

func receiveEvent(t *testing.T, ch <-chan events.UIEvent) events.UIEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
	return events.UIEvent{}
}

// Agent events reach the bus without passing publishClientEventWithChat; the
// hook must buffer them and give the live frame the same seq the buffer holds.
func TestStampChatEvent_AgentEventsGetBufferSeq(t *testing.T) {
	ws := newStampingServer(t, "c1", "chat-1")
	ch := ws.eventBus.Subscribe("test")
	t.Cleanup(func() { ws.eventBus.Unsubscribe("test") })

	original := map[string]interface{}{"chunk": "hi", "client_id": "c1", "chat_id": "chat-1"}
	ws.eventBus.Publish(events.EventTypeStreamChunk, original)

	ev := receiveEvent(t, ch)
	data := ev.Data.(map[string]interface{})
	cs := ws.clientContexts["c1"].ChatSessions["chat-1"]
	if cs.runBuffer == nil || cs.runBuffer.Len() != 1 {
		t.Fatalf("agent event was not buffered")
	}
	if got := eventSeq(data); got == 0 || got != cs.runBuffer.LastSeq() {
		t.Fatalf("live __seq = %d, buffer last seq = %d", got, cs.runBuffer.LastSeq())
	}
	if _, mutated := original["__seq"]; mutated {
		t.Fatal("publisher's map was mutated")
	}
}

func TestStampChatEvent_LeavesUnbufferedEventsAlone(t *testing.T) {
	ws := newStampingServer(t, "c1", "chat-1")
	for _, tc := range []struct {
		typ  string
		data map[string]interface{}
	}{
		{events.EventTypeFileChanged, map[string]interface{}{"client_id": "c1", "chat_id": "chat-1"}},
		{events.EventTypeStreamChunk, map[string]interface{}{"client_id": "c1"}},
		{events.EventTypeStreamChunk, map[string]interface{}{"client_id": "c1", "chat_id": "unknown"}},
	} {
		out := ws.stampChatEvent(tc.typ, tc.data).(map[string]interface{})
		if _, ok := out["__seq"]; ok {
			t.Errorf("%s %v was stamped", tc.typ, tc.data)
		}
	}
}

// Events published through publishClientEventWithChat are already stamped;
// the hook must not buffer them a second time.
func TestStampChatEvent_SkipsAlreadyStamped(t *testing.T) {
	ws := newStampingServer(t, "c1", "chat-1")
	ws.publishClientEventWithChat("c1", "chat-1", events.EventTypeToolStart, map[string]interface{}{"tool": "x"})
	if got := ws.clientContexts["c1"].ChatSessions["chat-1"].runBuffer.Len(); got != 1 {
		t.Fatalf("buffer holds %d events, want 1", got)
	}
}

func TestRunBufferSeqSeedIsMonotonicAcrossBuffers(t *testing.T) {
	prev := runBufferSeqSeed
	runBufferSeqSeed = func() int64 { return time.Now().UnixMicro() }
	t.Cleanup(func() { runBufferSeqSeed = prev })

	first := newSeededChatRunRingBuffer()
	var last int64
	for i := 0; i < 3; i++ {
		last = first.appendStamped(events.UIEvent{Type: events.EventTypeStreamChunk}, map[string]interface{}{})
	}
	time.Sleep(2 * time.Millisecond)
	second := newSeededChatRunRingBuffer()
	next := second.appendStamped(events.UIEvent{Type: events.EventTypeStreamChunk}, map[string]interface{}{})
	if next <= last {
		t.Fatalf("recreated buffer started at %d, not after previous last seq %d", next, last)
	}
	if float64(next) >= 1<<53 {
		t.Fatalf("seq %d exceeds the exact range of a JS number", next)
	}
}

func TestMergedStreamChunkCarriesLaterSeq(t *testing.T) {
	a := events.UIEvent{Type: events.EventTypeStreamChunk, Data: map[string]interface{}{"chunk": "a", "__seq": int64(7)}}
	b := events.UIEvent{Type: events.EventTypeStreamChunk, Data: map[string]interface{}{"chunk": "b", "__seq": int64(8)}}
	out := coalesceStreamChunks([]events.UIEvent{a, b})
	if len(out) != 1 {
		t.Fatalf("expected one merged event, got %d", len(out))
	}
	if got := eventSeq(out[0].Data); got != 8 {
		t.Fatalf("merged __seq = %d, want 8", got)
	}
}

// frameReader pumps a socket's frames into a channel so a test can wait on
// quiet windows without a read deadline (gorilla poisons a connection after
// any read error, timeouts included).
func frameReader(t *testing.T, conn *websocket.Conn) <-chan map[string]interface{} {
	t.Helper()
	out := make(chan map[string]interface{}, 256)
	go func() {
		defer close(out)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if json.Unmarshal(raw, &msg) != nil {
				continue
			}
			if msg["type"] == "session_conflict" {
				_ = conn.WriteJSON(map[string]string{"type": "session_takeover"})
				continue
			}
			out <- msg
		}
	}()
	return out
}

// readFrames collects frames until none arrive for the idle window.
func readFrames(frames <-chan map[string]interface{}, idle time.Duration) []map[string]interface{} {
	var out []map[string]interface{}
	for {
		select {
		case msg, ok := <-frames:
			if !ok {
				return out
			}
			out = append(out, msg)
		case <-time.After(idle):
			return out
		}
	}
}

func chunkFrames(frames []map[string]interface{}) (texts []string, seqs []int64) {
	for _, f := range frames {
		if f["type"] != events.EventTypeStreamChunk {
			continue
		}
		d := f["data"].(map[string]interface{})
		texts = append(texts, d["chunk"].(string))
		seqs = append(seqs, int64(d["__seq"].(float64)))
	}
	return texts, seqs
}

// End to end: chunks published while the client is disconnected are replayed
// when it reattaches after its last seen seq, and nothing it already saw is
// delivered twice.
func TestReattachDeliversEventsMissedWhileDisconnected(t *testing.T) {
	bus := events.NewEventBus()
	srv, err := NewReactWebServer(nil, bus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}
	srv.upgrader.CheckOrigin = func(*http.Request) bool { return true }
	srv.agentEnforceSingleSession = true
	srv.installRunBufferStamping()
	t.Cleanup(func() { bus.SetPublishHook(nil) })

	const clientID, chatID = "reattach-client", "chat-reattach"
	srv.clientContexts[clientID] = &webClientContext{
		ChatSessions:  map[string]*chatSession{chatID: newChatSession(chatID, "Reattach")},
		DefaultChatID: chatID,
	}
	publish := func(text string) {
		bus.Publish(events.EventTypeStreamChunk, map[string]interface{}{
			"chunk": text, "content_type": "assistant_text", "client_id": clientID, "chat_id": chatID,
		})
	}

	ts := httptest.NewServer(http.HandlerFunc(srv.handleWebSocket))
	t.Cleanup(ts.Close)
	base := "ws" + strings.TrimPrefix(ts.URL, "http")
	header := http.Header{"X-Sprout-Client-ID": {clientID}}

	conn, _, err := websocket.DefaultDialer.Dial(base+"?chat_id="+chatID, header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	frames1 := frameReader(t, conn)
	readFrames(frames1, 200*time.Millisecond)
	publish("one")
	publish("two")
	texts, seqs := chunkFrames(readFrames(frames1, 300*time.Millisecond))
	if strings.Join(texts, "") != "onetwo" || len(seqs) == 0 {
		t.Fatalf("live frames before disconnect = %v", texts)
	}
	lastSeen := seqs[len(seqs)-1]
	_ = conn.Close()

	// Published while no socket is attached.
	time.Sleep(100 * time.Millisecond)
	publish("three")
	publish("four")

	url := base + "?reattach=" + chatID + "&after_seq=" + strconv.FormatInt(lastSeen, 10)
	conn2, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatalf("redial: %v", err)
	}
	t.Cleanup(func() { _ = conn2.Close() })
	frames2 := frameReader(t, conn2)
	frames := readFrames(frames2, 300*time.Millisecond)
	publish("five")
	frames = append(frames, readFrames(frames2, 300*time.Millisecond)...)

	restored := false
	for _, f := range frames {
		if f["type"] == wsMessageTypeChatRunRestored {
			restored = true
		}
	}
	if !restored {
		t.Fatal("reattach did not lead with chat_run_restored")
	}
	texts, seqs = chunkFrames(frames)
	if got := strings.Join(texts, ""); got != "threefourfive" {
		t.Fatalf("after reattach got chunks %q, want threefourfive (frames: %s)", got, formatMessages(frames))
	}
	seen := map[int64]bool{}
	for _, s := range seqs {
		if s <= lastSeen {
			t.Errorf("seq %d was already seen before the disconnect", s)
		}
		if seen[s] {
			t.Errorf("seq %d delivered twice", s)
		}
		seen[s] = true
	}
}
