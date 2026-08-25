//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The frontend TerminalWebSocketService pings every 30s and force-reconnects
// when no pong arrives within its watchdog window (90s). The terminal WS
// handler historically had no "ping" case, so every idle-but-healthy terminal
// was cycled by that watchdog — and the reconnect's term.reset() destroyed
// any running full-screen TUI (the "vim freezes" bug). This pins the
// ping → pong contract on the terminal socket.
func TestTerminalWebSocketRespondsToPing(t *testing.T) {
	srv := &ReactWebServer{}
	server := httptest.NewServer(http.HandlerFunc(srv.handleTerminalWebSocket))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/terminal"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// session_created arrives first; then send a ping and expect a pong.
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read session_created failed: %v", err)
	}
	var created map[string]any
	if err := json.Unmarshal(msg, &created); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if created["type"] != "session_created" {
		t.Fatalf("expected session_created first, got %v", created["type"])
	}

	if err := conn.WriteJSON(map[string]any{"type": "ping"}); err != nil {
		t.Fatalf("send ping failed: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read pong failed: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(msg, &m); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if m["type"] == "pong" {
			return // pass
		}
	}
}
