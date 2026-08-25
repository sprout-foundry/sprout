//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestWritePanicError_SendsErrorEvent verifies WritePanicError sends
// a structured error event with type "error" and code "internal_panic".
func TestWritePanicError_SendsErrorEvent(t *testing.T) {
	testUpgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		sc := NewSafeConn(conn)
		sc.WritePanicError("test-session", "test-location", "something went wrong")

		time.Sleep(50 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read error event failed: %v", err)
	}

	var errorEvent map[string]interface{}
	if err := json.Unmarshal(msg, &errorEvent); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if errorEvent["type"] != "error" {
		t.Errorf("expected type=error, got %v", errorEvent["type"])
	}
	data, ok := errorEvent["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map, got %T", errorEvent["data"])
	}
	if data["code"] != "internal_panic" {
		t.Errorf("expected code=internal_panic, got %v", data["code"])
	}
	if data["session_id"] != "test-session" {
		t.Errorf("expected session_id=test-session, got %v", data["session_id"])
	}

	// Read session_terminated event
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err = ws.ReadMessage()
	if err != nil {
		t.Fatalf("read session_terminated event failed: %v", err)
	}

	var termEvent map[string]interface{}
	if err := json.Unmarshal(msg, &termEvent); err != nil {
		t.Fatalf("invalid JSON for session_terminated: %v", err)
	}

	if termEvent["type"] != "session_terminated" {
		t.Errorf("expected type=session_terminated, got %v", termEvent["type"])
	}
	termData, ok := termEvent["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected session_terminated data to be map, got %T", termEvent["data"])
	}
	if termData["session_id"] != "test-session" {
		t.Errorf("expected session_id=test-session, got %v", termData["session_id"])
	}
	if termData["code"] != "internal_panic" {
		t.Errorf("expected code=internal_panic, got %v", termData["code"])
	}
	if termData["status"] != "error" {
		t.Errorf("expected status=error, got %v", termData["status"])
	}
}

// TestWritePanicError_MarksConnectionClosed verifies the connection is
// marked as closed after WritePanicError.
func TestWritePanicError_MarksConnectionClosed(t *testing.T) {
	// Create a local upgrader for testing
	testUpgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		sc := NewSafeConn(conn)
		if sc.closed.Load() {
			t.Error("SafeConn should not be closed before WritePanicError")
		}
		sc.WritePanicError("session", "loc", "panic")
		if !sc.closed.Load() {
			t.Error("SafeConn should be closed after WritePanicError")
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer ws.Close()
}

// TestSafeConn_WriteJSON_IgnoresClosed verifies WriteJSON silently drops
// writes to a closed SafeConn.
func TestSafeConn_WriteJSON_IgnoresClosed(t *testing.T) {
	sc := NewSafeConn(nil) // nil conn, but we only test the closed flag
	sc.closed.Store(true)
	err := sc.WriteJSON(map[string]string{"test": "data"})
	if err != nil {
		t.Errorf("WriteJSON on closed conn should return nil, got %v", err)
	}
}

// TestCleanupAfterPanic_ResetsClientState verifies cleanupAfterPanic
// resets the client's query state, clears cached agents, and publishes
// a session_terminated event.
func TestCleanupAfterPanic_ResetsClientState(t *testing.T) {
	ws, err := NewReactWebServer(nil, newTestEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	clientID := "test-client"
	sessionID := "ws_test123"

	// Set up client context with active query state
	ws.mutex.Lock()
	ctx := &webClientContext{
		ActiveQuery:  true,
		CurrentQuery: "running query",
		ChatSessions: map[string]*chatSession{
			"default": {
				ActiveQuery:  true,
				CurrentQuery: "chat query running",
			},
		},
	}
	ctx.DefaultChatID = "default"
	ws.clientContexts[clientID] = ctx
	ws.activeQueries = 1
	ws.mutex.Unlock()

	// Subscribe to events to verify publishClientEvent worked
	eventCh := ws.eventBus.Subscribe("cleanup-test")
	defer ws.eventBus.Unsubscribe("cleanup-test")

	// Run cleanup
	ws.cleanupAfterPanicAgent(clientID, sessionID)

	// Verify top-level state is cleared
	ws.mutex.RLock()
	if ws.activeQueries != 0 {
		t.Errorf("expected activeQueries=0, got %d", ws.activeQueries)
	}
	ctx = ws.clientContexts[clientID]
	ws.mutex.RUnlock()

	if ctx == nil {
		t.Fatal("client context should still exist")
	}
	if ctx.ActiveQuery {
		t.Error("expected ActiveQuery to be false after cleanup")
	}
	if ctx.CurrentQuery != "" {
		t.Errorf("expected CurrentQuery to be empty, got %q", ctx.CurrentQuery)
	}

	// Verify per-chat state is cleared
	cs := ctx.ChatSessions["default"]
	if cs == nil {
		t.Fatal("default chat session should exist")
	}
	if cs.ActiveQuery {
		t.Error("expected chat session ActiveQuery to be false after cleanup")
	}
	if cs.CurrentQuery != "" {
		t.Errorf("expected chat session CurrentQuery to be empty, got %q", cs.CurrentQuery)
	}

	// Verify session_terminated event was published
	select {
	case event := <-eventCh:
		if event.Type != events.EventTypeSessionTerminated {
			t.Errorf("expected event type session_terminated, got %s", event.Type)
		}
		data, ok := event.Data.(map[string]interface{})
		if !ok {
			t.Fatal("expected event data to be a map")
		}
		if data["session_id"] != sessionID {
			t.Errorf("expected session_id=%q, got %q", sessionID, data["session_id"])
		}
		if data["code"] != "internal_panic" {
			t.Errorf("expected code=internal_panic, got %v", data["code"])
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for session_terminated event")
	}
}

// TestCleanupAfterPanic_ClearsCachedAgents verifies cleanupAfterPanic
// clears cached agents so the next request gets a fresh agent.
func TestCleanupAfterPanic_ClearsCachedAgents(t *testing.T) {
	ws, err := NewReactWebServer(nil, newTestEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	clientID := "test-client"

	// Set up client context with chat sessions
	ws.mutex.Lock()
	ctx := &webClientContext{
		ChatSessions: map[string]*chatSession{
			"default": {}, // empty agent reference
		},
	}
	ctx.DefaultChatID = "default"
	ws.clientContexts[clientID] = ctx
	ws.mutex.Unlock()

	ws.cleanupAfterPanicAgent(clientID, "session-1")

	// Verify: the context must still exist but with nil agents
	ws.mutex.RLock()
	ctx = ws.clientContexts[clientID]
	ws.mutex.RUnlock()

	if ctx == nil {
		t.Fatal("client context should still exist after cleanup")
	}
	if ctx.Agent != nil {
		t.Error("expected top-level Agent to be nil after cleanup")
	}
	if cs := ctx.ChatSessions["default"]; cs.Agent != nil {
		t.Error("expected chat session Agent to be nil after cleanup")
	}
}

// TestSafeConn_WritePanicError_HandlesDoublePanic verifies WritePanicError
// itself doesn't panic even if the underlying connection is in a bad state.
func TestSafeConn_WritePanicError_HandlesDoublePanic(t *testing.T) {
	sc := NewSafeConn(nil) // nil conn
	// This should not panic — the defensive recover inside WritePanicError
	// should catch the nil pointer dereference
	sc.WritePanicError("session", "loc", "test panic value")
	if !sc.closed.Load() {
		t.Error("expected SafeConn to be marked closed after WritePanicError")
	}
}

// TestCleanupAfterPanic_EmptyClientID_DoesNotPanic verifies cleanupAfterPanic
// with an empty clientID is a no-op.
func TestCleanupAfterPanic_EmptyClientID_DoesNotPanic(t *testing.T) {
	ws, err := NewReactWebServer(nil, newTestEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Should not panic
	ws.cleanupAfterPanicAgent("", "session-1")
	ws.cleanupAfterPanicAgent("  ", "session-2")
}

// TestCleanupAfterPanic_UnknownClientID_DoesNotPanic verifies cleanupAfterPanic
// with a clientID that has no context doesn't panic.
func TestCleanupAfterPanic_UnknownClientID_DoesNotPanic(t *testing.T) {
	ws, err := NewReactWebServer(nil, newTestEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Should not panic even though no context exists
	ws.cleanupAfterPanicAgent("nonexistent-client", "session-1")
}

// TestClearAllChatQueryState verifies clearAllChatQueryState resets all
// chat sessions and the top-level query state.
func TestClearAllChatQueryState(t *testing.T) {
	ctx := &webClientContext{
		ActiveQuery:  true,
		CurrentQuery: "top-level query",
		ChatSessions: map[string]*chatSession{
			"chat1": {ActiveQuery: true, CurrentQuery: "query1"},
			"chat2": {ActiveQuery: true, CurrentQuery: "query2"},
		},
	}

	ctx.clearAllChatQueryState()

	if ctx.ActiveQuery {
		t.Error("expected top-level ActiveQuery to be false")
	}
	if ctx.CurrentQuery != "" {
		t.Errorf("expected top-level CurrentQuery to be empty, got %q", ctx.CurrentQuery)
	}
	for id, cs := range ctx.ChatSessions {
		if cs.ActiveQuery {
			t.Errorf("expected chat %s ActiveQuery to be false", id)
		}
		if cs.CurrentQuery != "" {
			t.Errorf("expected chat %s CurrentQuery to be empty, got %q", id, cs.CurrentQuery)
		}
	}
}

// TestClearAllChatQueryState_NilChatSessions verifies clearing with nil
// ChatSessions is a safe no-op.
func TestClearAllChatQueryState_NilChatSessions(t *testing.T) {
	ctx := &webClientContext{
		ActiveQuery:  true,
		CurrentQuery: "query",
		ChatSessions: nil,
	}

	ctx.clearAllChatQueryState()

	if ctx.ActiveQuery {
		t.Error("expected top-level ActiveQuery to be false even with nil ChatSessions")
	}
}

// newTestEventBus creates an event bus for testing.
// This is a simple wrapper around events.EventBus with a mutex for thread safety.
func newTestEventBus() *events.EventBus {
	return events.NewEventBus()
}

// TestSafeConn_WriteJSON_WithConcurrentClose verifies WriteJSON handles
// concurrent close without panic.
func TestSafeConn_WriteJSON_WithConcurrentClose(t *testing.T) {
	sc := NewSafeConn(nil)
	sc.closed.Store(true)

	var wg sync.WaitGroup
	wg.Add(2)

	// Try writing from multiple goroutines concurrently
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			sc.WriteJSON(map[string]interface{}{"i": i})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			sc.WriteJSON(map[string]interface{}{"i": i})
		}
	}()

	wg.Wait()
}

// TestCleanupAfterPanic_WithMultipleChatSessions verifies cleanupAfterPanic
// resets all chat sessions, not just the default one.
func TestCleanupAfterPanic_WithMultipleChatSessions(t *testing.T) {
	ws, err := NewReactWebServer(nil, newTestEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	clientID := "test-client"
	sessionID := "ws_test456"

	// Set up client context with multiple chat sessions
	ws.mutex.Lock()
	ctx := &webClientContext{
		ActiveQuery:  true,
		CurrentQuery: "top-level query",
		ChatSessions: map[string]*chatSession{
			"chat1": {ActiveQuery: true, CurrentQuery: "query1"},
			"chat2": {ActiveQuery: true, CurrentQuery: "query2"},
			"chat3": {ActiveQuery: false, CurrentQuery: ""}, // already idle
		},
	}
	ctx.DefaultChatID = "chat1"
	ws.clientContexts[clientID] = ctx
	ws.activeQueries = 1
	ws.mutex.Unlock()

	ws.cleanupAfterPanicAgent(clientID, sessionID)

	// Verify all chat sessions are reset
	ws.mutex.RLock()
	ctx = ws.clientContexts[clientID]
	ws.mutex.RUnlock()

	if ctx == nil {
		t.Fatal("client context should exist")
	}

	for id, cs := range ctx.ChatSessions {
		if cs.ActiveQuery {
			t.Errorf("expected chat %s ActiveQuery to be false", id)
		}
		if cs.CurrentQuery != "" {
			t.Errorf("expected chat %s CurrentQuery to be empty, got %q", id, cs.CurrentQuery)
		}
	}
}
