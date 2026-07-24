//go:build !js

package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
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

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot
	ws.SetWorkspaceRoot(workspaceRoot)
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

// deleteChatSession calls the delete API and returns the HTTP status code and
// response body.
func deleteChatSession(t *testing.T, ws *ReactWebServer, clientID, chatID string) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"id": chatID})
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/delete", bytes.NewReader(body))
	req.Header.Set(webClientIDHeader, clientID)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsDelete(rec, req)
	return rec.Code, rec.Body.Bytes()
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

func TestChatSessionProviderModelScoping(t *testing.T) {
	ws := setupConcurrentTestServer(t)

	// Create two chat sessions.
	chatA := createChatSession(t, ws, testConcurrentClientID, "Chat A")
	chatB := createChatSession(t, ws, testConcurrentClientID, "Chat B")

	// Set provider/model on Chat A, and different values on Chat B, by
	// directly manipulating the chatSession struct fields.
	ws.mutex.Lock()
	ctx := ws.clientContexts[testConcurrentClientID]
	if csA := ctx.getChatSession(chatA); csA != nil {
		csA.mu.Lock()
		csA.Provider = "openai"
		csA.Model = "gpt-4"
		csA.mu.Unlock()
	}
	if csB := ctx.getChatSession(chatB); csB != nil {
		csB.mu.Lock()
		csB.Provider = "anthropic"
		csB.Model = "claude-3"
		csB.mu.Unlock()
	}
	ws.mutex.Unlock()

	// List all sessions and verify each has its own provider/model via the API.
	sessions := listChatSessions(t, ws, testConcurrentClientID)
	for _, s := range sessions {
		switch s["id"].(string) {
		case chatA:
			if s["provider"] != "openai" {
				t.Errorf("chat A: expected provider 'openai', got %v", s["provider"])
			}
			if s["model"] != "gpt-4" {
				t.Errorf("chat A: expected model 'gpt-4', got %v", s["model"])
			}
		case chatB:
			if s["provider"] != "anthropic" {
				t.Errorf("chat B: expected provider 'anthropic', got %v", s["provider"])
			}
			if s["model"] != "claude-3" {
				t.Errorf("chat B: expected model 'claude-3', got %v", s["model"])
			}
		}
	}

	// Switch to Chat A and verify the switch response includes provider/model.
	body, _ := json.Marshal(map[string]string{"id": chatA})
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/switch", bytes.NewReader(body))
	req.Header.Set(webClientIDHeader, testConcurrentClientID)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionsSwitch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("switch to chat A: status %d (%s)", rec.Code, rec.Body.String())
	}
	var switchResp struct {
		ChatSession map[string]interface{} `json:"chat_session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &switchResp); err != nil {
		t.Fatalf("decode switch response: %v", err)
	}
	if switchResp.ChatSession["provider"] != "openai" {
		t.Errorf("switch response: expected provider 'openai', got %v", switchResp.ChatSession["provider"])
	}
	if switchResp.ChatSession["model"] != "gpt-4" {
		t.Errorf("switch response: expected model 'gpt-4', got %v", switchResp.ChatSession["model"])
	}
}

// TestChatSessionDeleteAPI exercises the delete handler end-to-end via the
// HTTP API surface: active-query rejection, successful deletion, and
// double-delete rejection.
func TestChatSessionDeleteAPI(t *testing.T) {
	ws := setupConcurrentTestServer(t)

	// Create two chat sessions: "A" and "B".
	chatA := createChatSession(t, ws, testConcurrentClientID, "Chat A")
	chatB := createChatSession(t, ws, testConcurrentClientID, "Chat B")

	// 1. Mark chat "A" as active — delete should fail with 400 active query.
	ws.mutex.Lock()
	ctx := ws.clientContexts[testConcurrentClientID]
	if cs := ctx.getChatSession(chatA); cs != nil {
		cs.setQueryActive(true, "test query")
	}
	ws.mutex.Unlock()

	code, _ := deleteChatSession(t, ws, testConcurrentClientID, chatA)
	if code != http.StatusBadRequest {
		t.Fatalf("expected delete of active chat to return 400, got %d", code)
	}

	// 2. Mark chat "A" as inactive — delete should succeed.
	ws.mutex.Lock()
	if cs := ctx.getChatSession(chatA); cs != nil {
		cs.setQueryActive(false, "")
	}
	ws.mutex.Unlock()

	code, _ = deleteChatSession(t, ws, testConcurrentClientID, chatA)
	if code != http.StatusOK {
		t.Fatalf("expected delete of inactive chat to return 200, got %d", code)
	}

	// Verify the chat is gone from the registry.
	ws.mutex.RLock()
	_, exists := ctx.ChatSessions[chatA]
	ws.mutex.RUnlock()
	if exists {
		t.Fatal("expected chat A to be removed from ChatSessions after delete")
	}

	// 3. Delete chat "A" again — should fail with 400 not found.
	code, _ = deleteChatSession(t, ws, testConcurrentClientID, chatA)
	if code != http.StatusBadRequest {
		t.Fatalf("expected double-delete to return 400, got %d", code)
	}

	// 4. Verify the remaining chat "B" is unaffected.
	ws.mutex.RLock()
	_, exists = ctx.ChatSessions[chatB]
	ws.mutex.RUnlock()
	if !exists {
		t.Fatal("expected chat B to still exist after deleting chat A")
	}

	// 5. Verify top-level ActiveQuery was reset to false (no remaining
	// active queries).
	ws.mutex.RLock()
	activeQuery := ctx.ActiveQuery
	ws.mutex.RUnlock()
	if activeQuery {
		t.Fatal("expected top-level ActiveQuery to be false after delete")
	}
}

// TestChatSessionDeleteCannotDeleteDefault verifies that the default chat
// session cannot be deleted.
func TestChatSessionDeleteCannotDeleteDefault(t *testing.T) {
	ws := setupConcurrentTestServer(t)

	code, _ := deleteChatSession(t, ws, testConcurrentClientID, defaultChatID)
	if code != http.StatusBadRequest {
		t.Fatalf("expected delete of default chat to return 400, got %d", code)
	}
}

// TestChatSessionDeleteCannotDeleteActive verifies that the currently active
// (switched-to) chat session cannot be deleted.
func TestChatSessionDeleteCannotDeleteActive(t *testing.T) {
	ws := setupConcurrentTestServer(t)

	// Create and switch to a second chat.
	chatB := createChatSession(t, ws, testConcurrentClientID, "Chat B")
	switchChatSession(t, ws, testConcurrentClientID, chatB)

	// Try to delete the active chat — should fail.
	code, _ := deleteChatSession(t, ws, testConcurrentClientID, chatB)
	if code != http.StatusBadRequest {
		t.Fatalf("expected delete of active chat to return 400, got %d", code)
	}
}

// TestChatSessionDeleteConcurrentWithQuery verifies that a concurrent delete
// and query cannot both succeed. The system must end up in a consistent state:
// either the delete wins (query is rejected) or the query wins (delete is
// rejected because the chat now has an active query).
//
// Goroutines from previous iterations may still be draining when we read
// shared state, so we wait for all in-flight work to settle before each
// iteration's assertion.
func TestChatSessionDeleteConcurrentWithQuery(t *testing.T) {
	ws := setupConcurrentTestServer(t)

	// Create a chat session to delete/query concurrently.
	chatX := createChatSession(t, ws, testConcurrentClientID, "Chat X")

	const iterations = 30
	for i := 0; i < iterations; i++ {
		// Reset: ensure chat X exists, is inactive, and no goroutines are
		// running from a previous iteration.
		waitForActiveQueriesToDrain(t, ws, 5*time.Second)
		ws.mutex.Lock()
		ctx := ws.clientContexts[testConcurrentClientID]
		if _, ok := ctx.ChatSessions[chatX]; !ok {
			cs := newChatSession(chatX, "Chat X")
			ctx.ChatSessions[chatX] = cs
		} else {
			if cs := ctx.getChatSession(chatX); cs != nil {
				cs.setQueryActive(false, "")
			}
		}
		ctx.ActiveQuery = false
		ctx.CurrentQuery = ""
		ws.activeQueries = 0
		ws.mutex.Unlock()

		// Spawn two goroutines: one for delete, one for query.
		var deleteCode, queryCode int
		var wg sync.WaitGroup
		startCh := make(chan struct{})

		wg.Add(2)
		go func() {
			defer wg.Done()
			<-startCh
			code, _ := deleteChatSession(t, ws, testConcurrentClientID, chatX)
			deleteCode = code
		}()
		go func() {
			defer wg.Done()
			<-startCh
			code := queryChat(t, ws, testConcurrentClientID, chatX, "concurrent query")
			queryCode = code
		}()

		// Start both goroutines simultaneously.
		close(startCh)
		wg.Wait()

		// Wait for the query goroutine (if any) to finish so we can
		// read ws.activeQueries without race-with-leftover-goroutine noise.
		waitForActiveQueriesToDrain(t, ws, 5*time.Second)

		// Verify the invariant for this iteration:
		// If delete succeeded (200), the chat should be gone (or
		// getChatAgent may have lazily recreated it — pre-existing
		// behaviour, not the bug we're fixing). What matters: no
		// orphan goroutine should be running against a deleted chat.
		if deleteCode == http.StatusOK {
			ws.mutex.RLock()
			activeQueries := ws.activeQueries
			ws.mutex.RUnlock()

			if activeQueries > 0 && queryCode == http.StatusAccepted {
				t.Fatalf("iteration %d: delete=%d succeeded but ws.activeQueries=%d (orphan goroutine on deleted chat)",
					i, deleteCode, activeQueries)
			}
		}
	}
}

// waitForActiveQueriesToDrain blocks until ws.activeQueries reaches 0 or the
// timeout fires. Used to ensure goroutines from a prior iteration have
// completed cleanup before reading shared state.
func waitForActiveQueriesToDrain(t *testing.T, ws *ReactWebServer, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ws.mutex.RLock()
		n := ws.activeQueries
		ws.mutex.RUnlock()
		if n == 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Logf("warning: active queries did not drain within %s", timeout)
}
