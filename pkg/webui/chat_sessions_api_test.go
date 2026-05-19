package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestHandleAPIChatSessionsMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessions(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsCreateMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions/create", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCreate(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsCreateInvalidJSON(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCreate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsCreateSuccess(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create", strings.NewReader(`{"name":"New Chat"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCreate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIChatSessionsCreateEmptyName(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCreate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with auto-name, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsDeleteMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions/delete", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsDelete(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsDeleteMissingID(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/delete", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsDelete(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsDeleteDefaultSession(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/delete", strings.NewReader(`{"id":"default"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsDelete(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for default session, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "default") {
		t.Errorf("expected response to mention default, got: %s", body)
	}
}

func TestHandleAPIChatSessionsDeleteNotFound(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/delete", strings.NewReader(`{"id":"nonexistent"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsDelete(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for not found, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsDeleteInvalidJSON(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/delete", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsDelete(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsRenameMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions/rename", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsRename(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsRenameMissingID(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/rename", strings.NewReader(`{"name":"New Name"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsRename(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing ID, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsRenameMissingName(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/rename", strings.NewReader(`{"id":"test"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsRename(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsRenameNotFound(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/rename", strings.NewReader(`{"id":"nonexistent","name":"New"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsRename(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for not found, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsRenameInvalidJSON(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/rename", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsRename(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsPinMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions/pin", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsPin(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsPinMissingID(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/pin", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsPin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsPinNotFound(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/pin", strings.NewReader(`{"id":"nonexistent"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsPin(rec, req)

	// When no client context exists for default client, returns 404
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 404 or 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsPinInvalidJSON(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/pin", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsPin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsPinSuccess(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	ws.getOrCreateClientContext("default")

	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/pin", strings.NewReader(`{"id":"default"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsPin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIChatSessionsUnpinMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions/unpin", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsUnpin(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsUnpinMissingID(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/unpin", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsUnpin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsUnpinInvalidJSON(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/unpin", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsUnpin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsUnpinSuccess(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	ws.getOrCreateClientContext("default")

	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/unpin", strings.NewReader(`{"id":"default"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsUnpin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIChatSessionsSwitchMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions/switch", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsSwitch(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsSwitchMissingID(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/switch", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsSwitch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsSwitchNotFound(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	ws.getOrCreateClientContext("default")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/switch", strings.NewReader(`{"id":"nonexistent"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsSwitch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsSwitchInvalidJSON(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/switch", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsSwitch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsCompactMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions/compact", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCompact(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsCompactInvalidJSON(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/compact", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCompact(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionClearHistoryMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions/history", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionClearHistory(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionClearHistoryInvalidJSON(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/history", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionClearHistory(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsDeleteAllMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions/delete-all", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsDeleteAll(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsDeleteAllSuccess(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	ws.getOrCreateClientContext("default")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/delete-all", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsDeleteAll(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResolveChatID(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	ws.getOrCreateClientContext("default")

	t.Run("query param chat_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?chat_id=test-chat", nil)
		got := ws.resolveChatID(req, "default")
		if got != "test-chat" {
			t.Errorf("expected test-chat, got %q", got)
		}
	})

	t.Run("fallback to default", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		got := ws.resolveChatID(req, "default")
		if got != "default" {
			t.Errorf("expected default, got %q", got)
		}
	})
}

func TestHandleAPIChatSessionsCreateWithHandoff_MethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions/create-with-handoff", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCreateWithHandoff(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsCreateWithHandoff_InvalidJSON(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create-with-handoff", strings.NewReader("bad json"))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCreateWithHandoff(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionsCreateWithHandoff_MissingSourceChatID(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create-with-handoff", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCreateWithHandoff(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "sourceChatId") {
		t.Errorf("expected error about sourceChatId, got: %s", body)
	}
}

func TestHandleAPIChatSessionsCreateWithHandoff_SourceNotFound(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	ws.getOrCreateClientContext("default")
	
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create-with-handoff", strings.NewReader(`{"sourceChatId":"nonexistent"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCreateWithHandoff(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "not found") {
		t.Errorf("expected error about not found, got: %s", body)
	}
}

func TestHandleAPIChatSessionsCreateWithHandoff_Success(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	
	// Create a source chat session
	clientID := "default"
	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	sourceCS := ctx.getOrCreateChatSession("source-chat")
	
	// Add agent state with turn checkpoints
	state := map[string]interface{}{
		"messages": []interface{}{},
		"turn_checkpoints": []map[string]interface{}{
			{
				"start_index":         0,
				"end_index":           1,
				"summary":             "Test summary",
				"actionable_summary":  "Test actionable summary",
			},
		},
	}
	stateBytes, _ := json.Marshal(state)
	sourceCS.AgentState = stateBytes
	ws.mutex.Unlock()
	
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create-with-handoff", strings.NewReader(`{"sourceChatId":"source-chat"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCreateWithHandoff(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["message"] != "Chat session created with context from previous chat" {
		t.Errorf("unexpected message: %v", resp["message"])
	}

	chatSession, ok := resp["chat_session"].(map[string]interface{})
	if !ok {
		t.Fatal("chat_session not in response")
	}

	// Verify the new chat session has an ID
	if chatSession["id"] == nil {
		t.Error("chat session id is nil")
	}

	// Verify it's not the same as the source
	if chatSession["id"] == "source-chat" {
		t.Error("new chat ID should be different from source")
	}

	// Verify active_chat_id is present and matches the new session ID
	if resp["active_chat_id"] == nil {
		t.Error("active_chat_id not in response")
	}
	if resp["active_chat_id"] != chatSession["id"] {
		t.Errorf("active_chat_id %v does not match chat session id %v", resp["active_chat_id"], chatSession["id"])
	}
}

func TestHandleAPIChatSessionsCreateWithHandoff_CustomName(t *testing.T) {
	ws := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	
	// Create a source chat session
	clientID := "default"
	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	sourceCS := ctx.getOrCreateChatSession("source-chat")
	sourceCS.AgentState = []byte{}
	ws.mutex.Unlock()
	
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create-with-handoff", strings.NewReader(`{"sourceChatId":"source-chat","name":"Custom Handoff Chat"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCreateWithHandoff(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	chatSession, ok := resp["chat_session"].(map[string]interface{})
	if !ok {
		t.Fatal("chat_session not in response")
	}

	if chatSession["name"] != "Custom Handoff Chat" {
		t.Errorf("expected name Custom Handoff Chat, got %v", chatSession["name"])
	}
}
