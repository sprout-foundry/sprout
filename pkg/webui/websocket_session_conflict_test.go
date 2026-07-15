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

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testServer wraps an httptest.Server + ReactWebServer for test cleanup.
type testServer struct {
	ts  *httptest.Server
	srv *ReactWebServer
	bus *events.EventBus
}

// newTestServer creates a minimal ReactWebServer behind an httptest.Server
// for integration tests. The upgrader CheckOrigin is overridden to always
// accept so the httptest server can complete the WebSocket handshake.
func newTestServer(t *testing.T, preConfigure func(*ReactWebServer)) *testServer {
	t.Helper()
	bus := events.NewEventBus()
	srv, err := NewReactWebServer(nil, bus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}
	// Override CheckOrigin so the httptest server can complete the WS handshake.
	srv.upgrader.CheckOrigin = func(_ *http.Request) bool { return true }
	// SP-118 Phase 1: route to Mode 1 (single-active-session). Existing
	// TestSessionConflict_* tests exercise the takeover flow which only
	// exists on the Mode 1 path. The preConfigure callback can still
	// override this for tests that specifically need Mode 2.
	srv.agentEnforceSingleSession = true
	if preConfigure != nil {
		preConfigure(srv)
	}

	// Wire the handler directly — httptest.NewServer serves on "/", so no
	// extra path prefix is needed for the client to dial.
	ts := httptest.NewServer(http.HandlerFunc(srv.handleWebSocket))
	t.Cleanup(func() {
		ts.Close()
	})

	return &testServer{
		ts:  ts,
		srv: srv,
		bus: bus,
	}
}

// wsDialURL returns the ws:// URL for the test server.
func (ts *testServer) wsDialURL() string {
	return "ws" + strings.TrimPrefix(ts.ts.URL, "http")
}

// dialConn dials the WebSocket endpoint with the given clientID header value.
func (ts *testServer) dialConn(t *testing.T, clientID string) *websocket.Conn {
	t.Helper()
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	headers := http.Header{}
	if clientID != "" {
		headers.Set("X-Sprout-Client-ID", clientID)
	}

	conn, _, err := dialer.Dial(ts.wsDialURL(), headers)
	if err != nil {
		t.Fatalf("dial WebSocket: %v", err)
	}
	return conn
}

// dialConnWithHeaders dials with custom headers for service-mode tests.
func (ts *testServer) dialConnWithHeaders(t *testing.T, headers http.Header) *websocket.Conn {
	t.Helper()
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	conn, _, err := dialer.Dial(ts.wsDialURL(), headers)
	if err != nil {
		t.Fatalf("dial WebSocket with custom headers: %v", err)
	}
	return conn
}

// readMessage reads a JSON message with a timeout and returns it as a map.
func readMessage(t *testing.T, conn *websocket.Conn, timeout time.Duration) map[string]interface{} {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(timeout))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("readMessage unmarshal: %v — raw: %s", err, data)
	}
	return msg
}

// sendMessage writes a JSON message to the connection.
func sendMessage(t *testing.T, conn *websocket.Conn, msg map[string]interface{}) {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("sendMessage marshal: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("sendMessage write: %v", err)
	}
}

// expectType reads a message and asserts its "type" field.
func expectType(t *testing.T, conn *websocket.Conn, timeout time.Duration, wantType string) map[string]interface{} {
	t.Helper()
	msg := readMessage(t, conn, timeout)
	if msg["type"] != wantType {
		t.Fatalf("expected type %q, got %q (full message: %v)", wantType, msg["type"], msg)
	}
	return msg
}

// ---------------------------------------------------------------------------
// Tests: Normal flow — no conflict
// ---------------------------------------------------------------------------

// TestSessionConflict_NoConflict verifies that a single WebSocket connection
// receives a connection_status message (not session_conflict) and that the
// connection is properly tracked in activeWSByUserID.
func TestSessionConflict_NoConflict(t *testing.T) {
	ts := newTestServer(t, nil)
	conn := ts.dialConn(t, "client-a")
	defer conn.Close()

	// Should get connection_status, NOT session_conflict
	msg := expectType(t, conn, 5*time.Second, "connection_status")

	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		t.Fatal("expected data object in connection_status message")
	}
	if data["connected"] != true {
		t.Errorf("expected connected=true, got %v", data["connected"])
	}
	if data["session_id"] == "" {
		t.Error("expected non-empty session_id in connection_status")
	}
	if data["client_id"] != "client-a" {
		t.Errorf("expected client_id=client-a, got %v", data["client_id"])
	}

	// Verify connection is tracked in activeWSByUserID
	val, loaded := ts.srv.activeWSByUserID.Load("client-a")
	if !loaded {
		t.Fatal("expected activeWSByUserID to contain client-a")
	}
	active, ok := val.(*activeWSConn)
	if !ok {
		t.Fatal("expected *activeWSConn from activeWSByUserID")
	}
	if active.sessionID == "" {
		t.Error("expected non-empty sessionID in activeWSConn")
	}
	if active.connectedAt.IsZero() {
		t.Error("expected non-zero connectedAt in activeWSConn")
	}
}

// ---------------------------------------------------------------------------
// Tests: Conflict detection — second connection gets session_conflict
// ---------------------------------------------------------------------------

// TestSessionConflict_SecondConnection_GetConflict verifies that when a second
// WebSocket connects with the same tracking key, it receives a session_conflict
// message and the first connection remains undisturbed.
func TestSessionConflict_SecondConnection_GetConflict(t *testing.T) {
	ts := newTestServer(t, nil)
	conn1 := ts.dialConn(t, "client-b")
	defer conn1.Close()

	// First connection gets connection_status
	msg1 := expectType(t, conn1, 5*time.Second, "connection_status")
	sessionID1 := msg1["data"].(map[string]interface{})["session_id"].(string)

	// Second connection with same clientID should get session_conflict
	conn2 := ts.dialConn(t, "client-b")
	defer conn2.Close()

	msg2 := expectType(t, conn2, 5*time.Second, "session_conflict")
	data := msg2["data"].(map[string]interface{})

	// The conflict message should include the existing session ID
	if data["existing_session_id"] != sessionID1 {
		t.Errorf("expected existing_session_id=%q, got %q", sessionID1, data["existing_session_id"])
	}
	if _, ok := data["connected_at"].(float64); !ok {
		t.Error("expected connected_at as a numeric timestamp in session_conflict data")
	}

	// Close the second connection without confirming takeover
	conn2.Close()

	// Give the handler a moment to process the close
	time.Sleep(200 * time.Millisecond)

	// First connection should still be alive and tracked
	val, loaded := ts.srv.activeWSByUserID.Load("client-b")
	if !loaded {
		t.Fatal("expected client-b to still be active after second connection closed")
	}
	active, ok := val.(*activeWSConn)
	if !ok || active.sessionID != sessionID1 {
		t.Errorf("expected original connection to still be active, got sessionID=%v", active.sessionID)
	}
}

// ---------------------------------------------------------------------------
// Tests: Takeover flow — new device confirms, old device evicted
// ---------------------------------------------------------------------------

// TestSessionConflict_Takeover verifies the full takeover flow:
// 1. First connection is established
// 2. Second connection receives session_conflict
// 3. Second connection sends session_takeover
// 4. First connection receives session_displaced and is closed
// 5. Second connection proceeds with connection_status
func TestSessionConflict_Takeover(t *testing.T) {
	ts := newTestServer(t, nil)
	conn1 := ts.dialConn(t, "client-c")
	defer conn1.Close()

	// First connection gets connection_status
	expectType(t, conn1, 5*time.Second, "connection_status")

	// Second connection with same clientID
	conn2 := ts.dialConn(t, "client-c")
	defer conn2.Close()

	// Second connection gets session_conflict
	expectType(t, conn2, 5*time.Second, "session_conflict")

	// Second connection confirms takeover
	sendMessage(t, conn2, map[string]interface{}{
		"type": "session_takeover",
	})

	// First connection should receive session_displaced
	msg1b := expectType(t, conn1, 5*time.Second, "session_displaced")
	data := msg1b["data"].(map[string]interface{})
	if data["reason"] != "session_taken_over" {
		t.Errorf("expected reason=session_taken_over, got %q", data["reason"])
	}
	if data["message"] == "" {
		t.Error("expected non-empty message in session_displaced data")
	}

	// First connection should be closed (read should error or timeout)
	conn1.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, _, err := conn1.ReadMessage()
	if err == nil {
		t.Fatal("expected first connection to be closed after displacement")
	}

	// Second connection should now receive connection_status
	msg2b := expectType(t, conn2, 5*time.Second, "connection_status")
	data2b := msg2b["data"].(map[string]interface{})

	// Verify second connection is now the active one (by session ID)
	val, loaded := ts.srv.activeWSByUserID.Load("client-c")
	if !loaded {
		t.Fatal("expected client-c to be active after takeover")
	}
	active, ok := val.(*activeWSConn)
	if !ok {
		t.Fatal("expected *activeWSConn from activeWSByUserID")
	}
	if active.sessionID != data2b["session_id"] {
		t.Errorf("active session ID mismatch: active=%q, connection_status=%q", active.sessionID, data2b["session_id"])
	}
}

// TestSessionConflict_Takeover_UserMode verifies takeover works in service mode
// where the tracking key is the UserID (from trusted header) rather than clientID.
func TestSessionConflict_Takeover_UserMode(t *testing.T) {
	ts := newTestServer(t, func(srv *ReactWebServer) {
		srv.serviceMode = true
		srv.trustedUserHeader = "X-User-ID"
	})

	// Connect first "device" — both use same user ID via header override
	headers1 := http.Header{}
	headers1.Set("X-Sprout-Client-ID", "device-1")
	headers1.Set("X-User-ID", "user-42")
	conn1 := ts.dialConnWithHeaders(t, headers1)
	defer conn1.Close()

	expectType(t, conn1, 5*time.Second, "connection_status")

	// Connect second "device" with same user ID but different client ID
	headers2 := http.Header{}
	headers2.Set("X-Sprout-Client-ID", "device-2")
	headers2.Set("X-User-ID", "user-42")
	conn2 := ts.dialConnWithHeaders(t, headers2)
	defer conn2.Close()

	// Should get session_conflict because user-42 already has an active session
	expectType(t, conn2, 5*time.Second, "session_conflict")

	// Confirm takeover
	sendMessage(t, conn2, map[string]interface{}{"type": "session_takeover"})

	// Old device should be displaced
	expectType(t, conn1, 5*time.Second, "session_displaced")

	// New device should get connection_status
	msg2b := expectType(t, conn2, 5*time.Second, "connection_status")
	data2b := msg2b["data"].(map[string]interface{})

	// The active key should be the user ID, not the client ID
	val, loaded := ts.srv.activeWSByUserID.Load("user-42")
	if !loaded {
		t.Fatal("expected user-42 to be active after takeover")
	}
	active, ok := val.(*activeWSConn)
	if !ok {
		t.Fatal("expected *activeWSConn from activeWSByUserID for user-42")
	}
	if active.sessionID != data2b["session_id"] {
		t.Errorf("active session ID mismatch: active=%q, connection_status=%q", active.sessionID, data2b["session_id"])
	}
}

// ---------------------------------------------------------------------------
// Tests: No conflict with different tracking keys
// ---------------------------------------------------------------------------

// TestSessionConflict_DifferentTrackingKeys_NoConflict verifies that two
// connections with different tracking keys (different clientIDs in local mode)
// do NOT conflict.
func TestSessionConflict_DifferentTrackingKeys_NoConflict(t *testing.T) {
	ts := newTestServer(t, nil)
	conn1 := ts.dialConn(t, "client-x")
	defer conn1.Close()
	conn2 := ts.dialConn(t, "client-y")
	defer conn2.Close()

	// Both should get connection_status, no conflict
	expectType(t, conn1, 5*time.Second, "connection_status")
	expectType(t, conn2, 5*time.Second, "connection_status")

	// Both should be tracked independently
	if _, loaded := ts.srv.activeWSByUserID.Load("client-x"); !loaded {
		t.Error("expected client-x to be tracked")
	}
	if _, loaded := ts.srv.activeWSByUserID.Load("client-y"); !loaded {
		t.Error("expected client-y to be tracked")
	}
}

// ---------------------------------------------------------------------------
// Tests: evictExistingConnection
// ---------------------------------------------------------------------------

func TestEvictExistingConnection_Success(t *testing.T) {
	ts := newTestServer(t, nil)

	// Create a real connection so we have a proper activeWSConn
	conn := ts.dialConn(t, "evict-test")
	defer conn.Close()
	expectType(t, conn, 5*time.Second, "connection_status")

	// Now evict it
	result := ts.srv.evictExistingConnection("evict-test")
	if !result {
		t.Error("expected evictExistingConnection to return true")
	}

	// Entry should be gone
	if _, loaded := ts.srv.activeWSByUserID.Load("evict-test"); loaded {
		t.Error("expected evictExistingConnection to remove the entry")
	}
}

func TestEvictExistingConnection_NoExisting(t *testing.T) {
	ts := newTestServer(t, nil)

	// Try to evict a key that doesn't exist
	result := ts.srv.evictExistingConnection("nonexistent-key")
	if result {
		t.Error("expected evictExistingConnection to return false when no existing connection")
	}
}

// ---------------------------------------------------------------------------
// Tests: waitForTakeover
// ---------------------------------------------------------------------------

// newWaitForTakeoverTestServer creates a test server whose handler calls
// waitForTakeover and writes the result back as JSON. Used by the
// waitForTakeover unit tests.
func newWaitForTakeoverTestServer(t *testing.T, srv *ReactWebServer) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := srv.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		result := srv.waitForTakeover(conn, "test-session")
		resp := map[string]interface{}{"status": "takeover_failed"}
		if result {
			resp = map[string]interface{}{"status": "takeover_confirmed"}
		}
		_ = conn.WriteJSON(resp)
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts
}

func TestWaitForTakeover_Confirms(t *testing.T) {
	srv, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}
	srv.upgrader.CheckOrigin = func(_ *http.Request) bool { return true }

	testSrv := newWaitForTakeoverTestServer(t, srv)
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	dialURL := "ws" + strings.TrimPrefix(testSrv.URL, "http")
	conn, _, err := dialer.Dial(dialURL, http.Header{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send a valid session_takeover message
	sendMessage(t, conn, map[string]interface{}{
		"type": "session_takeover",
	})

	msg := readMessage(t, conn, 5*time.Second)
	status, ok := msg["status"].(string)
	if !ok || status != "takeover_confirmed" {
		t.Errorf("expected takeover_confirmed, got %v", msg)
	}
}

func TestWaitForTakeover_ClientDisconnects(t *testing.T) {
	srv, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}
	srv.upgrader.CheckOrigin = func(_ *http.Request) bool { return true }

	testSrv := newWaitForTakeoverTestServer(t, srv)
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	dialURL := "ws" + strings.TrimPrefix(testSrv.URL, "http")
	conn, _, err := dialer.Dial(dialURL, http.Header{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Close the connection immediately without sending any message
	conn.Close()

	// Give the server time to process the close and return from waitForTakeover.
	// If the server side does not return promptly this test will timeout.
	time.Sleep(500 * time.Millisecond)
}

func TestWaitForTakeover_WrongMessageType(t *testing.T) {
	srv, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}
	srv.upgrader.CheckOrigin = func(_ *http.Request) bool { return true }

	testSrv := newWaitForTakeoverTestServer(t, srv)
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	dialURL := "ws" + strings.TrimPrefix(testSrv.URL, "http")
	conn, _, err := dialer.Dial(dialURL, http.Header{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send a different message type (ping is allowed but wrong for takeover)
	sendMessage(t, conn, map[string]interface{}{
		"type": "ping",
	})

	msg := readMessage(t, conn, 5*time.Second)
	status, ok := msg["status"].(string)
	if !ok || status != "takeover_failed" {
		t.Errorf("expected takeover_failed when sending wrong message type, got %v", msg)
	}
}

func TestWaitForTakeover_InvalidJSON(t *testing.T) {
	srv, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}
	srv.upgrader.CheckOrigin = func(_ *http.Request) bool { return true }

	testSrv := newWaitForTakeoverTestServer(t, srv)
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	dialURL := "ws" + strings.TrimPrefix(testSrv.URL, "http")
	conn, _, err := dialer.Dial(dialURL, http.Header{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send invalid JSON
	conn.WriteMessage(websocket.TextMessage, []byte("not json"))

	msg := readMessage(t, conn, 5*time.Second)
	status, ok := msg["status"].(string)
	if !ok || status != "takeover_failed" {
		t.Errorf("expected takeover_failed for invalid JSON, got %v", msg)
	}
}

func TestWaitForTakeover_UnknownMessageType(t *testing.T) {
	srv, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}
	srv.upgrader.CheckOrigin = func(_ *http.Request) bool { return true }

	testSrv := newWaitForTakeoverTestServer(t, srv)
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	dialURL := "ws" + strings.TrimPrefix(testSrv.URL, "http")
	conn, _, err := dialer.Dial(dialURL, http.Header{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send an unknown/invalid message type
	sendMessage(t, conn, map[string]interface{}{
		"type": "unknown_type",
	})

	msg := readMessage(t, conn, 5*time.Second)
	status, ok := msg["status"].(string)
	if !ok || status != "takeover_failed" {
		t.Errorf("expected takeover_failed for unknown message type, got %v", msg)
	}
}

// ---------------------------------------------------------------------------
// Tests: Outbound / Inbound message type registry
// ---------------------------------------------------------------------------

func TestSP046_OutboundMessageTypesRegistered(t *testing.T) {
	// Verify that session_conflict and session_displaced are in the
	// allowed outbound message types registry.
	for _, msgType := range []string{"session_conflict", "session_displaced"} {
		if !validateOutboundMessageType(msgType) {
			t.Errorf("expected %q to be a valid outbound message type (SP-046)", msgType)
		}
	}
}

func TestSP046_InboundMessageTypeRegistered(t *testing.T) {
	// Verify that session_takeover is in the allowed inbound message types.
	if !allowedMessageTypes[AllowedMessageTypeSessionTakeover] {
		t.Error("expected session_takeover to be in allowedMessageTypes (SP-046)")
	}

	// Also verify via the WebSocketMessage validation path
	msg := &WebSocketMessage{Type: "session_takeover"}
	if err := msg.Validate(); err != nil {
		t.Errorf("session_takeover should be a valid message type: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: Additional edge cases
// ---------------------------------------------------------------------------

// TestSessionConflict_CleanupAfterDisconnect verifies that when a connection
// disconnects, its entry is removed from activeWSByUserID.
func TestSessionConflict_CleanupAfterDisconnect(t *testing.T) {
	ts := newTestServer(t, nil)
	conn := ts.dialConn(t, "cleanup-test")

	// Consume the connection_status message
	expectType(t, conn, 5*time.Second, "connection_status")

	// Verify it's tracked
	if _, loaded := ts.srv.activeWSByUserID.Load("cleanup-test"); !loaded {
		t.Fatal("expected connection to be tracked before disconnect")
	}

	// Close the connection
	conn.Close()

	// Give the handler goroutine time to clean up
	time.Sleep(300 * time.Millisecond)

	// Should be removed from activeWSByUserID
	if _, loaded := ts.srv.activeWSByUserID.Load("cleanup-test"); loaded {
		t.Error("expected connection to be removed from activeWSByUserID after disconnect")
	}
}

// TestSessionConflict_MultipleTakeovers tests a rapid succession of
// takeover events: client1 -> client2 -> client3 all with the same
// tracking key. Only the latest should be active.
func TestSessionConflict_MultipleTakeovers(t *testing.T) {
	ts := newTestServer(t, nil)

	// Client 1 connects
	conn1 := ts.dialConn(t, "multi-client")
	defer conn1.Close()
	expectType(t, conn1, 5*time.Second, "connection_status")

	// Client 2 connects, gets conflict, takes over
	conn2 := ts.dialConn(t, "multi-client")
	defer conn2.Close()
	expectType(t, conn2, 5*time.Second, "session_conflict")
	sendMessage(t, conn2, map[string]interface{}{"type": "session_takeover"})
	expectType(t, conn1, 5*time.Second, "session_displaced")
	msg2 := expectType(t, conn2, 5*time.Second, "connection_status")

	// Client 3 connects, gets conflict, takes over
	conn3 := ts.dialConn(t, "multi-client")
	defer conn3.Close()
	expectType(t, conn3, 5*time.Second, "session_conflict")
	sendMessage(t, conn3, map[string]interface{}{"type": "session_takeover"})
	expectType(t, conn2, 5*time.Second, "session_displaced")
	msg3 := expectType(t, conn3, 5*time.Second, "connection_status")

	// Only client3 should be active (verified by session ID match)
	sid3 := msg3["data"].(map[string]interface{})["session_id"].(string)
	val, loaded := ts.srv.activeWSByUserID.Load("multi-client")
	if !loaded {
		t.Fatal("expected multi-client to be tracked")
	}
	active, ok := val.(*activeWSConn)
	if !ok {
		t.Fatal("expected *activeWSConn from activeWSByUserID")
	}
	if active.sessionID != sid3 {
		t.Errorf("expected active session to be conn3's session: active=%q, conn3=%q", active.sessionID, sid3)
	}

	// Session IDs should all be different
	sid2 := msg2["data"].(map[string]interface{})["session_id"].(string)
	if sid2 == sid3 {
		t.Error("expected different session IDs for different connections")
	}

	// Close remaining connections
	conn1.Close()
	conn2.Close()
}

// TestSessionConflict_ActiveConnClosedChannel verifies that the closed channel
// in activeWSConn is properly closed when the connection ends.
func TestSessionConflict_ActiveConnClosedChannel(t *testing.T) {
	ts := newTestServer(t, nil)
	conn := ts.dialConn(t, "closed-ch-test")

	expectType(t, conn, 5*time.Second, "connection_status")

	// Get the activeWSConn
	val, loaded := ts.srv.activeWSByUserID.Load("closed-ch-test")
	if !loaded {
		t.Fatal("expected connection to be tracked")
	}
	active, ok := val.(*activeWSConn)
	if !ok {
		t.Fatal("expected *activeWSConn")
	}

	// The closed channel should NOT be closed yet
	select {
	case <-active.closed:
		t.Error("expected closed channel to NOT be closed while connection is active")
	default:
		// Good — channel is open
	}

	// Close the connection
	conn.Close()
	time.Sleep(300 * time.Millisecond)

	// The closed channel SHOULD be closed now
	select {
	case <-active.closed:
		// Good — channel was closed
	default:
		t.Error("expected closed channel to be closed after connection ends")
	}
}

// TestSessionConflict_SecondConnection_RefusesTakeover verifies that when
// a second connection receives session_conflict but sends a non-takeover
// message (which fails in waitForTakeover because only session_takeover is
// accepted), the first connection remains active.
func TestSessionConflict_SecondConnection_RefusesTakeover(t *testing.T) {
	ts := newTestServer(t, nil)

	conn1 := ts.dialConn(t, "refuse-client")
	defer conn1.Close()
	msg1 := expectType(t, conn1, 5*time.Second, "connection_status")
	sessionID1 := msg1["data"].(map[string]interface{})["session_id"].(string)

	// Second connection gets conflict
	conn2 := ts.dialConn(t, "refuse-client")
	expectType(t, conn2, 5*time.Second, "session_conflict")

	// Refuse takeover by sending a different message type
	sendMessage(t, conn2, map[string]interface{}{"type": "ping"})

	// Give time for the server to process
	time.Sleep(500 * time.Millisecond)

	// First connection should still be active
	val, loaded := ts.srv.activeWSByUserID.Load("refuse-client")
	if !loaded {
		t.Fatal("expected first connection to still be active")
	}
	active, ok := val.(*activeWSConn)
	if !ok || active.sessionID != sessionID1 {
		t.Errorf("expected original session to remain active, got %q", active.sessionID)
	}

	conn2.Close()
}

// TestSessionConflict_DefaultClientID tests that the default client ID
// is used as the tracking key when no client ID is provided.
func TestSessionConflict_DefaultClientID(t *testing.T) {
	ts := newTestServer(t, nil)

	// Connect without client ID header — should use defaultWebClientID
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	conn1, _, err := dialer.Dial(ts.wsDialURL(), http.Header{})
	if err != nil {
		t.Fatalf("dial conn1: %v", err)
	}
	defer conn1.Close()
	expectType(t, conn1, 5*time.Second, "connection_status")

	// Second connection also without client ID — should conflict
	conn2, _, err := dialer.Dial(ts.wsDialURL(), http.Header{})
	if err != nil {
		t.Fatalf("dial conn2: %v", err)
	}
	defer conn2.Close()
	expectType(t, conn2, 5*time.Second, "session_conflict")

	// Verify the tracking key is defaultWebClientID
	if _, loaded := ts.srv.activeWSByUserID.Load(defaultWebClientID); !loaded {
		t.Error("expected defaultWebClientID to be the tracking key")
	}
}

// TestSessionConflict_DisplacementData verifies the exact shape of the
// session_displaced message sent to the evicted connection.
func TestSessionConflict_DisplacementData(t *testing.T) {
	ts := newTestServer(t, nil)
	conn1 := ts.dialConn(t, "displacement-test")
	defer conn1.Close()
	expectType(t, conn1, 5*time.Second, "connection_status")

	conn2 := ts.dialConn(t, "displacement-test")
	defer conn2.Close()
	expectType(t, conn2, 5*time.Second, "session_conflict")

	sendMessage(t, conn2, map[string]interface{}{"type": "session_takeover"})

	msg := expectType(t, conn1, 5*time.Second, "session_displaced")
	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		t.Fatal("expected data as map in session_displaced")
	}

	// Verify reason
	if data["reason"] != "session_taken_over" {
		t.Errorf("expected reason=session_taken_over, got %q", data["reason"])
	}

	// Verify message contains informative text
	msgText, ok := data["message"].(string)
	if !ok {
		t.Fatal("expected message as string in session_displaced data")
	}
	if !strings.Contains(strings.ToLower(msgText), "session") {
		t.Errorf("expected displacement message to mention session, got %q", msgText)
	}
}

// TestActiveWSConnFields verifies the activeWSConn struct has the expected
// fields and they are populated correctly.
func TestActiveWSConnFields(t *testing.T) {
	ts := newTestServer(t, nil)
	conn := ts.dialConn(t, "field-test")
	defer conn.Close()
	expectType(t, conn, 5*time.Second, "connection_status")

	val, loaded := ts.srv.activeWSByUserID.Load("field-test")
	if !loaded {
		t.Fatal("expected connection to be tracked")
	}
	active, ok := val.(*activeWSConn)
	if !ok {
		t.Fatal("expected *activeWSConn")
	}

	if active.sessionID == "" {
		t.Error("expected non-empty sessionID")
	}
	if active.connectedAt.IsZero() {
		t.Error("expected non-zero connectedAt")
	}
	if active.safeConn == nil {
		t.Error("expected non-nil safeConn")
	}
	if active.conn == nil {
		t.Error("expected non-nil conn")
	}
	if active.closed == nil {
		t.Error("expected non-nil closed channel")
	}
}

// TestSessionConflict_TakeoverWithReconnect tests that after a takeover,
// the evicted client can reconnect and establish a new session.
func TestSessionConflict_TakeoverWithReconnect(t *testing.T) {
	ts := newTestServer(t, nil)

	conn1 := ts.dialConn(t, "reconnect-client")
	defer conn1.Close()
	expectType(t, conn1, 5*time.Second, "connection_status")

	// Client 2 takes over
	conn2 := ts.dialConn(t, "reconnect-client")
	defer conn2.Close()
	expectType(t, conn2, 5*time.Second, "session_conflict")
	sendMessage(t, conn2, map[string]interface{}{"type": "session_takeover"})
	expectType(t, conn1, 5*time.Second, "session_displaced")
	expectType(t, conn2, 5*time.Second, "connection_status")

	// Client 1 reconnects
	conn3 := ts.dialConn(t, "reconnect-client")
	defer conn3.Close()

	// Should get session_conflict again because conn2 is still active
	expectType(t, conn3, 5*time.Second, "session_conflict")

	// Confirm takeover from the new connection
	sendMessage(t, conn3, map[string]interface{}{"type": "session_takeover"})

	// conn2 should be displaced now
	expectType(t, conn2, 5*time.Second, "session_displaced")
	expectType(t, conn3, 5*time.Second, "connection_status")
}

// TestSessionConflict_SessionConflictData verifies the structure of
// the session_conflict message sent to the new (conflicting) connection.
func TestSessionConflict_SessionConflictData(t *testing.T) {
	ts := newTestServer(t, nil)

	conn1 := ts.dialConn(t, "conflict-data-test")
	defer conn1.Close()
	msg1 := expectType(t, conn1, 5*time.Second, "connection_status")
	existingSessionID := msg1["data"].(map[string]interface{})["session_id"].(string)

	conn2 := ts.dialConn(t, "conflict-data-test")
	defer conn2.Close()
	msg2 := expectType(t, conn2, 5*time.Second, "session_conflict")
	data := msg2["data"].(map[string]interface{})

	// Verify existing_session_id matches the original connection
	if data["existing_session_id"] != existingSessionID {
		t.Errorf("expected existing_session_id=%q, got %q", existingSessionID, data["existing_session_id"])
	}

	// Verify connected_at is a positive Unix timestamp
	tsVal, ok := data["connected_at"].(float64)
	if !ok {
		t.Error("expected connected_at as float64 (JSON number)")
	} else if tsVal <= 0 {
		t.Errorf("expected positive connected_at, got %f", tsVal)
	}
}

// TestSessionConflict_SessionTakeoverOutsideConflict tests that
// session_takeover received during normal message dispatch (outside of
// the conflict wait loop) is logged and ignored.
func TestSessionConflict_SessionTakeoverOutsideConflict(t *testing.T) {
	ts := newTestServer(t, nil)
	conn := ts.dialConn(t, "outlier-test")
	defer conn.Close()
	expectType(t, conn, 5*time.Second, "connection_status")

	// Send session_takeover during normal operation (not during conflict)
	sendMessage(t, conn, map[string]interface{}{
		"type": "session_takeover",
	})

	// The connection should remain active and undisturbed.
	// Give a small delay for the handler to process the message.
	time.Sleep(200 * time.Millisecond)

	val, loaded := ts.srv.activeWSByUserID.Load("outlier-test")
	if !loaded {
		t.Fatal("expected connection to still be active")
	}
	if _, ok := val.(*activeWSConn); !ok {
		t.Error("expected *activeWSConn in activeWSByUserID after spurious session_takeover")
	}
}
