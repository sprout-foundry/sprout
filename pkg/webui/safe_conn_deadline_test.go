//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A peer that stops reading must not block the writer forever: once the
// socket buffers fill, the write deadline fires and the connection is closed.
func TestSafeConnWriteDeadlineClosesStalledConnection(t *testing.T) {
	prev := safeConnWriteTimeout
	safeConnWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { safeConnWriteTimeout = prev })

	serverConn := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConn <- c
	}))
	t.Cleanup(srv.Close)

	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var conn *websocket.Conn
	select {
	case conn = <-serverConn:
	case <-time.After(5 * time.Second):
		t.Fatal("server never accepted the connection")
	}
	sc := NewSafeConn(conn)
	t.Cleanup(func() { _ = sc.Close() })

	payload := map[string]interface{}{"type": "stream_chunk", "data": map[string]interface{}{"chunk": strings.Repeat("x", 256*1024)}}
	deadline := time.Now().Add(20 * time.Second)
	var writeErr error
	for time.Now().Before(deadline) {
		if writeErr = sc.WriteJSON(payload); writeErr != nil {
			break
		}
	}
	if writeErr == nil {
		t.Fatal("writes to a non-reading peer never failed")
	}
	if !isWriteTimeout(writeErr) {
		t.Fatalf("write error = %v, want a timeout", writeErr)
	}
	if !sc.closed.Load() {
		t.Fatal("stalled connection was not marked closed")
	}
	if err := sc.WriteJSON(payload); err != nil {
		t.Fatalf("write after close should be a silent no-op, got %v", err)
	}
}
