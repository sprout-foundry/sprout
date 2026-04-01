package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alantheprice/ledit/pkg/events"
)

const testConcurrentClientID = "test-concurrent-client"

// setupConcurrentTestServer creates a ReactWebServer with a temporary workspace
// and pre-registers a client context so callers can make chat-session API
// requests immediately.
func setupConcurrentTestServer(t *testing.T) *ReactWebServer {
	t.Helper()

	daemonRoot := t.TempDir()
	workspaceRoot := filepath.Join(daemonRoot, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	ws.daemonRoot = daemonRoot
	ws.workspaceRoot = workspaceRoot
	ws.terminalManager = NewTerminalManager(daemonRoot)
	ws.fileConsents = newFileConsentManager()

	// Pre-register client context so resolveClientID finds a workspace root.
	clientCtx := ws.getOrCreateClientContextLocked(testConcurrentClientID)
	clientCtx.WorkspaceRoot = workspaceRoot
	clientCtx.Terminal = NewTerminalManager(workspaceRoot)
	clientCtx.FileConsents = newFileConsentManager()

	return ws
}

// --- Helper functions ---

// createChatSession initializes the client context (if needed), creates a chat
// session via the API, and returns the chat ID extracted from the JSON response.
func createChatSession(t *testing.T, ws *ReactWebServer, clientID, name string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create", bytes.NewReader(body))
	req.Header.Set(webClientIDHeader, clientID)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCreate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create chat session: status %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		ChatSession struct {
			ID string `json:"id"`
		} `json:"chat_session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if resp.ChatSession.ID == "" {
		t.Fatal("expected non-empty chat session id in create response")
	}
	return resp.ChatSession.ID
}

// listChatSessions returns the list of chat session info via the API.
func listChatSessions(t *testing.T, ws *ReactWebServer, clientID string) []map[string]interface{} {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions", nil)
	req.Header.Set(webClientIDHeader, clientID)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list chat sessions: status %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		ChatSessions []map[string]interface{} `json:"chat_sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	return resp.ChatSessions
}

// switchChatSession calls the switch API and returns the HTTP status code.
func switchChatSession(t *testing.T, ws *ReactWebServer, clientID, chatID string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"id": chatID})
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/switch", bytes.NewReader(body))
	req.Header.Set(webClientIDHeader, clientID)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsSwitch(rec, req)
	return rec.Code
}

// compactChatSession calls the compact API and returns the HTTP status code.
func compactChatSession(t *testing.T, ws *ReactWebServer, clientID, chatID string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"id": chatID})
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/compact", bytes.NewReader(body))
	req.Header.Set(webClientIDHeader, clientID)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsCompact(rec, req)
	return rec.Code
}

// queryChat sends a query to a specific chat and returns the HTTP status code.
// The chat ID is passed via the chat_id query parameter.
func queryChat(t *testing.T, ws *ReactWebServer, clientID, chatID, query string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"query": query})
	url := "/api/query?chat_id=" + chatID
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set(webClientIDHeader, clientID)
	rec := httptest.NewRecorder()
	ws.handleAPIQuery(rec, req)
	return rec.Code
}

// --- Tests ---

func TestConcurrentChatSwitchDuringQueryAllowed(t *testing.T) {
	ws := setupConcurrentTestServer(t)

	// Create a second chat session (the "default" one exists already).
	secondChatID := createChatSession(t, ws, testConcurrentClientID, "Second Chat")

	// Simulate an active query on the default chat by flipping the flag
	// directly — we can't use handleAPIQuery because it would launch a real
	// agent goroutine. Instead, we manipulate the chat-level active-query flag
	// which is what the switch handler no longer checks.
	ws.mutex.Lock()
	ctx := ws.clientContexts[testConcurrentClientID]
	if cs := ctx.getChatSession(ctx.DefaultChatID); cs != nil {
		cs.setQueryActive(true, "running query")
	}
	// Also set the top-level flag for backward compat paths.
	ctx.ActiveQuery = true
	ctx.CurrentQuery = "running query"
	ws.activeQueries = 1
	ws.mutex.Unlock()

	// Verify the default chat appears to have an active query.
	sessions := listChatSessions(t, ws, testConcurrentClientID)
	var foundActive bool
	for _, s := range sessions {
		if s["id"] == ctx.DefaultChatID {
			foundActive = s["active_query"].(bool)
		}
	}
	if !foundActive {
		t.Fatal("expected default chat to report active_query=true before switch")
	}

	// Switching to another chat should succeed (200) even though the default
	// chat has an active query — the key behaviour change.
	code := switchChatSession(t, ws, testConcurrentClientID, secondChatID)
	if code != http.StatusOK {
		t.Fatalf("expected switch during active query to return 200, got %d", code)
	}

	// Verify the active chat changed to the second chat.
	ws.mutex.RLock()
	activeID := ctx.getActiveChatID()
	ws.mutex.RUnlock()
	if activeID != secondChatID {
		t.Fatalf("expected active chat %q after switch, got %q", secondChatID, activeID)
	}
}

func TestConcurrentChatCompactNonActive(t *testing.T) {
	ws := setupConcurrentTestServer(t)

	// Create a second chat session.
	secondChatID := createChatSession(t, ws, testConcurrentClientID, "Compactable Chat")

	// Compact the non-active chat session. Before the change this returned
	// 400 because compact only worked on the active chat. Now any chat can
	// be compacted.
	code := compactChatSession(t, ws, testConcurrentClientID, secondChatID)
	if code != http.StatusOK {
		t.Fatalf("expected compact on non-active chat to return 200, got %d", code)
	}
}

func TestConcurrentChatsGetDistinctAgents(t *testing.T) {
	ws := setupConcurrentTestServer(t)

	// Create a second chat session.
	secondChatID := createChatSession(t, ws, testConcurrentClientID, "Agent Test Chat")

	// Force agent creation for each chat via getChatAgent. This returns the
	// underlying *agent.Agent pointers.
	agentA, err := ws.getChatAgent(testConcurrentClientID, defaultChatID)
	if err != nil {
		t.Fatalf("getChatAgent for default chat: %v", err)
	}
	agentB, err := ws.getChatAgent(testConcurrentClientID, secondChatID)
	if err != nil {
		t.Fatalf("getChatAgent for second chat: %v", err)
	}

	if agentA == nil || agentB == nil {
		t.Fatal("expected non-nil agents for both chats")
	}
	if agentA == agentB {
		t.Fatal("expected distinct agent instances for different chat sessions")
	}

	// Verify that calling getChatAgent again returns the same cached instances
	// (deterministic pointer comparison).
	agentA2, err := ws.getChatAgent(testConcurrentClientID, defaultChatID)
	if err != nil {
		t.Fatalf("getChatAgent retry for default chat: %v", err)
	}
	if agentA != agentA2 {
		t.Fatal("expected getChatAgent to return the same cached agent for default chat")
	}
	agentB2, err := ws.getChatAgent(testConcurrentClientID, secondChatID)
	if err != nil {
		t.Fatalf("getChatAgent retry for second chat: %v", err)
	}
	if agentB != agentB2 {
		t.Fatal("expected getChatAgent to return the same cached agent for second chat")
	}
}

func TestConcurrentChatQueryIsolated(t *testing.T) {
	ws := setupConcurrentTestServer(t)

	// Create a second chat session so we can exercise per-chat query state.
	chatB := createChatSession(t, ws, testConcurrentClientID, "Isolation Chat")

	// Mark chat A (default) as having an active query.
	ws.mutex.Lock()
	ctx := ws.clientContexts[testConcurrentClientID]
	chatAID := ctx.DefaultChatID
	defaultCS := ctx.getChatSession(chatAID)
	if defaultCS != nil {
		defaultCS.setQueryActive(true, "query on chat A")
	}
	ctx.setChatQueryActive(chatAID, true, "query on chat A")
	ws.mutex.Unlock()

	// Verify chat A has an active query.
	if !ctx.hasActiveQueryForChat(chatAID) {
		t.Fatal("expected chat A to report active query")
	}

	// Verify chat B does NOT have an active query.
	if ctx.hasActiveQueryForChat(chatB) {
		t.Fatal("expected chat B to not have an active query")
	}

	// Sending a query to chat B should succeed (202) because chat B has no
	// active query — isolation means chat A's query doesn't block chat B.
	//
	// Note: handleAPIQuery launches a real goroutine but it first checks
	// for active queries, so if the check passes it will return 202 before
	// the agent goroutine runs. We verify the pre-check returns the right
	// status without waiting for the background goroutine.
	code := queryChat(t, ws, testConcurrentClientID, chatB, "test query for chat B")
	if code != http.StatusAccepted {
		t.Fatalf("expected query to non-active chat to return 202, got %d", code)
	}

	// Sending a query to chat A should be rejected (409) because chat A
	// already has an active query.
	code = queryChat(t, ws, testConcurrentClientID, chatAID, "should be blocked")
	if code != http.StatusConflict {
		t.Fatalf("expected query to already-active chat to return 409, got %d", code)
	}
}

func TestConcurrentChatAgentLazyCreation(t *testing.T) {
	ws := setupConcurrentTestServer(t)

	// The client context is created by setupConcurrentTestServer. Verify that
	// the default chat session exists but has NOT created an agent yet.
	ws.mutex.RLock()
	ctx := ws.clientContexts[testConcurrentClientID]
	defaultCS := ctx.getChatSession(defaultChatID)
	ws.mutex.RUnlock()

	if defaultCS == nil {
		t.Fatal("expected default chat session to exist")
	}

	// Verify no agent on the chat session.
	defaultCS.mu.Lock()
	hasAgent := defaultCS.Agent != nil
	defaultCS.mu.Unlock()
	if hasAgent {
		t.Fatal("expected chat session to have no agent until first query")
	}

	// Verify no agent on the client context top-level either.
	if ctx.Agent != nil {
		t.Fatal("expected client context to have no agent until first query")
	}

	// Force agent creation for this chat by calling getChatAgent.
	agentInst, err := ws.getChatAgent(testConcurrentClientID, defaultChatID)
	if err != nil {
		t.Fatalf("getChatAgent: %v", err)
	}
	if agentInst == nil {
		t.Fatal("expected non-nil agent after getChatAgent call")
	}

	// Now the chat session should have a cached agent.
	defaultCS.mu.Lock()
	hasAgent = defaultCS.Agent != nil
	defaultCS.mu.Unlock()
	if !hasAgent {
		t.Fatal("expected chat session to have an agent after getChatAgent call")
	}

	// A second call should return the exact same pointer (no new agent created).
	agentInst2, err := ws.getChatAgent(testConcurrentClientID, defaultChatID)
	if err != nil {
		t.Fatalf("getChatAgent second call: %v", err)
	}
	if agentInst != agentInst2 {
		t.Fatal("expected getChatAgent to return the same cached agent instance")
	}
}
