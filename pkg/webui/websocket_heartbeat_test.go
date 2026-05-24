//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/gorilla/websocket"
)

// testingConnPair holds a connected client websocket.Conn and server SafeConn
// for testing WebSocket message handlers without a full ReactWebServer.
type testingConnPair struct {
	client *websocket.Conn
	server *SafeConn
}

// newTestingConnPair creates a connected client/server pair via an httptest
// server. The client side reads responses; the server side is used to write
// messages (e.g. from handleHeartbeatMessage).
func newTestingConnPair(t *testing.T) *testingConnPair {
	t.Helper()

	var serverConn *websocket.Conn
	connCh := make(chan *websocket.Conn, 1)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}
		connCh <- conn
	})

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	clientConn, _, err := (&websocket.Dialer{}).Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	t.Cleanup(func() { clientConn.Close() })

	serverConn = <-connCh

	return &testingConnPair{
		client: clientConn,
		server: NewSafeConn(serverConn),
	}
}

// newTestHeartbeatServer creates a ReactWebServer with an event bus for
// heartbeat-related tests.
func newTestHeartbeatServer(t *testing.T) *ReactWebServer {
	t.Helper()
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestHeartbeatMessageUpdatesLastSeen(t *testing.T) {
	server := newTestHeartbeatServer(t)

	// Create a client context and set its LastSeenAt to a time in the past
	ctx := server.getOrCreateClientContext("test-client")
	ctx.LastSeenAt = time.Now().Add(-5 * time.Minute)

	pair := newTestingConnPair(t)
	defer pair.server.Close()

	server.handleHeartbeatMessage(pair.server, "test-client")

	// Verify LastSeenAt was updated to a recent time
	if time.Since(ctx.LastSeenAt) > time.Second {
		t.Errorf("expected LastSeenAt to be recent, but it was %v ago", time.Since(ctx.LastSeenAt))
	}

	// Read and verify the heartbeat_ack response
	_, data, err := pair.client.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read heartbeat_ack: %v", err)
	}

	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to parse heartbeat_ack JSON: %v", err)
	}

	if msg["type"] != "heartbeat_ack" {
		t.Errorf("expected type=heartbeat_ack, got %v", msg["type"])
	}

	if _, ok := msg["data"].(map[string]interface{})["timestamp"]; !ok {
		t.Error("expected data.timestamp to be present in heartbeat_ack")
	}
}

func TestHeartbeatMessageSendsAck(t *testing.T) {
	server := newTestHeartbeatServer(t)

	// Create client context so touchClientLastSeen doesn't silently skip it
	_ = server.getOrCreateClientContext("test-client")

	pair := newTestingConnPair(t)
	defer pair.server.Close()

	server.handleHeartbeatMessage(pair.server, "test-client")

	_, data, err := pair.client.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Verify the response structure: { "type": "heartbeat_ack", "data": { "timestamp": ... } }
	if msg["type"] != "heartbeat_ack" {
		t.Errorf("expected type=heartbeat_ack, got %v", msg["type"])
	}

	dataMap, ok := msg["data"].(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be a map")
	}
	ts, ok := dataMap["timestamp"]
	if !ok {
		t.Fatal("expected data.timestamp to be present")
	}
	tsNum, ok := ts.(float64) // JSON numbers are float64 in Go
	if !ok {
		t.Errorf("expected timestamp to be a number, got %T", ts)
	}
	// Verify timestamp is reasonable (within last 60 seconds)
	now := float64(time.Now().Unix())
	if tsNum < now-60 || tsNum > now+1 {
		t.Errorf("unexpected timestamp %v, expected something near now=%v", tsNum, now)
	}
}

func TestHeartbeatMessageNonexistentClient(t *testing.T) {
	server := newTestHeartbeatServer(t)

	pair := newTestingConnPair(t)
	defer pair.server.Close()

	// Should not panic even though the client context doesn't exist
	// touchClientLastSeen handles this gracefully (does nothing)
	server.handleHeartbeatMessage(pair.server, "nonexistent-client")

	// Should still receive heartbeat_ack
	_, data, err := pair.client.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if msg["type"] != "heartbeat_ack" {
		t.Errorf("expected type=heartbeat_ack, got %v", msg["type"])
	}
}

func TestHeartbeatMessageDefaultClientID(t *testing.T) {
	server := newTestHeartbeatServer(t)

	// Create a context for the default client
	ctx := server.getOrCreateClientContext(defaultWebClientID)
	ctx.LastSeenAt = time.Now().Add(-2 * time.Minute)

	pair := newTestingConnPair(t)
	defer pair.server.Close()

	// Empty clientID should be normalized to defaultWebClientID
	server.handleHeartbeatMessage(pair.server, "")

	// Verify the default client's LastSeenAt was updated
	if time.Since(ctx.LastSeenAt) > time.Second {
		t.Errorf("expected default client LastSeenAt to be updated, but it was %v ago", time.Since(ctx.LastSeenAt))
	}

	// Should still receive heartbeat_ack
	_, data, err := pair.client.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if msg["type"] != "heartbeat_ack" {
		t.Errorf("expected type=heartbeat_ack, got %v", msg["type"])
	}
}
