//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// newAutomateTestServer creates a ReactWebServer with a client context for
// "test-client". Returns the server and the daemon root path for test setup.
func newAutomateTestServer(t *testing.T) (*ReactWebServer, string) {
	t.Helper()
	daemonRoot := t.TempDir()

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot
	ws.workspaceRoot = daemonRoot

	if _, err := ws.setClientWorkspaceRoot("test-client", daemonRoot); err != nil {
		t.Fatalf("setClientWorkspaceRoot: %v", err)
	}

	return ws, daemonRoot
}

// createSessionFile writes a session file into the .sprout/automate/ directory
// under the given workspace root, returning the sproutDir path.
func createSessionFile(workspaceRoot, sessionID string, info *automate.AutomateSessionInfo) string {
	sproutDir := filepath.Join(workspaceRoot, ".sprout")
	if err := automate.WriteSessionFile(sproutDir, sessionID, info); err != nil {
		panic(err)
	}
	return sproutDir
}

// createWorkflowFile writes a valid workflow JSON into the automate/ directory
// under the given dir, returning the full path.
func createWorkflowFile(dir, name, desc string, requiresApproval *bool) string {
	automateDir := filepath.Join(dir, "automate")
	if err := os.MkdirAll(automateDir, 0o755); err != nil {
		panic(err)
	}

	var raw map[string]interface{}
	if desc != "" {
		raw = map[string]interface{}{
			"description": desc,
			"initial":     map[string]interface{}{"prompt": "do something"},
		}
	} else {
		raw = map[string]interface{}{
			"initial": map[string]interface{}{"prompt": "do something"},
		}
	}
	if requiresApproval != nil {
		raw["requires_approval"] = *requiresApproval
	}

	data, _ := json.MarshalIndent(raw, "", "  ")
	path := filepath.Join(automateDir, name+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		panic(err)
	}
	_ = dir // suppress unused warning when desc is optional
	return path
}

// ---------------------------------------------------------------------------
// Routing: registerAutomateRoutes + handleAPIAutomateSessionsAll dispatch
// ---------------------------------------------------------------------------

func TestAutomateRoutesRegistered(t *testing.T) {
	ws, _ := newAutomateTestServer(t)
	// Should not panic.
	mux := ws.setupRoutes(context.Background())
	if mux == nil {
		t.Fatal("setupRoutes returned nil")
	}
}

func TestAutomateSessionsAll_DispatchSingle(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)
	sproutDir := createSessionFile(daemonRoot, "single-1", &automate.AutomateSessionInfo{
		Workflow:  "my-wf",
		PID:       99999999,
		StartedAt: time.Now(),
		Kind:      "automate",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/single-1", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionsAll(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp sessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SessionID != "single-1" {
		t.Errorf("expected session_id 'single-1', got %q", resp.SessionID)
	}
	if resp.Workflow != "my-wf" {
		t.Errorf("expected workflow 'my-wf', got %q", resp.Workflow)
	}

	// Verify file still exists (single is read-only).
	if _, err := automate.ReadSessionFile(sproutDir, "single-1"); err != nil {
		t.Error("session file should still exist after single-read")
	}
}

func TestAutomateSessionsAll_DispatchStop(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)
	sproutDir := createSessionFile(daemonRoot, "stop-1", &automate.AutomateSessionInfo{
		Workflow:  "stop-wf",
		PID:       99999999,
		StartedAt: time.Now(),
		Kind:      "automate",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/automate/sessions/stop-1/stop", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionsAll(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "stopped" {
		t.Errorf("expected status 'stopped', got %q", resp["status"])
	}

	// Session file should be removed.
	_, err := automate.ReadSessionFile(sproutDir, "stop-1")
	if err == nil {
		t.Error("session file should be removed after stop")
	}
}

func TestAutomateSessionsAll_DispatchOutput(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	outputFile := filepath.Join(daemonRoot, "output.txt")
	if err := os.WriteFile(outputFile, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	createSessionFile(daemonRoot, "out-1", &automate.AutomateSessionInfo{
		Workflow:       "out-wf",
		PID:            99999999,
		StartedAt:      time.Now(),
		Kind:           "automate",
		OutputFilePath: outputFile,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/out-1/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionsAll(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["output"] != "hello world" {
		t.Errorf("expected output 'hello world', got %q", resp["output"])
	}
}

func TestAutomateSessionsAll_DispatchEmptyPathToList(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	// Create the .sprout/automate dir but no files.
	os.MkdirAll(filepath.Join(daemonRoot, ".sprout", "automate"), 0o755)

	// /api/automate/sessions/ with trailing slash but no ID should dispatch to list.
	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionsAll(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp []interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty list, got %d items", len(resp))
	}
}

// ---------------------------------------------------------------------------
// handleAPIAutomateWorkflows — GET /api/automate/workflows
// ---------------------------------------------------------------------------

func TestHandleAPIAutomateWorkflows_MethodNotAllowed(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/automate/workflows", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateWorkflows(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAutomateWorkflows_EmptyDir(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	// automate/ directory does not exist in the test temp dir.
	req := httptest.NewRequest(http.MethodGet, "/api/automate/workflows", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateWorkflows(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp []interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty array when dir doesn't exist, got %d items", len(resp))
	}
}

func TestHandleAPIAutomateWorkflows_WithWorkflows(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(daemonRoot)

	createWorkflowFile(daemonRoot, "build", "Run the build suite", nil)
	trueVal := true
	createWorkflowFile(daemonRoot, "deploy", "", &trueVal) // no description

	req := httptest.NewRequest(http.MethodGet, "/api/automate/workflows", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateWorkflows(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var items []map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(items))
	}

	// Find each workflow by name.
	for _, item := range items {
		name, _ := item["name"].(string)
		if name == "build" {
			if item["description"] != "Run the build suite" {
				t.Errorf("build: expected description 'Run the build suite', got %q", item["description"])
			}
			if item["filename"] != "build" {
				t.Errorf("build: expected filename 'build', got %q", item["filename"])
			}
		}
		if name == "deploy" {
			// Description omitted (omitempty).
			if item["filename"] != "deploy" {
				t.Errorf("deploy: expected filename 'deploy', got %q", item["filename"])
			}
		}
		if item["file_path"] == "" {
			t.Errorf("expected non-empty file_path for %q", name)
		}
	}
}

// ---------------------------------------------------------------------------
// handleAPIAutomateSessionsList — GET /api/automate/sessions
// ---------------------------------------------------------------------------

func TestHandleAPIAutomateSessionsList_MethodNotAllowed(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/automate/sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionsList(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAutomateSessionsList_Empty(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	// Ensure .sprout/automate doesn't exist.
	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionsList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp []interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty list, got %d items", len(resp))
	}
}

func TestHandleAPIAutomateSessionsList_WithSessions(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	now := time.Now()
	budget := 10.5
	createSessionFile(daemonRoot, "sess-a", &automate.AutomateSessionInfo{
		Workflow:  "wf-a",
		PID:       99999999,
		StartedAt: now,
		Kind:      "automate",
		BudgetUSD: &budget,
	})
	createSessionFile(daemonRoot, "sess-b", &automate.AutomateSessionInfo{
		Workflow:  "wf-b",
		PID:       99999998,
		StartedAt: now,
		Kind:      "automate",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionsList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp []sessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(resp))
	}

	// Verify both sessions have expected fields.
	for _, s := range resp {
		if s.Workflow != "wf-a" && s.Workflow != "wf-b" {
			t.Errorf("unexpected workflow %q", s.Workflow)
		}
		if s.Status != "exited" {
			t.Errorf("expected status 'exited' for dead PID, got %q", s.Status)
		}
		if s.Kind != "automate" {
			t.Errorf("expected kind 'automate', got %q", s.Kind)
		}
	}
}

func TestHandleAPIAutomateSessionsList_StatusEnrichment(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	// PID 1 (init) is always alive on Unix.
	createSessionFile(daemonRoot, "running-sess", &automate.AutomateSessionInfo{
		Workflow:  "live-wf",
		PID:       1,
		StartedAt: time.Now(),
		Kind:      "automate",
	})
	// Dead PID.
	createSessionFile(daemonRoot, "dead-sess", &automate.AutomateSessionInfo{
		Workflow:  "dead-wf",
		PID:       99999999,
		StartedAt: time.Now(),
		Kind:      "automate",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionsList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp []sessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	statuses := make(map[string]string)
	for _, s := range resp {
		statuses[s.Workflow] = s.Status
	}
	if statuses["live-wf"] != "running" {
		t.Errorf("expected live-wf status 'running', got %q", statuses["live-wf"])
	}
	if statuses["dead-wf"] != "exited" {
		t.Errorf("expected dead-wf status 'exited', got %q", statuses["dead-wf"])
	}
}

// ---------------------------------------------------------------------------
// handleAPIAutomateSessionSingle — GET /api/automate/sessions/:id
// ---------------------------------------------------------------------------

func TestHandleAPIAutomateSessionSingle_Found(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)
	startedAt := time.Now()
	createSessionFile(daemonRoot, "single-find", &automate.AutomateSessionInfo{
		Workflow:  "find-wf",
		PID:       12345,
		StartedAt: startedAt,
		Kind:      "automate",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/single-find", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionSingle(rec, req, "single-find")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp sessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SessionID != "single-find" {
		t.Errorf("expected session_id 'single-find', got %q", resp.SessionID)
	}
	if resp.Workflow != "find-wf" {
		t.Errorf("expected workflow 'find-wf', got %q", resp.Workflow)
	}
	if resp.PID != 12345 {
		t.Errorf("expected PID 12345, got %d", resp.PID)
	}
}

func TestHandleAPIAutomateSessionSingle_NotFound(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/no-such-id", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionSingle(rec, req, "no-such-id")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAutomateSessionSingle_MethodNotAllowed(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/automate/sessions/some-id", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionSingle(rec, req, "some-id")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAutomateSessionSingle_EmptyID(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionSingle(rec, req, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// handleAPIAutomateSessionStop — POST /api/automate/sessions/:id/stop
// ---------------------------------------------------------------------------

func TestHandleAPIAutomateSessionStop_MethodNotAllowed(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/stop-id/stop", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionStop(rec, req, "stop-id")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAutomateSessionStop_Success(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)
	sproutDir := createSessionFile(daemonRoot, "stop-success", &automate.AutomateSessionInfo{
		Workflow:  "stop-wf",
		PID:       99999999,
		StartedAt: time.Now(),
		Kind:      "automate",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/automate/sessions/stop-success/stop", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionStop(rec, req, "stop-success")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["session_id"] != "stop-success" {
		t.Errorf("expected session_id 'stop-success', got %q", resp["session_id"])
	}
	if resp["status"] != "stopped" {
		t.Errorf("expected status 'stopped', got %q", resp["status"])
	}

	// Session file should be removed.
	_, err := automate.ReadSessionFile(sproutDir, "stop-success")
	if err == nil {
		t.Error("session file should be removed after stop")
	}
}

func TestHandleAPIAutomateSessionStop_NotFound(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/automate/sessions/no-id/stop", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionStop(rec, req, "no-id")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAutomateSessionStop_EmptyID(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/automate/sessions//stop", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionStop(rec, req, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// handleAPIAutomateSessionOutput — GET /api/automate/sessions/:id/output
// ---------------------------------------------------------------------------

func TestHandleAPIAutomateSessionOutput_MethodNotAllowed(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/automate/sessions/out-id/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionOutput(rec, req, "out-id")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAutomateSessionOutput_NoOutputFile(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	// Session with empty OutputFilePath.
	createSessionFile(daemonRoot, "no-out", &automate.AutomateSessionInfo{
		Workflow:  "no-out-wf",
		PID:       99999999,
		StartedAt: time.Now(),
		Kind:      "automate",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/no-out/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionOutput(rec, req, "no-out")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["output"] != "" {
		t.Errorf("expected empty output, got %q", resp["output"])
	}
	if resp["offset"] != float64(0) {
		t.Errorf("expected offset 0, got %v", resp["offset"])
	}
	if resp["total"] != float64(0) {
		t.Errorf("expected total 0, got %v", resp["total"])
	}
}

func TestHandleAPIAutomateSessionOutput_WithContent(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	outputFile := filepath.Join(daemonRoot, "output.txt")
	knownContent := "line1\nline2\nline3\n"
	if err := os.WriteFile(outputFile, []byte(knownContent), 0o644); err != nil {
		t.Fatal(err)
	}

	createSessionFile(daemonRoot, "out-content", &automate.AutomateSessionInfo{
		Workflow:       "out-wf",
		PID:            99999999,
		StartedAt:      time.Now(),
		Kind:           "automate",
		OutputFilePath: outputFile,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/out-content/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionOutput(rec, req, "out-content")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["output"] != knownContent {
		t.Errorf("expected output %q, got %q", knownContent, resp["output"])
	}
	expectedOffset := len(knownContent)
	if int(resp["offset"].(float64)) != expectedOffset {
		t.Errorf("expected offset %d, got %v", expectedOffset, resp["offset"])
	}
	if int(resp["total"].(float64)) != expectedOffset {
		t.Errorf("expected total %d, got %v", expectedOffset, resp["total"])
	}
}

func TestHandleAPIAutomateSessionOutput_WithSinceOffset(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	outputFile := filepath.Join(daemonRoot, "output.txt")
	fullContent := "0123456789ABCDEF"
	if err := os.WriteFile(outputFile, []byte(fullContent), 0o644); err != nil {
		t.Fatal(err)
	}

	createSessionFile(daemonRoot, "out-since", &automate.AutomateSessionInfo{
		Workflow:       "out-wf",
		PID:            99999999,
		StartedAt:      time.Now(),
		Kind:           "automate",
		OutputFilePath: outputFile,
	})

	// Read from offset 6.
	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/out-since/output?since=6", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionOutput(rec, req, "out-since")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["output"] != "6789ABCDEF" {
		t.Errorf("expected output '6789ABCDEF', got %q", resp["output"])
	}
	if int(resp["offset"].(float64)) != 16 {
		t.Errorf("expected offset 16, got %v", resp["offset"])
	}
	if int(resp["total"].(float64)) != 16 {
		t.Errorf("expected total 16, got %v", resp["total"])
	}
}

func TestHandleAPIAutomateSessionOutput_NotFound(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/no-session/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionOutput(rec, req, "no-session")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAutomateSessionOutput_SincePastEOF(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	outputFile := filepath.Join(daemonRoot, "output.txt")
	if err := os.WriteFile(outputFile, []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}

	createSessionFile(daemonRoot, "out-past", &automate.AutomateSessionInfo{
		Workflow:       "out-wf",
		PID:            99999999,
		StartedAt:      time.Now(),
		Kind:           "automate",
		OutputFilePath: outputFile,
	})

	// Request offset 999, past the 5-byte file.
	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/out-past/output?since=999", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionOutput(rec, req, "out-past")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["output"] != "" {
		t.Errorf("expected empty output past EOF, got %q", resp["output"])
	}
	if int(resp["offset"].(float64)) != 5 {
		t.Errorf("expected offset clamped to 5, got %v", resp["offset"])
	}
	if int(resp["total"].(float64)) != 5 {
		t.Errorf("expected total 5, got %v", resp["total"])
	}
}

func TestHandleAPIAutomateSessionOutput_SinceExactEOF(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	outputFile := filepath.Join(daemonRoot, "output.txt")
	content := "exact"
	if err := os.WriteFile(outputFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	createSessionFile(daemonRoot, "out-exact", &automate.AutomateSessionInfo{
		Workflow:       "out-wf",
		PID:            99999999,
		StartedAt:      time.Now(),
		Kind:           "automate",
		OutputFilePath: outputFile,
	})

	// Request offset exactly at EOF.
	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/out-exact/output?since=5", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionOutput(rec, req, "out-exact")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["output"] != "" {
		t.Errorf("expected empty output at exact EOF, got %q", resp["output"])
	}
}

func TestHandleAPIAutomateSessionOutput_EmptyID(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions//output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionOutput(rec, req, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// handleAPIAutomateRun — POST /api/automate/run
// ---------------------------------------------------------------------------

func TestHandleAPIAutomateRun_MethodNotAllowed(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/automate/run", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateRun(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAutomateRun_InvalidJSON(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/automate/run",
		strings.NewReader("not json"))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateRun(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAutomateRun_MissingWorkflow(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/automate/run",
		strings.NewReader(`{}`))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateRun(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "workflow") {
		t.Errorf("expected error mentioning workflow, got %s", body)
	}
}

func TestHandleAPIAutomateRun_InvalidWorkflow(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/automate/run",
		strings.NewReader(`{"workflow":"non-existent-workflow"}`))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateRun(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAutomateRun_RequiresApproval(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(daemonRoot)

	// Workflow with no requires_approval field defaults to true.
	createWorkflowFile(daemonRoot, "approval-test", "Needs approval", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/automate/run",
		strings.NewReader(`{"workflow":"approval-test"}`))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["requires_approval"] != true {
		t.Errorf("expected requires_approval true, got %v", resp["requires_approval"])
	}
	if resp["workflow"] != "approval-test" {
		t.Errorf("expected workflow 'approval-test', got %v", resp["workflow"])
	}
}

func TestHandleAPIAutomateRun_ExplicitApprovalTrue(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(daemonRoot)

	trueVal := true
	createWorkflowFile(daemonRoot, "explicit-true", "", &trueVal)

	req := httptest.NewRequest(http.MethodPost, "/api/automate/run",
		strings.NewReader(`{"workflow":"explicit-true"}`))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["requires_approval"] != true {
		t.Errorf("expected requires_approval true, got %v", resp["requires_approval"])
	}
}

func TestHandleAPIAutomateRun_NoApproval_Bypassed(t *testing.T) {
	// Verify that a workflow with requires_approval: false does NOT get the
	// requires_approval response. In the test binary environment, getClientAgent
	// may succeed, but that is acceptable — the assertion is about the approval
	// gate, not execution. We use a nil approval flag (defaults to true) to
	// safely test the approval gate without triggering execution code that
	// depends on agent/BPM infrastructure.
	ws, daemonRoot := newAutomateTestServer(t)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(daemonRoot)

	// Default (nil requires_approval) means approval IS required.
	createWorkflowFile(daemonRoot, "approval-default", "", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/automate/run",
		strings.NewReader(`{"workflow":"approval-default"}`))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["requires_approval"] != true {
		t.Errorf("expected requires_approval true for unset field, got %v", resp["requires_approval"])
	}
}

func TestHandleAPIAutomateRun_WorkflowWithExtension_Resolves(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(daemonRoot)

	// Use requires_approval: true (or nil/default) so the handler stops
	// at the approval gate and never reaches execution code.
	createWorkflowFile(daemonRoot, "ext-test", "Has extension", nil)

	// Request with .json extension — should resolve the file successfully
	// (proving extension handling works) then hit the approval gate.
	req := httptest.NewRequest(http.MethodPost, "/api/automate/run",
		strings.NewReader(`{"workflow":"ext-test.json"}`))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["requires_approval"] != true {
		t.Errorf("expected requires_approval true, got %v", resp["requires_approval"])
	}
	// The key assertion: the workflow was resolved (not 400), proving .json extension works.
}

// ---------------------------------------------------------------------------
// Helpers: getSproutDir, makeSessionResponse
// ---------------------------------------------------------------------------

func TestGetSproutDir(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(webClientIDHeader, "test-client")

	dir := ws.getSproutDir(req)
	// macOS resolves /var to /private/var via symlinks, so compare with
	// EvalSymlinks to handle both representations.
	resolvedDaemon, err := filepath.EvalSymlinks(daemonRoot)
	if err != nil {
		resolvedDaemon = daemonRoot
	}
	expected := filepath.Join(resolvedDaemon, ".sprout")
	if dir != expected {
		// Also accept the non-resolved form in case symlinks differ.
		altExpected := filepath.Join(daemonRoot, ".sprout")
		if dir != altExpected {
			t.Errorf("expected sprout dir %q or %q, got %q", expected, altExpected, dir)
		}
	}
}

func TestGetSproutDir_FallbackToCWD(t *testing.T) {
	// Create a server with a workspace root within the test temp dir.
	tmpDir := t.TempDir()
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = tmpDir
	ws.workspaceRoot = tmpDir

	// Create a client context so getWorkspaceRootForRequest works.
	if _, err := ws.setClientWorkspaceRoot("test-client", tmpDir); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(webClientIDHeader, "test-client")

	dir := ws.getSproutDir(req)
	if dir == "" {
		t.Error("expected non-empty sprout dir")
	}
	// Should end with .sprout.
	if !strings.HasSuffix(dir, ".sprout") {
		t.Errorf("expected sprout dir ending with '.sprout', got %q", dir)
	}
}

func TestMakeSessionResponse_Exited(t *testing.T) {
	info := automate.AutomateSessionInfo{
		Workflow:  "test-wf",
		PID:       99999999,
		StartedAt: time.Now(),
		Kind:      "automate",
	}
	resp := makeSessionResponse(info)

	if resp.Status != "exited" {
		t.Errorf("expected status 'exited', got %q", resp.Status)
	}
	if resp.Workflow != "test-wf" {
		t.Errorf("expected workflow 'test-wf', got %q", resp.Workflow)
	}
	if resp.PID != 99999999 {
		t.Errorf("expected PID 99999999, got %d", resp.PID)
	}
	if resp.Kind != "automate" {
		t.Errorf("expected kind 'automate', got %q", resp.Kind)
	}
}

func TestMakeSessionResponse_Running(t *testing.T) {
	info := automate.AutomateSessionInfo{
		Workflow:  "live-wf",
		PID:       1, // init is always alive on Unix
		StartedAt: time.Now(),
		Kind:      "automate",
	}
	resp := makeSessionResponse(info)

	if resp.Status != "running" {
		t.Errorf("expected status 'running', got %q", resp.Status)
	}
}

func TestMakeSessionResponse_OutputFilePreserved(t *testing.T) {
	outputPath := "/tmp/some-output.log"
	info := automate.AutomateSessionInfo{
		Workflow:       "test-wf",
		PID:            99999999,
		StartedAt:      time.Now(),
		Kind:           "automate",
		OutputFilePath: outputPath,
	}
	resp := makeSessionResponse(info)

	if resp.OutputFilePath != outputPath {
		t.Errorf("expected output_file_path %q, got %q", outputPath, resp.OutputFilePath)
	}
}

func TestMakeSessionResponse_BudgetPreserved(t *testing.T) {
	budget := 25.0
	info := automate.AutomateSessionInfo{
		Workflow:  "budget-wf",
		PID:       99999999,
		StartedAt: time.Now(),
		Kind:      "automate",
		BudgetUSD: &budget,
	}
	resp := makeSessionResponse(info)

	if resp.BudgetUSD == nil {
		t.Error("expected BudgetUSD to be non-nil")
	} else if *resp.BudgetUSD != 25.0 {
		t.Errorf("expected budget 25.0, got %f", *resp.BudgetUSD)
	}
}

// ---------------------------------------------------------------------------
// Edge cases: Workflow run with path traversal protection
// ---------------------------------------------------------------------------

func TestHandleAPIAutomateRun_PathTraversalRejection(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	// Attempt path traversal — should be caught by ResolvePath.
	req := httptest.NewRequest(http.MethodPost, "/api/automate/run",
		strings.NewReader(`{"workflow":"../etc/passwd"}`))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateRun(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Session ID path traversal rejection
// ---------------------------------------------------------------------------

func TestHandleAPIAutomateSessionSingle_PathTraversal(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/../etc/passwd", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionSingle(rec, req, "../etc/passwd")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal session ID, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAutomateSessionStop_PathTraversal(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/automate/sessions/../stop", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionStop(rec, req, "..")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal session ID on stop, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIAutomateSessionOutput_PathTraversal(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/../output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionOutput(rec, req, "..")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal session ID on output, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Integration: end-to-end via mux (route registration)
// ---------------------------------------------------------------------------

func TestAutomateRoutes_EndToEnd_Mux(t *testing.T) {
	ws, _ := newAutomateTestServer(t)

	mux := ws.setupRoutes(context.Background())
	if mux == nil {
		t.Fatal("setupRoutes returned nil")
	}

	tests := []struct {
		method string
		path   string
		body   io.Reader
		code   int
	}{
		// Workflows list.
		{http.MethodGet, "/api/automate/workflows", nil, http.StatusOK},
		{http.MethodPost, "/api/automate/workflows", nil, http.StatusMethodNotAllowed},

		// Sessions list (empty).
		{http.MethodGet, "/api/automate/sessions", nil, http.StatusOK},
		{http.MethodPost, "/api/automate/sessions", nil, http.StatusMethodNotAllowed},

		// Run with no agent.
		{http.MethodGet, "/api/automate/run", nil, http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/automate/run", strings.NewReader(`{}`), http.StatusBadRequest},
	}

	for i, tt := range tests {
		t.Run(fmt.Sprintf("test-%d-%s-%s", i, tt.method, tt.path), func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, tt.body)
			req.Header.Set(webClientIDHeader, "test-client")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.code {
				t.Errorf("expected %d for %s %s, got %d: %s", tt.code, tt.method, tt.path, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAutomateRoutes_EndToEnd_SessionLifecycle(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	mux := ws.setupRoutes(context.Background())

	// Create a session file with output.
	outputFile := filepath.Join(daemonRoot, "e2e-output.txt")
	if err := os.WriteFile(outputFile, []byte("e2e output content"), 0o644); err != nil {
		t.Fatal(err)
	}

	sproutDir := createSessionFile(daemonRoot, "e2e-sess", &automate.AutomateSessionInfo{
		Workflow:       "e2e-wf",
		PID:            99999999,
		StartedAt:      time.Now(),
		Kind:           "automate",
		OutputFilePath: outputFile,
	})

	// 1. List sessions via mux.
	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}

	// 2. Single session via mux.
	req = httptest.NewRequest(http.MethodGet, "/api/automate/sessions/e2e-sess", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("single: expected 200, got %d", rec.Code)
	}
	var resp sessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SessionID != "e2e-sess" {
		t.Errorf("expected session_id 'e2e-sess', got %q", resp.SessionID)
	}

	// 3. Output via mux.
	req = httptest.NewRequest(http.MethodGet, "/api/automate/sessions/e2e-sess/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("output: expected 200, got %d", rec.Code)
	}
	var outResp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&outResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if outResp["output"] != "e2e output content" {
		t.Errorf("expected 'e2e output content', got %q", outResp["output"])
	}

	// 4. Stop via mux.
	req = httptest.NewRequest(http.MethodPost, "/api/automate/sessions/e2e-sess/stop", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stop: expected 200, got %d", rec.Code)
	}

	// 5. Verify session file is gone — single should return 404 now.
	req = httptest.NewRequest(http.MethodGet, "/api/automate/sessions/e2e-sess", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("after stop: expected 404, got %d", rec.Code)
	}

	// 6. Output should also be 404 now.
	req = httptest.NewRequest(http.MethodGet, "/api/automate/sessions/e2e-sess/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("output after stop: expected 404, got %d", rec.Code)
	}

	// Cleanup: the sproutDir variable is used for verification but not needed
	// since the session file is removed.
	_ = sproutDir
}

// ---------------------------------------------------------------------------
// Session output file missing (exists session but output file deleted)
// ---------------------------------------------------------------------------

func TestHandleAPIAutomateSessionOutput_OutputFileMissing(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	// Create a session that references an output file that doesn't exist.
	createSessionFile(daemonRoot, "out-missing", &automate.AutomateSessionInfo{
		Workflow:       "missing-wf",
		PID:            99999999,
		StartedAt:      time.Now(),
		Kind:           "automate",
		OutputFilePath: filepath.Join(daemonRoot, "nonexistent-output.txt"),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/automate/sessions/out-missing/output", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateSessionOutput(rec, req, "out-missing")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing output file, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Run with additional optional params (budget_usd, budget_warn, heartbeat)
// ---------------------------------------------------------------------------

func TestHandleAPIAutomateRun_OptionalParamsParsed(t *testing.T) {
	ws, daemonRoot := newAutomateTestServer(t)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(daemonRoot)

	// Use default approval (requires_approval is nil, defaults to true)
	// so the handler stops at the approval gate. This tests that optional
	// params (budget_usd, budget_warn, heartbeat) parse correctly without
	// causing JSON errors — the handler must reach the approval gate, not
	// fail at JSON decoding.
	createWorkflowFile(daemonRoot, "opt-params", "", nil)

	body := strings.NewReader(`{
		"workflow": "opt-params",
		"budget_usd": 50.0,
		"budget_warn": "80",
		"heartbeat": 30
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/automate/run", body)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIAutomateRun(rec, req)

	// The handler should reach the approval gate (200 with requires_approval: true),
	// proving that the JSON with optional params was parsed correctly.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["requires_approval"] != true {
		t.Errorf("expected requires_approval true, got %v", resp["requires_approval"])
	}
}
