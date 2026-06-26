//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestReplayCapturesLiveEventsDuringFlush proves that when a client
// reconnects with ?reattach=<chat_id>&after_seq=<n>, any live events
// published by the agent DURING the replay flush are captured and
// delivered after the replay batch instead of being lost.
//
// Regression test for: "Lost Live Events During WebSocket Reconnection Replay"
// where the EventBus subscription happened AFTER replay, so events published
// during the replay window were permanently dropped.
func TestReplayCapturesLiveEventsDuringFlush(t *testing.T) {
	bus := events.NewEventBus()
	srv, err := NewReactWebServer(nil, bus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}
	srv.upgrader.CheckOrigin = func(_ *http.Request) bool { return true }

	clientID := "replay-client"
	chatID := "chat-replay-test"

	// Pre-populate the run buffer with 3 events so replay has something to flush.
	srv.clientContexts[clientID] = &webClientContext{
		ChatSessions:  map[string]*chatSession{chatID: newChatSession(chatID, "Replay Test")},
		DefaultChatID: chatID,
	}
	for _, body := range []string{"buf-a", "buf-b", "buf-c"} {
		srv.publishClientEventWithChat(clientID, chatID, events.EventTypeStreamChunk, map[string]interface{}{
			"content": body,
		})
	}

	ts := httptest.NewServer(http.HandlerFunc(srv.handleWebSocket))
	defer ts.Close()

	// Publish a live event shortly after the client connects.
	// The handler's replay loop runs synchronously, so this goroutine
	// will race with it — the live event should be captured by the
	// drain goroutine, not lost.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Wait just enough for the connection_status to be sent,
		// then publish a live event during the replay window.
		time.Sleep(50 * time.Millisecond)
		srv.publishClientEventWithChat(clientID, chatID, events.EventTypeStreamChunk, map[string]interface{}{
			"content": "live-d",
			"__live":  true,
		})
	}()

	// Connect with reattach params so the handler replays buffered events.
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "?reattach=" + chatID + "&after_seq=0"
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second
	conn, _, err := dialer.Dial(url, http.Header{"X-Sprout-Client-ID": {clientID}})
	if err != nil {
		t.Fatalf("dial WebSocket: %v", err)
	}
	defer conn.Close()

	// Collect all messages for a window.
	var received []map[string]interface{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Errorf("unmarshal: %v — raw: %s", err, data)
				continue
			}
			received = append(received, msg)
		}
	}()

	wg.Wait()
	// Give the goroutine a bit more time to drain.
	time.Sleep(200 * time.Millisecond)
	conn.Close()
	<-done

	// Find the live event among received messages.
	var foundLive bool
	for _, msg := range received {
		if msg["type"] == events.EventTypeStreamChunk {
			if d, ok := msg["data"].(map[string]interface{}); ok {
				if d["__live"] == true {
					foundLive = true
					break
				}
			}
		}
	}

	if !foundLive {
		t.Errorf("live event published during replay was NOT received by the client — "+
			"received %d messages:\n%s", len(received), formatMessages(received))
	}
}

func formatMessages(msgs []map[string]interface{}) string {
	var parts []string
	for i, m := range msgs {
		parts = append(parts, fmt.Sprintf("  [%d] type=%v", i, m["type"]))
	}
	return strings.Join(parts, "\n")
}
