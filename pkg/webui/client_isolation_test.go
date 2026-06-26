//go:build !js

package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestMultiWindowClientIsolationForWorkspaceSessionAndModel(t *testing.T) {
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolatedHome, ".config"))
	t.Setenv("USERPROFILE", isolatedHome)
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	t.Cleanup(func() { credentials.ResetStorageBackend() })
	credentials.ResetStorageBackend()

	daemonRoot := t.TempDir()
	workspaceA := filepath.Join(daemonRoot, "workspace-a")
	workspaceB := filepath.Join(daemonRoot, "workspace-b")
	if err := os.MkdirAll(workspaceA, 0o755); err != nil {
		t.Fatalf("mkdir workspaceA: %v", err)
	}
	if err := os.MkdirAll(workspaceB, 0o755); err != nil {
		t.Fatalf("mkdir workspaceB: %v", err)
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot
	ws.workspaceRoot = daemonRoot
	ws.terminalManager = NewTerminalManager(daemonRoot)
	ws.fileConsents = newFileConsentManager()

	clientA := "window-a"
	clientB := "window-b"

	if _, err := ws.setClientWorkspaceRoot(clientA, workspaceA); err != nil {
		t.Fatalf("set clientA workspace: %v", err)
	}
	if _, err := ws.setClientWorkspaceRoot(clientB, workspaceB); err != nil {
		t.Fatalf("set clientB workspace: %v", err)
	}

	agentA, err := ws.getClientAgent(clientA)
	if err != nil {
		t.Fatalf("get clientA agent: %v", err)
	}
	agentB, err := ws.getClientAgent(clientB)
	if err != nil {
		t.Fatalf("get clientB agent: %v", err)
	}
	if agentA == nil || agentB == nil {
		t.Fatal("expected non-nil agents for both clients")
	}
	if agentA == agentB {
		t.Fatal("expected distinct live agents per client")
	}
	if !agentA.IsStreamingEnabled() || !agentB.IsStreamingEnabled() {
		t.Fatal("expected WebUI client agents to have streaming enabled")
	}

	const modelA = "window-a-model"
	if err := agentA.SetModel(modelA); err != nil {
		t.Fatalf("set model for clientA: %v", err)
	}
	if err := ws.syncAgentStateForClient(clientA); err != nil {
		t.Fatalf("sync clientA state: %v", err)
	}

	if got := agentB.GetModel(); got == modelA {
		t.Fatalf("clientB model leaked from clientA: %q", got)
	}
	if agentA.GetProvider() == "" || agentB.GetProvider() == "" {
		t.Fatalf("expected non-empty providers, got clientA=%q clientB=%q", agentA.GetProvider(), agentB.GetProvider())
	}

	snapshotA, _ := json.Marshal(agent.AgentState{SessionID: "session-a", Messages: []api.Message{}})
	snapshotB, _ := json.Marshal(agent.AgentState{SessionID: "session-b", Messages: []api.Message{}})
	ws.setAgentStateForClient(clientA, snapshotA)
	ws.setAgentStateForClient(clientB, snapshotB)

	assertWorkspace := func(clientID, expected string) {
		t.Helper()
		// Resolve symlinks on expected to match server behavior (macOS /var → /private/var)
		if evaled, err := filepath.EvalSymlinks(expected); err == nil {
			expected = evaled
		}
		req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
		req.Header.Set(webClientIDHeader, clientID)
		rec := httptest.NewRecorder()
		ws.handleAPIWorkspace(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("workspace status for %s: %d (%s)", clientID, rec.Code, rec.Body.String())
		}
		var payload struct {
			WorkspaceRoot string `json:"workspace_root"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("workspace decode for %s: %v", clientID, err)
		}
		if payload.WorkspaceRoot != expected {
			t.Fatalf("workspace mismatch for %s: got %q want %q", clientID, payload.WorkspaceRoot, expected)
		}
	}

	assertProviderModel := func(clientID, expectedModel string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
		req.Header.Set(webClientIDHeader, clientID)
		rec := httptest.NewRecorder()
		ws.handleAPIProviders(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("providers status for %s: %d (%s)", clientID, rec.Code, rec.Body.String())
		}
		var payload struct {
			CurrentProvider string `json:"current_provider"`
			CurrentModel    string `json:"current_model"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("providers decode for %s: %v", clientID, err)
		}
		if payload.CurrentModel != expectedModel {
			t.Fatalf("model mismatch for %s: got %q want %q", clientID, payload.CurrentModel, expectedModel)
		}
		if payload.CurrentProvider == "" {
			t.Fatalf("expected non-empty provider for %s", clientID)
		}
	}

	assertSession := func(clientID, expectedSessionID string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/sessions?scope=current", nil)
		req.Header.Set(webClientIDHeader, clientID)
		rec := httptest.NewRecorder()
		ws.handleAPISessions(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("sessions status for %s: %d (%s)", clientID, rec.Code, rec.Body.String())
		}
		var payload struct {
			CurrentSessionID string `json:"current_session_id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("sessions decode for %s: %v", clientID, err)
		}
		if payload.CurrentSessionID != expectedSessionID {
			t.Fatalf("session mismatch for %s: got %q want %q", clientID, payload.CurrentSessionID, expectedSessionID)
		}
	}

	assertWorkspace(clientA, workspaceA)
	assertWorkspace(clientB, workspaceB)
	assertProviderModel(clientA, modelA)
	assertProviderModel(clientB, agentB.GetModel())
	assertSession(clientA, "session-a")
	assertSession(clientB, "session-b")
}

func TestActiveQueryIsolationAllowsOtherWindowWorkspaceSwitch(t *testing.T) {
	daemonRoot := t.TempDir()
	clientAStart := filepath.Join(daemonRoot, "a-start")
	clientBStart := filepath.Join(daemonRoot, "b-start")
	clientATarget := filepath.Join(daemonRoot, "a-target")
	clientBTarget := filepath.Join(daemonRoot, "b-target")
	for _, dir := range []string{clientAStart, clientBStart, clientATarget, clientBTarget} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot
	ws.workspaceRoot = daemonRoot
	ws.terminalManager = NewTerminalManager(daemonRoot)
	ws.fileConsents = newFileConsentManager()

	clientA := "window-a"
	clientB := "window-b"
	if _, err := ws.setClientWorkspaceRoot(clientA, clientAStart); err != nil {
		t.Fatalf("set clientA workspace: %v", err)
	}
	if _, err := ws.setClientWorkspaceRoot(clientB, clientBStart); err != nil {
		t.Fatalf("set clientB workspace: %v", err)
	}

	// Simulate a running query in client A only.
	ws.setClientQueryActive(clientA, true)
	ws.activeQueries = 1

	bodyA, _ := json.Marshal(map[string]string{"path": clientATarget})
	reqA := httptest.NewRequest(http.MethodPost, "/api/workspace", bytes.NewReader(bodyA))
	reqA.Header.Set(webClientIDHeader, clientA)
	recA := httptest.NewRecorder()
	ws.handleAPIWorkspace(recA, reqA)
	if recA.Code != http.StatusConflict {
		t.Fatalf("expected clientA workspace switch to be blocked with 409, got %d: %s", recA.Code, recA.Body.String())
	}

	bodyB, _ := json.Marshal(map[string]string{"path": clientBTarget})
	reqB := httptest.NewRequest(http.MethodPost, "/api/workspace", bytes.NewReader(bodyB))
	reqB.Header.Set(webClientIDHeader, clientB)
	recB := httptest.NewRecorder()
	ws.handleAPIWorkspace(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("expected clientB workspace switch to succeed, got %d: %s", recB.Code, recB.Body.String())
	}

	// Resolve expected paths for comparison (server resolves symlinks via getWorkspaceRootForRequest)
	evaledClientAStart := clientAStart
	if evaled, err := filepath.EvalSymlinks(clientAStart); err == nil {
		evaledClientAStart = evaled
	}
	evaledClientBTarget := clientBTarget
	if evaled, err := filepath.EvalSymlinks(clientBTarget); err == nil {
		evaledClientBTarget = evaled
	}
	if got := ws.getWorkspaceRootForRequest(reqA); got != evaledClientAStart {
		t.Fatalf("clientA workspace should remain unchanged, got %q want %q", got, evaledClientAStart)
	}
	if got := ws.getWorkspaceRootForRequest(reqB); got != evaledClientBTarget {
		t.Fatalf("clientB workspace should move to target, got %q want %q", got, evaledClientBTarget)
	}
}

func TestSetClientWorkspaceRootResetsAgentSessionState(t *testing.T) {
	daemonRoot := t.TempDir()
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	t.Cleanup(func() { credentials.ResetStorageBackend() })
	credentials.ResetStorageBackend()
	startWorkspace := filepath.Join(daemonRoot, "start")
	nextWorkspace := filepath.Join(daemonRoot, "next")
	if err := os.MkdirAll(startWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir start workspace: %v", err)
	}
	if err := os.MkdirAll(nextWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir next workspace: %v", err)
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot
	ws.workspaceRoot = daemonRoot

	clientID := "window-reset"
	if _, err := ws.setClientWorkspaceRoot(clientID, startWorkspace); err != nil {
		t.Fatalf("set start workspace: %v", err)
	}

	agentInst, err := ws.getClientAgent(clientID)
	if err != nil {
		t.Fatalf("get client agent: %v", err)
	}
	if agentInst == nil {
		t.Fatal("expected non-nil agent before workspace reset")
	}

	snapshot, _ := json.Marshal(agent.AgentState{SessionID: "session-before-reset", Messages: []api.Message{{Role: "user", Content: "hello"}}})
	ws.setAgentStateForClient(clientID, snapshot)
	ws.setClientQueryActive(clientID, true)

	if _, err := ws.setClientWorkspaceRoot(clientID, nextWorkspace); err != nil {
		t.Fatalf("set next workspace: %v", err)
	}

	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	ws.mutex.RUnlock()
	if ctx == nil {
		t.Fatal("expected client context after workspace reset")
	}
	if ctx.Agent != nil {
		t.Fatal("expected live agent to be cleared on workspace reset")
	}
	if ctx.CurrentSessionID != "" {
		t.Fatalf("expected current session id to be cleared, got %q", ctx.CurrentSessionID)
	}
	if ctx.ActiveQuery {
		t.Fatal("expected active query flag to be cleared on workspace reset")
	}

	var state agent.AgentState
	if err := json.Unmarshal(ctx.AgentState, &state); err != nil {
		t.Fatalf("decode agent snapshot: %v", err)
	}
	if state.SessionID != "" {
		t.Fatalf("expected empty session id in reset snapshot, got %q", state.SessionID)
	}
	if len(state.Messages) != 0 {
		t.Fatalf("expected empty messages after reset snapshot, got %d", len(state.Messages))
	}
}

func TestShouldForwardEventToConnectionRequiresClientIDExceptGlobal(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	targeted := events.UIEvent{
		Type: events.EventTypeQueryProgress,
		Data: map[string]interface{}{"client_id": "client-a"},
	}
	connInfoA := &ConnectionInfo{ClientID: "client-a", ChatID: ""}
	connInfoB := &ConnectionInfo{ClientID: "client-b", ChatID: ""}
	if !ws.shouldForwardEventToConnection(targeted, connInfoA) {
		t.Fatal("expected targeted event to be forwarded to matching client")
	}
	if ws.shouldForwardEventToConnection(targeted, connInfoB) {
		t.Fatal("expected targeted event to be blocked for non-matching client")
	}

	untargeted := events.UIEvent{
		Type: events.EventTypeQueryProgress,
		Data: map[string]interface{}{"message": "no client metadata"},
	}
	connInfoGeneric := &ConnectionInfo{ClientID: "client-a", ChatID: ""}
	if ws.shouldForwardEventToConnection(untargeted, connInfoGeneric) {
		t.Fatal("expected untargeted non-global event to be blocked")
	}

	global := events.UIEvent{
		Type: events.EventTypeMetricsUpdate,
		Data: map[string]interface{}{"uptime": "1m"},
	}
	if !ws.shouldForwardEventToConnection(global, connInfoGeneric) {
		t.Fatal("expected global metrics update to be forwarded without client metadata")
	}
}

func TestShouldForwardEventToConnectionChatIDFiltering(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Test 1: Event with both client_id and chat_id - should match both
	eventBoth := events.UIEvent{
		Type: events.EventTypeQueryProgress,
		Data: map[string]interface{}{"client_id": "client-a", "chat_id": "chat-1"},
	}
	connMatchingBoth := &ConnectionInfo{ClientID: "client-a", ChatID: "chat-1"}
	connWrongChat := &ConnectionInfo{ClientID: "client-a", ChatID: "chat-2"}
	connUnfiltered := &ConnectionInfo{ClientID: "client-a", ChatID: ""}

	if !ws.shouldForwardEventToConnection(eventBoth, connMatchingBoth) {
		t.Fatal("expected event with client_id and chat_id to match connection with both")
	}
	if ws.shouldForwardEventToConnection(eventBoth, connWrongChat) {
		t.Fatal("expected event with chat_id to be blocked when connection has different chat_id")
	}
	if !ws.shouldForwardEventToConnection(eventBoth, connUnfiltered) {
		t.Fatal("expected event with chat_id to be forwarded to unfiltered connection (no specific chat)")
	}

	// Test 2: Event with only chat_id (no client_id) - should match by chat_id
	eventChatOnly := events.UIEvent{
		Type: events.EventTypeQueryProgress,
		Data: map[string]interface{}{"chat_id": "chat-1"},
	}
	connMatchingChat := &ConnectionInfo{ClientID: "client-b", ChatID: "chat-1"}
	connWrongChatOnly := &ConnectionInfo{ClientID: "client-b", ChatID: "chat-2"}

	if !ws.shouldForwardEventToConnection(eventChatOnly, connMatchingChat) {
		t.Fatal("expected event with chat_id to match connection with same chat_id")
	}
	if ws.shouldForwardEventToConnection(eventChatOnly, connWrongChatOnly) {
		t.Fatal("expected event with chat_id to be blocked when connection has different chat_id")
	}

	// Test 3: Event with neither client_id nor chat_id - only global events allowed
	eventNeither := events.UIEvent{
		Type: events.EventTypeQueryProgress,
		Data: map[string]interface{}{"message": "test"},
	}
	connAny := &ConnectionInfo{ClientID: "client-a", ChatID: "chat-1"}

	if ws.shouldForwardEventToConnection(eventNeither, connAny) {
		t.Fatal("expected event with no client_id or chat_id to be blocked")
	}

	// Test 4: Global event without client_id or chat_id - should be forwarded
	eventGlobal := events.UIEvent{
		Type: events.EventTypeMetricsUpdate,
		Data: map[string]interface{}{"uptime": "1m"},
	}
	if !ws.shouldForwardEventToConnection(eventGlobal, connAny) {
		t.Fatal("expected global metrics update to be forwarded")
	}
}

func TestStopSSHSessionLockedClearsMatchingClientSSHContext(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.clientContexts = map[string]*webClientContext{
		"client-a": {
			WorkspaceRoot:  "/tmp/a",
			SSHHostAlias:   "host-a",
			SSHSessionKey:  "host-a::$HOME",
			SSHLauncherURL: "http://launcher-a",
			SSHHomePath:    "/home/a",
			AgentState:     emptyAgentStateSnapshot(),
		},
		"client-b": {
			WorkspaceRoot:  "/tmp/b",
			SSHHostAlias:   "host-b",
			SSHSessionKey:  "host-b::$HOME",
			SSHLauncherURL: "http://launcher-b",
			SSHHomePath:    "/home/b",
			AgentState:     emptyAgentStateSnapshot(),
		},
	}

	ws.stopSSHSessionLocked("host-a::$HOME")

	ws.mutex.RLock()
	ctxA := ws.clientContexts["client-a"]
	ctxB := ws.clientContexts["client-b"]
	ws.mutex.RUnlock()

	if ctxA == nil || ctxB == nil {
		t.Fatal("expected both client contexts to remain allocated")
	}
	if !reflect.DeepEqual([]string{ctxA.SSHHostAlias, ctxA.SSHSessionKey, ctxA.SSHLauncherURL, ctxA.SSHHomePath}, []string{"", "", "", ""}) {
		t.Fatalf("expected client-a SSH context to be cleared, got host=%q key=%q launcher=%q home=%q", ctxA.SSHHostAlias, ctxA.SSHSessionKey, ctxA.SSHLauncherURL, ctxA.SSHHomePath)
	}
	if ctxB.SSHSessionKey != "host-b::$HOME" {
		t.Fatalf("expected client-b SSH context to remain unchanged, got %q", ctxB.SSHSessionKey)
	}
}

func TestCleanupInactiveClientContextsRemovesOnlyStaleInactiveDisconnectedClients(t *testing.T) {
	daemonRoot := t.TempDir()

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot
	ws.workspaceRoot = daemonRoot

	old := time.Now().Add(-2 * time.Hour)
	recent := time.Now().Add(-5 * time.Minute)

	ws.clientContexts = map[string]*webClientContext{
		defaultWebClientID: {
			WorkspaceRoot: daemonRoot,
			LastSeenAt:    old,
			AgentState:    emptyAgentStateSnapshot(),
		},
		"stale-client": {
			WorkspaceRoot: filepath.Join(daemonRoot, "stale"),
			LastSeenAt:    old,
			Terminal:      NewTerminalManager(daemonRoot),
			AgentState:    emptyAgentStateSnapshot(),
		},
		"recent-client": {
			WorkspaceRoot: filepath.Join(daemonRoot, "recent"),
			LastSeenAt:    recent,
			Terminal:      NewTerminalManager(daemonRoot),
			AgentState:    emptyAgentStateSnapshot(),
		},
		"active-client": {
			WorkspaceRoot: filepath.Join(daemonRoot, "active"),
			LastSeenAt:    old,
			Terminal:      NewTerminalManager(daemonRoot),
			ActiveQuery:   true,
			AgentState:    emptyAgentStateSnapshot(),
		},
		"connected-client": {
			WorkspaceRoot: filepath.Join(daemonRoot, "connected"),
			LastSeenAt:    old,
			Terminal:      NewTerminalManager(daemonRoot),
			AgentState:    emptyAgentStateSnapshot(),
		},
	}

	ws.connections.Store("conn-1", &ConnectionInfo{ClientID: "connected-client", Type: "webui"})

	removed := ws.cleanupInactiveClientContexts(30 * time.Minute)
	if removed != 1 {
		t.Fatalf("expected 1 stale client to be removed, got %d", removed)
	}

	ws.mutex.RLock()
	_, hasDefault := ws.clientContexts[defaultWebClientID]
	_, hasStale := ws.clientContexts["stale-client"]
	_, hasRecent := ws.clientContexts["recent-client"]
	_, hasActive := ws.clientContexts["active-client"]
	_, hasConnected := ws.clientContexts["connected-client"]
	ws.mutex.RUnlock()

	if !hasDefault {
		t.Fatal("default client context should never be removed")
	}
	if hasStale {
		t.Fatal("stale inactive disconnected client should be removed")
	}
	if !hasRecent {
		t.Fatal("recent client should not be removed")
	}
	if !hasActive {
		t.Fatal("active-query client should not be removed")
	}
	if !hasConnected {
		t.Fatal("connected client should not be removed")
	}
}
