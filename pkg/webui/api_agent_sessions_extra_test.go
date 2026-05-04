package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// handleAgentSessionKill
// ---------------------------------------------------------------------------

func TestHandleAgentSessionKill_MethodNotAllowed(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-kill-method", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.Hidden = true
	session.mutex.Unlock()

	// Use GET instead of POST.
	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions/bg-kill-method/kill", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET on /kill, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAgentSessionKill_NotFound(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/nonexistent/kill", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent session, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAgentSessionKill_NotBackground(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("regular-kill-test", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = false // not a background session
	session.Active = true
	session.Hidden = true
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/regular-kill-test/kill", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-background session, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not a background session") {
		t.Errorf("expected 'not a background session' in body, got %s", rec.Body.String())
	}
}

func TestHandleAgentSessionKill_Success(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-kill-success", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.Hidden = true
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/bg-kill-success/kill", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["id"] != "bg-kill-success" {
		t.Errorf("expected id 'bg-kill-success', got %v", resp["id"])
	}
	if resp["status"] != "killed" {
		t.Errorf("expected status 'killed', got %v", resp["status"])
	}
}

func TestHandleAgentSessionKill_SetsInactive(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-kill-inactive", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.Hidden = true
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/bg-kill-inactive/kill", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the session is marked inactive
	session.mutex.RLock()
	active := session.Active
	session.mutex.RUnlock()
	if active {
		t.Error("expected session.Active to be false after kill")
	}
}

func TestHandleAgentSessionKill_ContentTypeJSON(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-kill-ct", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.Hidden = true
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/bg-kill-ct/kill", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", contentType)
	}
}

func TestHandleAgentSessionKill_InactiveBackgroundSession(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-kill-inactive-bg", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = false // already inactive
	session.Hidden = true
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/bg-kill-inactive-bg/kill", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	// Even if inactive, kill should still succeed (idempotent kill)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "killed" {
		t.Errorf("expected status 'killed', got %v", resp["status"])
	}
}

func TestHandleAgentSessionKill_SessionRemovedFromHiddenList(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-kill-remove", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.Hidden = true
	session.mutex.Unlock()

	// Verify it's in the hidden sessions before kill
	hiddenBefore := tm.ListHiddenSessions()
	found := false
	for _, id := range hiddenBefore {
		if id == "bg-kill-remove" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("session should be in hidden sessions before kill")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/bg-kill-remove/kill", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify it's removed from hidden sessions after kill
	hiddenAfter := tm.ListHiddenSessions()
	for _, id := range hiddenAfter {
		if id == "bg-kill-remove" {
			t.Error("session should be removed from hidden sessions after kill")
			break
		}
	}
}
