//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// SP-142 §3: the workspace query gate. One workspace, one runner — a query
// from a second chat is rejected with 409 workspace_busy (naming the running
// chat) while any other chat in the client context has a query running;
// the gate is the read side only, releasing via the existing query_completed
// lifecycle.

// setupModeTestServer mirrors concurrent_chat_test.go's setup: a temp
// workspace plus a pre-registered client context so /api/query resolves.
func setupModeTestServer(t *testing.T) *ReactWebServer {
	t.Helper()
	daemonRoot := t.TempDir()
	workspaceRoot := filepath.Join(daemonRoot, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot
	ws.SetWorkspaceRoot(workspaceRoot)
	clientCtx := ws.getOrCreateClientContextLocked(testConcurrentClientID)
	clientCtx.WorkspaceRoot = workspaceRoot
	return ws
}

// setChatRunning simulates a running query on a chat the way the shared
// query runner's step-2 set does (test-only flag flip — the real path
// launches a live agent goroutine).
func setChatRunning(ws *ReactWebServer, chatID string) {
	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	ctx := ws.clientContexts[testConcurrentClientID]
	if cs := ctx.getChatSession(chatID); cs != nil {
		cs.setQueryActive(true, "running query")
	}
	ctx.ActiveQuery = true
	ctx.CurrentQuery = "running query"
}

func TestWorkspaceQueryGateRejectsSecondChat(t *testing.T) {
	ws := setupModeTestServer(t)
	if code := switchChatSession(t, ws, testConcurrentClientID, "default"); code != http.StatusOK {
		t.Fatalf("switch to default chat: %d", code)
	}
	secondID := createChatSession(t, ws, testConcurrentClientID, "Second Chat")
	if code := switchChatSession(t, ws, testConcurrentClientID, secondID); code != http.StatusOK {
		t.Fatalf("switch to second chat: %d", code)
	}

	// First chat runs a query; the second chat's submit must be rejected.
	setChatRunning(ws, "default")
	req := httptest.NewRequest(http.MethodPost, "/api/query?chat_id="+secondID,
		strings.NewReader(`{"query":"hello"}`))
	req.Header.Set(webClientIDHeader, testConcurrentClientID)
	rec := httptest.NewRecorder()
	ws.handleAPIQuery(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 workspace_busy, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Error           string `json:"error"`
		Code            string `json:"code"`
		RunningChatID   string `json:"running_chat_id"`
		RunningChatName string `json:"running_chat_name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != "workspace_busy" {
		t.Fatalf("expected code workspace_busy, got %q", payload.Code)
	}
	if payload.RunningChatID != "default" {
		t.Fatalf("expected running_chat_id default, got %q", payload.RunningChatID)
	}
	if payload.RunningChatName != "Chat" {
		t.Fatalf("expected running_chat_name Chat, got %q", payload.RunningChatName)
	}
}

func TestWorkspaceQueryGateReleasesAfterCompletion(t *testing.T) {
	ws := setupModeTestServer(t)
	secondID := createChatSession(t, ws, testConcurrentClientID, "Second Chat")
	if code := switchChatSession(t, ws, testConcurrentClientID, secondID); code != http.StatusOK {
		t.Fatalf("switch to second chat: %d", code)
	}

	setChatRunning(ws, "default")
	// Release via the query_completed lifecycle: the runner's deferred
	// cleanup is setChatQueryActive(false, ""), which the gate reads.
	ws.mutex.Lock()
	ws.clientContexts[testConcurrentClientID].setChatQueryActive("default", false, "")
	ws.activeQueries = 0
	ws.mutex.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/query?chat_id="+secondID,
		strings.NewReader(`{"query":"hello"}`))
	req.Header.Set(webClientIDHeader, testConcurrentClientID)
	rec := httptest.NewRecorder()
	ws.handleAPIQuery(rec, req)
	// No provider is configured in the test workspace, so the submit past
	// the gate fails at agent creation (503) — the point is that it got
	// PAST the workspace gate (not 409 workspace_busy).
	if rec.Code == http.StatusConflict {
		t.Fatalf("expected the gate to release, got 409: %s", rec.Body.String())
	}
}

func TestWorkspaceQueryGateSameChatUnaffected(t *testing.T) {
	ws := setupModeTestServer(t)
	setChatRunning(ws, "default")

	// A same-chat submit hits the pre-existing query_in_progress 409 (shared
	// mode / steer semantics) — never the workspace gate. The workspace gate
	// must not widen that rejection's meaning.
	req := httptest.NewRequest(http.MethodPost, "/api/query?chat_id=default",
		strings.NewReader(`{"query":"hello"}`))
	req.Header.Set(webClientIDHeader, testConcurrentClientID)
	rec := httptest.NewRecorder()
	ws.handleAPIQuery(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != "query_in_progress" {
		t.Fatalf("expected query_in_progress (same-chat semantics), got %q", payload.Code)
	}
}
