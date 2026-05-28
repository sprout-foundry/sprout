//go:build !js

package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// newTestWebServer creates a ReactWebServer with a terminal manager and a client
// context for "test-client", returning the client's TerminalManager for test setup.
func newTestWebServer(t *testing.T) (*ReactWebServer, *TerminalManager) {
	t.Helper()
	daemonRoot := t.TempDir()

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot
	ws.workspaceRoot = daemonRoot

	// Workspace must be within daemonRoot to pass the security check in setClientWorkspaceRoot.
	workspaceDir := daemonRoot // daemonRoot itself is always allowed
	if _, err := ws.setClientWorkspaceRoot("test-client", workspaceDir); err != nil {
		t.Fatalf("setClientWorkspaceRoot: %v", err)
	}

	// After setClientWorkspaceRoot, the client context has its own TerminalManager.
	// Get it via the request-based accessor so we create sessions on the right manager.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	tm := ws.getTerminalManagerForRequest(req)
	return ws, tm
}

// ---------------------------------------------------------------------------
// handleAPIAgentSessions — GET /api/terminal/agent-sessions
// ---------------------------------------------------------------------------

func TestHandleAPIAgentSessions_MethodNotAllowed(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessions(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAgentSessions_EmptyList(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Sessions []map[string]interface{} `json:"sessions"`
		Count    int                      `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Count != 0 {
		t.Errorf("expected count 0, got %d", resp.Count)
	}
	if len(resp.Sessions) != 0 {
		t.Errorf("expected empty sessions, got %d", len(resp.Sessions))
	}
}

func TestHandleAPIAgentSessions_ExcludesNonBackgroundSessions(t *testing.T) {
	ws, tm := newTestWebServer(t)

	// Create a regular hidden session (not background).
	_, err := tm.CreateHiddenSession("regular-hidden", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	// Create a background session by manually marking a hidden session.
	bgSession, err := tm.CreateHiddenSession("bg-test-1", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	bgSession.mutex.Lock()
	bgSession.IsBackground = true
	bgSession.Name = "echo hello"
	bgSession.Active = true
	bgSession.mutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Sessions []map[string]interface{} `json:"sessions"`
		Count    int                      `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("expected 1 background session, got %d", resp.Count)
	}
	if resp.Sessions[0]["id"] != "bg-test-1" {
		t.Errorf("expected session id 'bg-test-1', got %v", resp.Sessions[0]["id"])
	}
}

func TestHandleAPIAgentSessions_ResponseFields(t *testing.T) {
	ws, tm := newTestWebServer(t)

	// Create a background session with known fields.
	session, err := tm.CreateHiddenSession("bg-fields-test", "agent", "chat-abc")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Name = "npm install --save"
	session.Active = true
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Sessions []map[string]interface{} `json:"sessions"`
		Count    int                      `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("expected 1 session, got %d", resp.Count)
	}
	s := resp.Sessions[0]

	if s["id"] != "bg-fields-test" {
		t.Errorf("expected id 'bg-fields-test', got %v", s["id"])
	}
	if s["name"] != "npm install --save" {
		t.Errorf("expected name 'npm install --save', got %v", s["name"])
	}
	if s["status"] != "active" {
		t.Errorf("expected status 'active', got %v", s["status"])
	}
	if s["chat_id"] != "chat-abc" {
		t.Errorf("expected chat_id 'chat-abc', got %v", s["chat_id"])
	}
	if _, ok := s["output_preview"]; !ok {
		t.Error("expected output_preview field to be present")
	}
}

func TestHandleAPIAgentSessions_InactiveStatus(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-inactive", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = false
	session.Name = "done cmd"
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Sessions []map[string]interface{} `json:"sessions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Sessions[0]["status"] != "inactive" {
		t.Errorf("expected status 'inactive', got %v", resp.Sessions[0]["status"])
	}
}

func TestHandleAPIAgentSessions_OutputPreviewMax500Bytes(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-preview", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	// Write more than 500 bytes to the ring buffer.
	longOutput := strings.Repeat("A", 800) + "TAIL"
	session.broadcast([]byte(longOutput))

	session.mutex.Lock()
	session.IsBackground = true
	session.Name = "big output"
	session.Active = true
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Sessions []map[string]interface{} `json:"sessions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	preview := resp.Sessions[0]["output_preview"].(string)

	// Preview should be truncated to last 500 bytes (800 A's + TAIL = 804, last 500 = 496 A's + TAIL).
	expected := strings.Repeat("A", 496) + "TAIL"
	if preview != expected {
		t.Errorf("preview mismatch:\nexpected length %d, got length %d\nexpected: %q\n  got: %q",
			len(expected), len(preview), expected[:50]+"...", preview[:50]+"...")
	}
}

func TestHandleAPIAgentSessions_OutputPreviewShortContent(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-short-preview", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	// Write less than 500 bytes — should return all.
	session.broadcast([]byte("short output"))

	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.Name = "short"
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Sessions []map[string]interface{} `json:"sessions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Sessions[0]["output_preview"].(string) != "short output" {
		t.Errorf("expected full output for short content, got %q", resp.Sessions[0]["output_preview"])
	}
}

func TestHandleAPIAgentSessions_EmptyOutputPreview(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-no-output", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.Name = "no output yet"
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Sessions []map[string]interface{} `json:"sessions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Sessions[0]["output_preview"] != "" {
		t.Errorf("expected empty output_preview, got %q", resp.Sessions[0]["output_preview"])
	}
}

func TestHandleAPIAgentSessions_ContentTypeJSON(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessions(rec, req)

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", contentType)
	}
}

// ---------------------------------------------------------------------------
// Multiple background sessions
// ---------------------------------------------------------------------------

func TestHandleAPIAgentSessions_MultipleBackgroundSessions(t *testing.T) {
	ws, tm := newTestWebServer(t)

	// Create 3 background sessions with different properties.
	for i, chat := range []string{"chat-1", "chat-2", "chat-3"} {
		session, err := tm.CreateHiddenSession("bg-multi-"+chat, "agent", chat)
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
		}
		session.mutex.Lock()
		session.IsBackground = true
		session.Name = "cmd-" + chat
		session.Active = (i%2 == 0) // alternate active/inactive
		session.mutex.Unlock()
	}

	// Create one regular hidden session that should NOT appear.
	regular, err := tm.CreateHiddenSession("regular-only", "agent", "chat-99")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	regular.mutex.Lock()
	regular.IsBackground = false
	regular.Active = true
	regular.mutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Sessions []map[string]interface{} `json:"sessions"`
		Count    int                      `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Count != 3 {
		t.Fatalf("expected 3 background sessions, got %d", resp.Count)
	}

	// Verify no regular hidden sessions leaked.
	for _, s := range resp.Sessions {
		if s["id"] == "regular-only" {
			t.Error("regular hidden session should NOT appear in background sessions list")
		}
	}

	// Verify active/inactive statuses.
	for _, s := range resp.Sessions {
		id := s["id"].(string)
		if strings.Contains(id, "chat-1") || strings.Contains(id, "chat-3") {
			if s["status"] != "active" {
				t.Errorf("expected active status for %s, got %v", id, s["status"])
			}
		} else if strings.Contains(id, "chat-2") {
			if s["status"] != "inactive" {
				t.Errorf("expected inactive status for %s, got %v", id, s["status"])
			}
		}
	}
}

// ---------------------------------------------------------------------------
// handleAPIAgentSessionActions — dispatch
// ---------------------------------------------------------------------------

func TestHandleAPIAgentSessionActions_InvalidPath(t *testing.T) {
	ws, _ := newTestWebServer(t)

	cases := []struct {
		path string
		desc string
	}{
		{"/api/terminal/agent-sessions/", "empty path (no ID or action)"},
		{"/api/terminal/agent-sessions/only-id", "missing action"},
		{"/api/terminal/agent-sessions/id/extra/deep", "too many path segments"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set(webClientIDHeader, "test-client")
			rec := httptest.NewRecorder()
			ws.handleAPIAgentSessionActions(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for %s, got %d: %s", tc.desc, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandleAPIAgentSessionActions_EmptyIDOrAction(t *testing.T) {
	ws, _ := newTestWebServer(t)

	// Empty session ID (whitespace only after trim) - URL-encode the space.
	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions/%20/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty ID, got %d: %s", rec.Code, rec.Body.String())
	}

	// Empty action (whitespace only after trim) - URL-encode the space.
	req = httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions/some-id/%20", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty action, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAgentSessionActions_UnknownAction(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions/some-id/invalid", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown action, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Unknown action: invalid") {
		t.Errorf("expected 'Unknown action: invalid' in body, got %s", rec.Body.String())
	}
}

func TestHandleAPIAgentSessionActions_SessionIDTooLong(t *testing.T) {
	ws, _ := newTestWebServer(t)

	longID := strings.Repeat("x", maxSessionIDLength+1)
	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions/"+longID+"/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for oversized session ID, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "too long") {
		t.Errorf("expected 'too long' in body, got %s", rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// handleAgentSessionOutput — GET /api/terminal/agent-sessions/{id}/output
// ---------------------------------------------------------------------------

func TestHandleAgentSessionOutput_MethodNotAllowed(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-output-test", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/bg-output-test/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST on /output, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAgentSessionOutput_NotFound(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions/nonexistent/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent session, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAgentSessionOutput_NotBackground(t *testing.T) {
	ws, tm := newTestWebServer(t)

	// Create a regular hidden session (not background).
	_, err := tm.CreateHiddenSession("regular-hidden-out", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions/regular-hidden-out/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-background session, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAgentSessionOutput_ReturnsPlainText(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-output-plain", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	// Write known output to the ring buffer.
	testOutput := "line1\nline2\nline3\n"
	session.broadcast([]byte(testOutput))

	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions/bg-output-plain/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("expected text/plain content type, got %q", contentType)
	}

	// Output should contain the test lines.
	body := rec.Body.String()
	if !strings.Contains(body, "line1") || !strings.Contains(body, "line2") || !strings.Contains(body, "line3") {
		t.Errorf("output missing expected lines: %q", body)
	}
}

func TestHandleAgentSessionOutput_WhitespaceTrimmed(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-output-trim", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.broadcast([]byte("trimmed output"))
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.mutex.Unlock()

	// URL-encode leading/trailing spaces in the session ID segment — handler trims whitespace.
	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions/%20%20bg-output-trim%20%20/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for trimmed session ID, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAgentSessionOutput_ContentTypePlainText(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-content-type", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.broadcast([]byte("test output"))
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions/bg-content-type/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	contentType := rec.Header().Get("Content-Type")
	if contentType != "text/plain; charset=utf-8" {
		t.Errorf("expected Content-Type 'text/plain; charset=utf-8', got %q", contentType)
	}
}

func TestHandleAgentSessionOutput_StripsANSI(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-ansi-strip", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	// Write ANSI-colored output.
	ansiOutput := "\033[32mgreen text\033[0m normal text\n"
	session.broadcast([]byte(ansiOutput))

	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions/bg-ansi-strip/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	output := rec.Body.String()
	if strings.ContainsRune(output, '\x1b') {
		t.Errorf("output should have ANSI escapes stripped: %q", output)
	}
	if !strings.Contains(output, "green text") {
		t.Errorf("output should still contain readable text: %q", output)
	}
}

// ---------------------------------------------------------------------------
// handleAgentSessionAttach — POST /api/terminal/agent-sessions/{id}/attach
// ---------------------------------------------------------------------------

func TestHandleAgentSessionAttach_MethodNotAllowed(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-attach-method", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.Hidden = true
	session.mutex.Unlock()

	// Use GET instead of POST.
	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions/bg-attach-method/attach", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET on /attach, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAgentSessionAttach_NotFound(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/nonexistent/attach", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent session, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAgentSessionAttach_NotBackground(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("regular-attach-test", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = false // not a background session
	session.Active = true
	session.Hidden = true
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/regular-attach-test/attach", nil)
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

func TestHandleAgentSessionAttach_InactiveSession(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-inactive-attach", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = false // inactive
	session.Hidden = true
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/bg-inactive-attach/attach", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for inactive session, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not active") {
		t.Errorf("expected 'not active' in body, got %s", rec.Body.String())
	}
}

func TestHandleAgentSessionAttach_Success(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-attach-success", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.Hidden = true
	session.mutex.Unlock()

	// Verify it's hidden before attach.
	sessionsBefore := tm.ListSessions()
	for _, id := range sessionsBefore {
		if id == "bg-attach-success" {
			t.Fatal("session should not be in ListSessions() before attach")
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/bg-attach-success/attach", nil)
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
	if resp["id"] != "bg-attach-success" {
		t.Errorf("expected id 'bg-attach-success', got %v", resp["id"])
	}
	if resp["status"] != "attached" {
		t.Errorf("expected status 'attached', got %v", resp["status"])
	}

	// Verify Hidden flag is cleared.
	session.mutex.RLock()
	hidden := session.Hidden
	session.mutex.RUnlock()
	if hidden {
		t.Error("expected session.Hidden to be false after attach")
	}

	// Verify session appears in ListSessions.
	sessionsAfter := tm.ListSessions()
	found := false
	for _, id := range sessionsAfter {
		if id == "bg-attach-success" {
			found = true
			break
		}
	}
	if !found {
		t.Error("session should appear in ListSessions() after attach")
	}
}

func TestHandleAgentSessionAttach_IdempotentAlreadyVisible(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-already-visible", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.Hidden = false // already visible
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/bg-already-visible/attach", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for already-visible session, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "attached" {
		t.Errorf("expected status 'attached' for idempotent call, got %v", resp["status"])
	}
}

func TestHandleAgentSessionAttach_ContentTypeJSON(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-content-attach", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.Hidden = false // already attached for Content-Type test
	session.mutex.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/bg-content-attach/attach", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", contentType)
	}
}

func TestHandleAgentSessionAttach_PostBodyIgnored(t *testing.T) {
	ws, tm := newTestWebServer(t)

	session, err := tm.CreateHiddenSession("bg-attach-body", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	session.mutex.Lock()
	session.IsBackground = true
	session.Active = true
	session.Hidden = true
	session.mutex.Unlock()

	// Send a body with the POST — the handler should ignore it.
	req := httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/bg-attach-body/attach",
		bytes.NewReader([]byte(`{"extra": "data"}`)))
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
	if resp["status"] != "attached" {
		t.Errorf("expected 'attached', got %v", resp["status"])
	}
}

// ---------------------------------------------------------------------------
// Integration: background session lifecycle via ExecuteCommandInBackground
// ---------------------------------------------------------------------------

func TestHandleAPIAgentSessions_WithRealBackgroundCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	ws, tm := newTestWebServer(t)

	sessionID, err := tm.ExecuteCommandInBackground(context.Background(), "chat-bg-integration", "echo bg-integration-test")
	if err != nil {
		t.Skipf("PTY creation failed, skipping: %v", err)
	}

	// Wait briefly for output to appear.
	time.Sleep(500 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAgentSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Sessions []map[string]interface{} `json:"sessions"`
		Count    int                      `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should find at least our background session.
	found := false
	for _, s := range resp.Sessions {
		if s["id"] == sessionID {
			found = true
			if s["chat_id"] != "chat-bg-integration" {
				t.Errorf("expected chat_id 'chat-bg-integration', got %v", s["chat_id"])
			}
			if s["status"] != "active" {
				t.Errorf("expected status 'active', got %v", s["status"])
			}
			break
		}
	}
	if !found {
		t.Errorf("background session %q not found in API response", sessionID)
	}

	// Test output endpoint.
	req = httptest.NewRequest(http.MethodGet, "/api/terminal/agent-sessions/"+sessionID+"/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("output endpoint: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	output := rec.Body.String()
	if !strings.Contains(output, "bg-integration-test") {
		t.Errorf("output should contain 'bg-integration-test', got: %q", output)
	}

	// Test attach endpoint.
	req = httptest.NewRequest(http.MethodPost, "/api/terminal/agent-sessions/"+sessionID+"/attach", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	ws.handleAPIAgentSessionActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("attach endpoint: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var attachResp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&attachResp); err != nil {
		t.Fatalf("failed to decode attach response: %v", err)
	}
	if attachResp["status"] != "attached" {
		t.Errorf("expected status 'attached', got %v", attachResp["status"])
	}

	// Session should now be visible.
	foundVisible := false
	for _, id := range tm.ListSessions() {
		if id == sessionID {
			foundVisible = true
			break
		}
	}
	if !foundVisible {
		t.Error("session should be visible after attach")
	}
}
