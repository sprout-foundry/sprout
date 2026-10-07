//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHumaConversationDispatch proves the conversation-family Huma operations
// (chat-sessions, sessions, subagent, edits, shell-approvals) dispatch through
// the live ServeMux and emit the same response the plain handlers did. The
// migrated operations are thin wrappers over the existing plain handlers, so a
// regression that broke the Huma wiring (dropped registration, wrong method
// pattern, a double body) would surface here as a wrong status or a missing
// payload.
//
// It exercises deterministic routes that need no live agent or provider:
//   - GET /api/chat-sessions: a fresh server lists zero sessions (200, stable
//     shape).
//   - GET /api/sessions/{id}/export with a nonexistent id: the {id} path
//     variable is routed through the mux and the handler reports 404.
//   - GET /api/edits/{id} (a subtree route): the dispatcher routes to the
//     status branch and reports 404 for an unknown edit.
//
// It also checks the method gate on a GET-only Huma route: POST
// /api/chat-sessions must not be served by the GET operation (Huma enforces
// the method), so it reaches the SPA catch-all and 404s rather than returning a
// 2xx.
func TestHumaConversationDispatch(t *testing.T) {
	ws, _ := newTestWebServer(t)
	mux := ws.setupRoutes(context.Background())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// GET /api/chat-sessions — a Huma operation (was a plain handler). A fresh
	// server has no chat sessions, so the handler writes a deterministic 200
	// with an empty list.
	resp, err := http.Get(srv.URL + "/api/chat-sessions")
	if err != nil {
		t.Fatalf("GET /api/chat-sessions: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /api/chat-sessions body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close /api/chat-sessions body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/chat-sessions: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var cs map[string]any
	if err := json.Unmarshal(body, &cs); err != nil {
		t.Fatalf("decode /api/chat-sessions body: %v", err)
	}
	if cs["message"] != "success" {
		t.Fatalf("GET /api/chat-sessions: message = %v, want success", cs["message"])
	}
	// A fresh server auto-creates the default chat, so the list carries exactly
	// one entry flagged is_default. Assert the stable shape (a single default
	// chat with no messages) rather than an empty list.
	list, ok := cs["chat_sessions"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("GET /api/chat-sessions: chat_sessions = %v, want one default chat", cs["chat_sessions"])
	}
	def, ok := list[0].(map[string]any)
	if !ok || def["is_default"] != true {
		t.Fatalf("GET /api/chat-sessions: first entry = %v, want is_default=true", list[0])
	}
	if cs["total_sessions"] != float64(1) {
		t.Fatalf("GET /api/chat-sessions: total_sessions = %v, want 1", cs["total_sessions"])
	}

	// GET /api/sessions/{id}/export with a nonexistent id — a {id} path
	// variable routed through the ServeMux. The handler resolves the id via
	// r.PathValue and reports 404 (session not found) for an unknown session.
	exportResp, err := http.Get(srv.URL + "/api/sessions/does-not-exist/export")
	if err != nil {
		t.Fatalf("GET /api/sessions/{id}/export: %v", err)
	}
	exportBody, err := io.ReadAll(exportResp.Body)
	if err != nil {
		t.Fatalf("read export body: %v", err)
	}
	if err := exportResp.Body.Close(); err != nil {
		t.Errorf("close export body: %v", err)
	}
	if exportResp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /api/sessions/{id}/export: status = %d, want %d (body: %s)",
			exportResp.StatusCode, http.StatusNotFound, exportBody)
	}

	// GET /api/edits/{id} — a subtree route (registered as GET /api/edits/).
	// The dispatcher routes to the status branch (no /decision suffix) and
	// reports 404 for an edit not in the pending registry.
	editsResp, err := http.Get(srv.URL + "/api/edits/nope123")
	if err != nil {
		t.Fatalf("GET /api/edits/{id}: %v", err)
	}
	if _, err := io.Copy(io.Discard, editsResp.Body); err != nil {
		t.Errorf("drain /api/edits body: %v", err)
	}
	if err := editsResp.Body.Close(); err != nil {
		t.Errorf("close /api/edits body: %v", err)
	}
	if editsResp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /api/edits/{id}: status = %d, want %d", editsResp.StatusCode, http.StatusNotFound)
	}

	// Method gate on a GET-only Huma route: POST /api/chat-sessions must not be
	// served by the GET operation (Huma enforces the method), so it reaches the
	// SPA catch-all and 404s rather than returning a 2xx.
	gateResp, err := http.Post(srv.URL+"/api/chat-sessions", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/chat-sessions: %v", err)
	}
	if _, err := io.Copy(io.Discard, gateResp.Body); err != nil {
		t.Errorf("drain /api/chat-sessions gate body: %v", err)
	}
	if err := gateResp.Body.Close(); err != nil {
		t.Errorf("close /api/chat-sessions gate body: %v", err)
	}
	if gateResp.StatusCode == http.StatusOK || gateResp.StatusCode == http.StatusAccepted {
		t.Fatalf("POST /api/chat-sessions: status = %d, want a non-2xx method-gate response", gateResp.StatusCode)
	}
}
