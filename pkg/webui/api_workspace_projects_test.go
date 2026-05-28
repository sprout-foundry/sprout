//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func resetProjectsCache() {
	projectsCache = nil
	projectsCacheTime = time.Time{}
}

type projectsResponse struct {
	Projects   []ProjectInfo `json:"projects"`
	DaemonRoot string        `json:"daemon_root"`
	Cached     bool          `json:"cached"`
}

func TestHandleAPIWorkspaceProjects_Success(t *testing.T) {
	resetProjectsCache()
	t.Cleanup(resetProjectsCache)

	daemonRoot := t.TempDir()

	// Create a subdirectory with project markers so FindProjectsInDirectory finds it
	projectDir := filepath.Join(daemonRoot, "myproject")
	if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to create test project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module example.com/myproject\n"), 0644); err != nil {
		t.Fatalf("failed to create go.mod: %v", err)
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot

	req := httptest.NewRequest(http.MethodGet, "/api/workspace/projects", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspaceProjects(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp projectsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}

	if resp.DaemonRoot == "" {
		t.Error("expected daemon_root in response")
	}

	if len(resp.Projects) == 0 {
		t.Error("expected at least one project in the projects array")
	}

	// Also check for the "myproject" directory being detected
	found := false
	for _, p := range resp.Projects {
		if p.Name == "myproject" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'myproject' in projects list")
	}

	// The daemon root itself may also be detected as a project if it has markers
	// (unlikely for a temp dir, but the handler also checks IsProjectDirectory on daemonRoot)
}

func TestHandleAPIWorkspaceProjects_EmptyResults(t *testing.T) {
	resetProjectsCache()
	t.Cleanup(resetProjectsCache)

	daemonRoot := t.TempDir()
	// Leave the directory empty — no project markers, no subdirectories

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot

	req := httptest.NewRequest(http.MethodGet, "/api/workspace/projects", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspaceProjects(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp projectsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}

	// Verify the raw JSON contains the "projects" key (even if its value is null)
	var raw map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("expected JSON object, got error: %v", err)
	}
	if _, ok := raw["projects"]; !ok {
		t.Error("expected 'projects' key in JSON response, even when no projects exist")
	}

	if resp.DaemonRoot == "" {
		t.Error("expected daemon_root in response")
	}

	if resp.Cached {
		t.Error("first request should not be cached")
	}
}

func TestHandleAPIWorkspaceProjects_MethodNotAllowed(t *testing.T) {
	resetProjectsCache()
	t.Cleanup(resetProjectsCache)

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/workspace/projects", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspaceProjects(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST, got %d", rec.Code)
	}
}

func TestHandleAPIWorkspaceProjects_Caching(t *testing.T) {
	resetProjectsCache()
	t.Cleanup(resetProjectsCache)

	daemonRoot := t.TempDir()

	// Create a subdirectory with project markers
	projectDir := filepath.Join(daemonRoot, "cachedproject")
	if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to create test project dir: %v", err)
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot

	// First request — should NOT be cached
	req1 := httptest.NewRequest(http.MethodGet, "/api/workspace/projects", nil)
	rec1 := httptest.NewRecorder()
	ws.handleAPIWorkspaceProjects(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d: %s", rec1.Code, rec1.Body.String())
	}

	var resp1 projectsResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("first request: expected JSON response, got error: %v", err)
	}

	if resp1.Cached {
		t.Error("first request should have cached=false")
	}

	// Second request — should be cached
	req2 := httptest.NewRequest(http.MethodGet, "/api/workspace/projects", nil)
	rec2 := httptest.NewRecorder()
	ws.handleAPIWorkspaceProjects(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var resp2 projectsResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("second request: expected JSON response, got error: %v", err)
	}

	if !resp2.Cached {
		t.Error("second request should have cached=true")
	}

	if !reflect.DeepEqual(resp1.Projects, resp2.Projects) {
		t.Error("cached and non-cached responses should return identical projects")
	}
}
