//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/automate"
)

// createShellWorkflowFile writes a shell-based workflow JSON (using command
// instead of prompt) into the automate/ directory under the given dir.
func createShellWorkflowFile(dir, name, desc, command string) string {
	automateDir := filepath.Join(dir, "automate")
	if err := os.MkdirAll(automateDir, 0o755); err != nil {
		panic(err)
	}

	raw := map[string]interface{}{
		"initial": map[string]interface{}{
			"command": command,
		},
	}
	if desc != "" {
		raw["description"] = desc
	}

	data, _ := json.MarshalIndent(raw, "", "  ")
	path := filepath.Join(automateDir, name+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		panic(err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Integration: full workflow through the mux (end-to-end)
// ---------------------------------------------------------------------------

// TestAutomateIntegration_FullWorkflow tests the complete automate API flow
// through the mux: discover, list, create, inspect, stop, and verify cleanup.
func TestAutomateIntegration_FullWorkflow(t *testing.T) {
	t.Helper()

	// 1. Setup: create temp dir with automate/ subdirectory and a shell workflow.
	ws, daemonRoot := newAutomateTestServer(t)

	// automate.Discover(automate.Dir()) resolves relative to cwd.
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(daemonRoot)

	createShellWorkflowFile(daemonRoot, "build-check", "Run the project build to verify everything compiles", "make build")

	mux := ws.setupRoutes(context.Background())
	if mux == nil {
		t.Fatal("setupRoutes returned nil")
	}

	// 2. Discover workflows via mux — the build-check workflow should appear.
	req := httptest.NewRequest(http.MethodGet, "/api/automate/workflows", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("workflows: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var workflows []map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&workflows); err != nil {
		t.Fatalf("decode workflows: %v", err)
	}
	if len(workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(workflows))
	}
	if workflows[0]["name"] != "build-check.json" {
		t.Errorf("expected workflow name 'build-check.json', got %q", workflows[0]["name"])
	}
	if workflows[0]["description"] != "Run the project build to verify everything compiles" {
		t.Errorf("unexpected description: %q", workflows[0]["description"])
	}

	// 3. List sessions via mux — should be empty initially.
	req = httptest.NewRequest(http.MethodGet, "/api/automate/sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("sessions list (empty): expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var sessions []sessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected empty session list, got %d items", len(sessions))
	}

	// 4. Create a session file with PID 1 (init, always alive on Unix) to
	// simulate a "running" session without risking the test process itself.
	sproutDir := createSessionFile(daemonRoot, "integ-sess-1", &automate.AutomateSessionInfo{
		Workflow:       "build-check",
		PID:            1,
		StartedAt:      time.Now(),
		Kind:           "automate",
		OutputFilePath: "",
	})

	// 5. List sessions again via mux — should show one running session.
	req = httptest.NewRequest(http.MethodGet, "/api/automate/sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("sessions list (with session): expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	sessions = nil
	if err := json.NewDecoder(rec.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode sessions after create: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Workflow != "build-check" {
		t.Errorf("expected workflow 'build-check', got %q", sessions[0].Workflow)
	}
	if sessions[0].Status != "running" {
		t.Errorf("expected status 'running', got %q", sessions[0].Status)
	}

	// 6. Get single session details via mux.
	req = httptest.NewRequest(http.MethodGet, "/api/automate/sessions/integ-sess-1", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("single session: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var single sessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&single); err != nil {
		t.Fatalf("decode single session: %v", err)
	}
	if single.SessionID != "integ-sess-1" {
		t.Errorf("expected session_id 'integ-sess-1', got %q", single.SessionID)
	}
	if single.Workflow != "build-check" {
		t.Errorf("expected workflow 'build-check', got %q", single.Workflow)
	}
	if single.Status != "running" {
		t.Errorf("expected status 'running', got %q", single.Status)
	}
	if single.PID != 1 {
		t.Errorf("expected PID 1, got %d", single.PID)
	}

	// 7. Stop the session via mux.
	req = httptest.NewRequest(http.MethodPost, "/api/automate/sessions/integ-sess-1/stop", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("stop session: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var stopResp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&stopResp); err != nil {
		t.Fatalf("decode stop response: %v", err)
	}
	if stopResp["status"] != "stopped" {
		t.Errorf("expected status 'stopped', got %q", stopResp["status"])
	}
	if stopResp["session_id"] != "integ-sess-1" {
		t.Errorf("expected session_id 'integ-sess-1', got %q", stopResp["session_id"])
	}

	// 8. List sessions again — should be empty now.
	req = httptest.NewRequest(http.MethodGet, "/api/automate/sessions", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("sessions list (after stop): expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	sessions = nil
	if err := json.NewDecoder(rec.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode sessions after stop: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected empty session list after stop, got %d items", len(sessions))
	}

	// 9. Verify the session file is actually removed from disk.
	_, err := automate.ReadSessionFile(sproutDir, "integ-sess-1")
	if err == nil {
		t.Error("session file should be removed from disk after stop")
	}

	// 10. Verify the single session endpoint now returns 404.
	req = httptest.NewRequest(http.MethodGet, "/api/automate/sessions/integ-sess-1", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("single session after stop: expected 404, got %d", rec.Code)
	}
}
