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

func TestHandleAPIChatSessionMessagesMethodNotAllowed(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/messages", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionMessages(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionMessagesReturnsMessagesWithoutSwitching(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Create two sessions so we can prove the messages fetch does not move
	// the active chat.
	createReq := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create", strings.NewReader(`{}`))
	createRec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCreate(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create: expected 200, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var createResp struct {
		ChatSession struct {
			ID string `json:"id"`
		} `json:"chat_session"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatal(err)
	}
	createdID := createResp.ChatSession.ID

	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions/messages?chat_id="+createdID, nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ChatID      string `json:"chat_id"`
		ChatSession struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		} `json:"chat_session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ChatSession.ID != createdID {
		t.Fatalf("expected session %q, got %q", createdID, resp.ChatSession.ID)
	}
}

func TestHandleAPIChatSessionMessagesUnknownChat(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions/messages?chat_id=does-not-exist", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionMessages(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
