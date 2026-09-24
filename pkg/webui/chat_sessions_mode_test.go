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

// SP-142 §1: chats carry a workspace-mode lane. "" (legacy) reads as code;
// "design" is the other lane. Create stamps the lane, summaries/lists carry
// it, and a lane-scoped switch rejects cross-mode targets.

func TestChatSessionModeCreateStampsLane(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create",
		strings.NewReader(`{"id":"design-1","name":"Design chat","mode":"design"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCreate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ws.mutex.RLock()
	ctx := ws.clientContexts["default"]
	var cs *chatSession
	if ctx != nil {
		cs = ctx.getChatSession("design-1")
	}
	ws.mutex.RUnlock()
	if cs == nil {
		t.Fatal("design-1 chat not found")
	}
	if cs.Mode != "design" {
		t.Fatalf("expected mode design, got %q", cs.Mode)
	}
}

func TestChatSessionModeUnknownValueNormalizesToLegacy(t *testing.T) {
	cs := newChatSessionInMode("x", "X", "bananas")
	if cs.Mode != "" {
		t.Fatalf("expected unknown mode to normalize to \"\", got %q", cs.Mode)
	}
	if got := normalizeChatMode(cs.Mode); got != "code" {
		t.Fatalf("expected \"\" to read as code, got %q", got)
	}
	if got := normalizeChatMode("design"); got != "design" {
		t.Fatalf("expected design to stay design, got %q", got)
	}
}

func TestChatSessionModeListCarriesLane(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	create := func(body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create", strings.NewReader(body))
		rec := httptest.NewRecorder()
		ws.handleAPIChatSessionsCreate(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("create %s failed: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	create(`{"id":"code-1","name":"Code chat"}`)
	create(`{"id":"design-1","name":"Design chat","mode":"design"}`)

	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var list struct {
		ChatSessions []map[string]interface{} `json:"chat_sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]interface{}{}
	for _, entry := range list.ChatSessions {
		byID[entry["id"].(string)] = entry
	}
	if mode, ok := byID["design-1"]["mode"].(string); !ok || mode != "design" {
		t.Fatalf("expected design-1 to carry mode design, got %v", byID["design-1"]["mode"])
	}
	// The code lane is omitted (reads as code client-side).
	if _, ok := byID["code-1"]["mode"]; ok {
		t.Fatalf("expected code-1 to omit mode, got %v", byID["code-1"]["mode"])
	}
}

func TestChatSessionModeSwitchRejectsCrossMode(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	create := func(body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create", strings.NewReader(body))
		rec := httptest.NewRecorder()
		ws.handleAPIChatSessionsCreate(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("create %s failed: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	create(`{"id":"code-1","name":"Code chat"}`)
	create(`{"id":"design-1","name":"Design chat","mode":"design"}`)

	switchTo := func(body string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/switch", strings.NewReader(body))
		rec := httptest.NewRecorder()
		ws.handleAPIChatSessionsSwitch(rec, req)
		return rec.Code
	}

	// Cross-mode: a design-lane client switching to a code chat (and a
	// legacy "" chat is a code chat) must be rejected.
	if code := switchTo(`{"id":"code-1","mode":"design"}`); code != http.StatusConflict {
		t.Fatalf("expected 409 for design→code switch, got %d", code)
	}
	if code := switchTo(`{"id":"design-1","mode":"code"}`); code != http.StatusConflict {
		t.Fatalf("expected 409 for code→design switch, got %d", code)
	}
	// In-lane switches stay free — including the legacy "" chat from code.
	if code := switchTo(`{"id":"code-1","mode":"code"}`); code != http.StatusOK {
		t.Fatalf("expected 200 for code→code(\"\") switch, got %d", code)
	}
	if code := switchTo(`{"id":"design-1","mode":"design"}`); code != http.StatusOK {
		t.Fatalf("expected 200 for design→design switch, got %d", code)
	}
	// No mode named: the backstop stays silent (legacy clients unchanged).
	if code := switchTo(`{"id":"design-1"}`); code != http.StatusOK {
		t.Fatalf("expected 200 for mode-less switch, got %d", code)
	}
}
