//go:build !js

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

func TestHandleAPIProjectFolderSetUpdatesProjectFolder(t *testing.T) {
	initialRoot := t.TempDir()
	nextRoot := filepath.Join(initialRoot, "project")
	if err := os.Mkdir(nextRoot, 0o755); err != nil {
		t.Fatalf("mkdir next root: %v", err)
	}

	// Resolve symlinks so the expected path matches what filepathAbsEval
	// produces inside handleAPIProjectFolder (macOS /var → /private/var).
	resolvedNext, err := filepath.EvalSymlinks(nextRoot)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	server := &ReactWebServer{
		daemonRoot:      initialRoot,
		projectFolder:   initialRoot,
		terminalManager: NewTerminalManager(initialRoot),
	}

	body, err := json.Marshal(map[string]string{"path": nextRoot})
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/workspace", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleAPIProjectFolder(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if got := server.GetProjectFolder(); got != resolvedNext {
		t.Fatalf("expected project folder %q, got %q", resolvedNext, got)
	}

	if server.terminalManager == nil {
		t.Fatal("expected terminal manager to be reset")
	}

	if got := server.terminalManager.projectFolder; got != resolvedNext {
		t.Fatalf("expected terminal manager root %q, got %q", resolvedNext, got)
	}

}

func TestHandleAPIProjectFolderGetReturnsCurrentRoot(t *testing.T) {
	root := t.TempDir()
	server := &ReactWebServer{
		daemonRoot:      root,
		projectFolder:   root,
		terminalManager: NewTerminalManager(root),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	rec := httptest.NewRecorder()

	server.handleAPIProjectFolder(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var response struct {
		DaemonRoot    string `json:"daemon_root"`
		ProjectFolder string `json:"workspace_root"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if response.DaemonRoot != filepath.Clean(root) {
		t.Fatalf("expected daemon root %q, got %q", root, response.DaemonRoot)
	}

	if response.ProjectFolder != filepath.Clean(root) {
		t.Fatalf("expected project folder %q, got %q", root, response.ProjectFolder)
	}
}

func TestHandleAPIProjectFolderBrowseIsScopedToDaemonRoot(t *testing.T) {
	daemonRoot := t.TempDir()
	childDir := filepath.Join(daemonRoot, "child")
	if err := os.Mkdir(childDir, 0o755); err != nil {
		t.Fatalf("mkdir child dir: %v", err)
	}
	server := &ReactWebServer{
		daemonRoot:      daemonRoot,
		projectFolder:   daemonRoot,
		terminalManager: NewTerminalManager(daemonRoot),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workspace/browse?path="+childDir, nil)
	rec := httptest.NewRecorder()
	server.handleAPIProjectFolderBrowse(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	outsideRoot := t.TempDir()
	req = httptest.NewRequest(http.MethodGet, "/api/workspace/browse?path="+outsideRoot, nil)
	rec = httptest.NewRecorder()
	server.handleAPIProjectFolderBrowse(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for path outside daemon root, got %d", rec.Code)
	}
}

func TestHandleAPIProjectFolderSetRejectsDuringActiveQuery(t *testing.T) {
	root := t.TempDir()
	server := &ReactWebServer{
		daemonRoot:      root,
		projectFolder:   root,
		terminalManager: NewTerminalManager(root),
		activeQueries:   1,
		clientContexts: map[string]*webClientContext{
			"client-a": {
				ProjectFolder: root,
				Terminal:      NewTerminalManager(root),
				FileConsents:  newFileConsentManager(),
				AgentState:    emptyAgentStateSnapshot(),
				ActiveQuery:   true,
			},
		},
	}

	nextRoot := filepath.Join(root, "project")
	if err := os.Mkdir(nextRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"path": nextRoot})
	req := httptest.NewRequest(http.MethodPost, "/api/workspace", bytes.NewReader(body))
	req.Header.Set(webClientIDHeader, "client-a")
	rec := httptest.NewRecorder()

	server.handleAPIProjectFolder(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if response["code"] != "query_in_progress" {
		t.Fatalf("expected code query_in_progress, got %v", response["code"])
	}
	// Project folder should NOT have changed
	if got := server.GetProjectFolder(); got != root {
		t.Fatalf("project folder should not have changed, got %q", got)
	}
}
