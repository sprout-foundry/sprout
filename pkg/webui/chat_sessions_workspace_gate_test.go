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

// The workspace query gate. One workspace, one runner — a query
// from a second chat is rejected with 409 workspace_busy (naming the running
// chat) while any other chat in the client context has a query running;
// the gate is the read side only, releasing via the existing query_completed
// lifecycle.

// setupWorkspaceGateTestServer mirrors concurrent_chat_test.go's setup: a temp
// workspace plus a pre-registered client context so /api/query resolves.
func setupWorkspaceGateTestServer(t *testing.T) *ReactWebServer {
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
	if _, err := ws.SetWorkspaceRoot(workspaceRoot); err != nil {
		t.Fatal(err)
	}
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
	ws := setupWorkspaceGateTestServer(t)
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
	ws := setupWorkspaceGateTestServer(t)
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
	ws := setupWorkspaceGateTestServer(t)
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
