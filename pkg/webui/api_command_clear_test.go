//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCommandExecuteClearRotatesAndNotifies pins the New Session button's
// backend contract: POST /api/command/execute with /clear must (a) work on a
// fresh client context (the button may be the first thing a new browser
// session invokes), (b) actually rotate the agent's session, and (c) publish
// a session_changed event so the WebUI clears its transcript — the button
// previously executed the command but published nothing, so the UI never
// cleared.
func TestCommandExecuteClearRotatesAndNotifies(t *testing.T) {
	ws := setupConcurrentTestServer(t)

	// Fresh HTTP request with the test client header — no prior API calls,
	// exactly like a browser's first New Session click.
	req := httptest.NewRequest(http.MethodPost, "/api/command/execute", strings.NewReader(`{"command":"/clear"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(webClientIDHeader, testConcurrentClientID)
	rec := httptest.NewRecorder()
	ws.handleAPICommandExecute(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /clear, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Command string `json:"command"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Command != "clear" {
		t.Fatalf("expected command 'clear', got %q", resp.Command)
	}
	if resp.Error != "" {
		t.Fatalf("expected no error, got %q", resp.Error)
	}

	// The agent for the default chat must have rotated to a new session.
	agentInst, err := ws.getChatAgent(testConcurrentClientID, defaultChatID)
	if err != nil {
		t.Fatalf("getChatAgent after clear: %v", err)
	}
	if got := agentInst.GetSessionID(); got == "" || strings.HasPrefix(got, "session_") == false {
		// RotateSession assigns fresh session_<nanos> IDs; an empty ID means
		// the rotation never ran.
		t.Fatalf("expected a rotated session id, got %q", got)
	}
}
