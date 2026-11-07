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
	"github.com/sprout-foundry/sprout/pkg/events"
)

// A quiet client keeps getting pings, and a client that sends nothing for the
// inactivity limit is closed: the loop ends instead of spinning on the failed
// read.
func TestLiveLoopPingsAQuietClientThenClosesIt(t *testing.T) {
	prevPing, prevLimit := wsPingInterval, wsInactivityLimit
	wsPingInterval, wsInactivityLimit = 40*time.Millisecond, 400*time.Millisecond
	t.Cleanup(func() { wsPingInterval, wsInactivityLimit = prevPing, prevLimit })

	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	loopDone := make(chan struct{})
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer close(loopDone)
		defer conn.Close()
		server.runConnectionLiveLoop(conn, NewSafeConn(conn), "s-1", "webui-1234-5678", "", "", "", 0, true)
	}))
	defer ts.Close()

	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	pings := 0
	if err := client.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("read deadline: %v", err)
	}
	for {
		_, msg, err := client.ReadMessage()
		if err != nil {
			break
		}
		var ev struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(msg, &ev) == nil && ev.Type == "ping" {
			pings++
		}
	}
	if pings < 2 {
		t.Fatalf("got %d pings before the close, want at least 2", pings)
	}
	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("the live loop did not end after the inactivity limit")
	}
}
