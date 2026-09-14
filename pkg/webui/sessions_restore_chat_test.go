//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// postRestore issues a chat-scoped session restore request.
func postRestore(t *testing.T, ws *ReactWebServer, clientID, sessionID, chatID string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]string{"session_id": sessionID}
	if chatID != "" {
		body["chat_id"] = chatID
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/restore", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(webClientIDHeader, clientID)
	rec := httptest.NewRecorder()
	ws.handleAPIRestoreSession(rec, req)
	return rec
}

// TestRestoreSessionScopesToRequestedChat pins the multi-chat contract:
// restoring with an explicit chat_id must import the state into THAT
// chat's agent, not the client's active chat. Previously the handler
// always wrote into the active chat — a background pane restoring its own
// history would clobber whatever chat happened to be focused.
func TestRestoreSessionScopesToRequestedChat(t *testing.T) {
	ws := setupConcurrentTestServer(t)

	// Two chats; make the SECOND one active.
	chatA := defaultChatID
	chatB := createChatSession(t, ws, testConcurrentClientID, "Restore Target")
	if code := switchChatSession(t, ws, testConcurrentClientID, chatB); code != http.StatusOK {
		t.Fatalf("switch to chatB: %d", code)
	}

	// Seed a saved session on disk for chatA's agent to restore from.
	agentA, err := ws.getChatAgent(testConcurrentClientID, chatA)
	if err != nil {
		t.Fatalf("getChatAgent A: %v", err)
	}
	// Fresh agents start with an empty session ID; give it one so the
	// state save has a valid target.
	if agentA.GetSessionID() == "" {
		agentA.SetSessionID("session_restore_test_a")
	}
	savedID := agentA.GetSessionID()
	agentA.AddMessage(api.Message{Role: "user", Content: "hello from the saved session"})
	if err := agentA.SaveStateScoped(savedID, ws.workspaceRoot); err != nil {
		t.Fatalf("save state: %v", err)
	}

	// Restore that session INTO chat A while chat B is active.
	rec := postRestore(t, ws, testConcurrentClientID, savedID, chatA)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Chat A's agent now holds the restored message; chat B's does not.
	msgsA := agentA.GetMessages()
	if len(msgsA) == 0 {
		t.Fatal("chat A agent has no messages after restore")
	}
	agentB, err := ws.getChatAgent(testConcurrentClientID, chatB)
	if err != nil {
		t.Fatalf("getChatAgent B: %v", err)
	}
	for _, m := range agentB.GetMessages() {
		if strings.Contains(m.Content, "saved session") {
			t.Fatalf("restore leaked into chat B (active) — messages must stay scoped")
		}
	}

	// Top-level current-session pointer must NOT reflect the background
	// restore — /api/sessions reports it as current_session_id.
	ws.mutex.RLock()
	ctx := ws.clientContexts[testConcurrentClientID]
	topCurrent := ctx.CurrentSessionID
	bCurrent := ctx.ChatSessions[chatB].CurrentSessionID
	ws.mutex.RUnlock()
	if topCurrent == savedID {
		t.Fatalf("top-level CurrentSessionID %q reflects the background restore; must stay scoped to the active chat", topCurrent)
	}
	if bCurrent == savedID {
		t.Fatalf("active chat B's CurrentSessionID %q was clobbered by chat A's restore", bCurrent)
	}
}
