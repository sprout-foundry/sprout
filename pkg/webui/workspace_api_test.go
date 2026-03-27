package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleAPIWorkspaceSetUpdatesWorkspaceRoot(t *testing.T) {
	initialRoot := t.TempDir()
	nextRoot := filepath.Join(initialRoot, "project")
	if err := os.Mkdir(nextRoot, 0o755); err != nil {
		t.Fatalf("mkdir next root: %v", err)
	}
	server := &ReactWebServer{
		daemonRoot:      initialRoot,
		workspaceRoot:   initialRoot,
		terminalManager: NewTerminalManager(initialRoot),
	}

	body, err := json.Marshal(map[string]string{"path": nextRoot})
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/workspace", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleAPIWorkspace(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if got := server.GetWorkspaceRoot(); got != nextRoot {
		t.Fatalf("expected workspace root %q, got %q", nextRoot, got)
	}

	if server.terminalManager == nil {
		t.Fatal("expected terminal manager to be reset")
	}

	if got := server.terminalManager.workspaceRoot; got != nextRoot {
		t.Fatalf("expected terminal manager root %q, got %q", nextRoot, got)
	}
}

func TestHandleAPIWorkspaceGetReturnsCurrentRoot(t *testing.T) {
	root := t.TempDir()
	server := &ReactWebServer{
		daemonRoot:      root,
		workspaceRoot:   root,
		terminalManager: NewTerminalManager(root),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	rec := httptest.NewRecorder()

	server.handleAPIWorkspace(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var response struct {
		DaemonRoot    string `json:"daemon_root"`
		WorkspaceRoot string `json:"workspace_root"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if response.DaemonRoot != filepath.Clean(root) {
		t.Fatalf("expected daemon root %q, got %q", root, response.DaemonRoot)
	}

	if response.WorkspaceRoot != filepath.Clean(root) {
		t.Fatalf("expected workspace root %q, got %q", root, response.WorkspaceRoot)
	}
}

func TestHandleAPIWorkspaceBrowseIsScopedToDaemonRoot(t *testing.T) {
	daemonRoot := t.TempDir()
	childDir := filepath.Join(daemonRoot, "child")
	if err := os.Mkdir(childDir, 0o755); err != nil {
		t.Fatalf("mkdir child dir: %v", err)
	}
	server := &ReactWebServer{
		daemonRoot:      daemonRoot,
		workspaceRoot:   daemonRoot,
		terminalManager: NewTerminalManager(daemonRoot),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workspace/browse?path="+childDir, nil)
	rec := httptest.NewRecorder()
	server.handleAPIWorkspaceBrowse(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	outsideRoot := t.TempDir()
	req = httptest.NewRequest(http.MethodGet, "/api/workspace/browse?path="+outsideRoot, nil)
	rec = httptest.NewRecorder()
	server.handleAPIWorkspaceBrowse(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for path outside daemon root, got %d", rec.Code)
	}
}
