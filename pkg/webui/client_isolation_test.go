package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/events"
)

func TestMultiWindowClientIsolationForWorkspaceSessionAndModel(t *testing.T) {
	daemonRoot := t.TempDir()
	workspaceA := filepath.Join(daemonRoot, "workspace-a")
	workspaceB := filepath.Join(daemonRoot, "workspace-b")
	if err := os.MkdirAll(workspaceA, 0o755); err != nil {
		t.Fatalf("mkdir workspaceA: %v", err)
	}
	if err := os.MkdirAll(workspaceB, 0o755); err != nil {
		t.Fatalf("mkdir workspaceB: %v", err)
	}

	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
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

	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
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

	if got := ws.getWorkspaceRootForRequest(reqA); got != clientAStart {
		t.Fatalf("clientA workspace should remain unchanged, got %q want %q", got, clientAStart)
	}
	if got := ws.getWorkspaceRootForRequest(reqB); got != clientBTarget {
		t.Fatalf("clientB workspace should move to target, got %q want %q", got, clientBTarget)
	}
}
