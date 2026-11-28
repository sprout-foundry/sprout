//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
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

// TestCommandExecuteTargetsBodyChat: the UI names the chat in the request
// body, so /clear from a background pane must rotate that chat's session and
// leave the active chat alone.
func TestCommandExecuteTargetsBodyChat(t *testing.T) {
	ws := setupConcurrentTestServer(t)

	active, err := ws.getChatAgent(testConcurrentClientID, defaultChatID)
	if err != nil {
		t.Fatalf("active chat agent: %v", err)
	}
	background, err := ws.getChatAgent(testConcurrentClientID, "chat-bg")
	if err != nil {
		t.Fatalf("background chat agent: %v", err)
	}
	activeBefore, backgroundBefore := active.GetSessionID(), background.GetSessionID()

	req := httptest.NewRequest(http.MethodPost, "/api/command/execute",
		strings.NewReader(`{"command":"/clear","chat_id":"chat-bg"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(webClientIDHeader, testConcurrentClientID)
	rec := httptest.NewRecorder()
	ws.handleAPICommandExecute(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if got := active.GetSessionID(); got != activeBefore {
		t.Errorf("active chat session changed from %q to %q", activeBefore, got)
	}
	if got := background.GetSessionID(); got == backgroundBefore {
		t.Errorf("background chat session was not rotated (still %q)", got)
	}
}

// bufferLen reports a chat session's buffered-event count (0 when unset).
func bufferLen(cs *chatSession) int {
	if cs == nil || cs.runBuffer == nil {
		return 0
	}
	return cs.runBuffer.Len()
}

// TestCommandExecuteClearResetsRunBuffer pins the reattach contract: the
// /clear path drops the chat's run buffer (and its TTL timer) before
// publishing session_changed("clear"), so a client reconnecting with
// ?reattach=<chat>&after_seq=<n> afterwards replays nothing.
func TestCommandExecuteClearResetsRunBuffer(t *testing.T) {
	ws := setupConcurrentTestServer(t)

	clientID, chatID := testConcurrentClientID, defaultChatID
	// Buffer a stream chunk and a query_completed — the latter schedules
	// the TTL reset timer, so the test can also assert it is cancelled.
	ws.appendChatEventToRunBuffer(clientID, chatID, events.EventTypeStreamChunk, map[string]interface{}{
		"content": "old-session-chunk",
	})
	ws.appendChatEventToRunBuffer(clientID, chatID, events.EventTypeQueryCompleted, map[string]interface{}{
		"ok": true,
	})

	cs := ws.lookupChatSession(clientID, chatID)
	if cs == nil {
		t.Fatal("chat session not found after buffering")
	}
	cs.mu.Lock()
	if cs.runBuffer == nil || cs.runBuffer.Len() != 2 {
		t.Fatalf("expected 2 buffered events, got %d", bufferLen(cs))
	}
	if cs.runBufferResetTimer == nil {
		t.Fatal("expected the TTL reset timer to be scheduled by query_completed")
	}
	cs.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/command/execute", strings.NewReader(`{"command":"/clear"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(webClientIDHeader, clientID)
	rec := httptest.NewRecorder()
	ws.handleAPICommandExecute(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /clear, got %d: %s", rec.Code, rec.Body.String())
	}

	cs.mu.Lock()
	if bufferLen(cs) != 0 {
		t.Errorf("expected empty run buffer after /clear, got %d", bufferLen(cs))
	}
	if cs.runBufferResetTimer != nil {
		t.Error("expected the TTL reset timer to be cancelled after /clear")
	}
	cs.mu.Unlock()

	// A reattach after the clear replays nothing — only the restored frame.
	msgs := ws.buildChatRunReplayMessages(clientID, chatID, 0)
	if len(msgs) != 1 {
		t.Errorf("expected only the chat_run_restored frame after clear, got %d messages", len(msgs))
	}
	for _, m := range msgs[1:] {
		if m["type"] == events.EventTypeStreamChunk {
			t.Errorf("replay contained a stream chunk from the cleared session: %v", m)
		}
	}
}
