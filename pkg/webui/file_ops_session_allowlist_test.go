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

	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestFileOps_AllowlistCoversCreateDeleteRename ensures the bulk of
// the file-mutation API endpoints honor the session folder allowlist.
// Before this audit pass, only handleFileRead/Write consulted it, so
// the user could approve a folder for reads but still couldn't create
// or delete files in it from the browser. The fix routes all
// externally-targeting file ops through ws.allowExternalAccessForRequest.
func TestFileOps_AllowlistCoversCreateDeleteRename(t *testing.T) {
	workspaceRoot := t.TempDir()
	externalDir := t.TempDir()

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	clientID := "test-client"
	ctx := ws.getOrCreateClientContext(clientID)
	ctx.WorkspaceRoot = workspaceRoot
	chat := ctx.getOrCreateChatSession("default")

	// Baseline: create with no allowlist — must be forbidden.
	createBody, _ := json.Marshal(map[string]any{"path": filepath.Join(externalDir, "new.txt"), "type": "file"})
	req := httptest.NewRequest(http.MethodPost, "/api/files/create", bytes.NewReader(createBody))
	req.Header.Set("X-Sprout-Client-ID", clientID)
	rec := httptest.NewRecorder()
	ws.handleAPICreateFile(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("baseline create: expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	// Allowlist the folder via the active chat agent.
	chat.Agent.AddSessionAllowedFolder(externalDir)

	// Create now succeeds.
	req = httptest.NewRequest(http.MethodPost, "/api/files/create", bytes.NewReader(createBody))
	req.Header.Set("X-Sprout-Client-ID", clientID)
	rec = httptest.NewRecorder()
	ws.handleAPICreateFile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("allowlisted create: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	created := filepath.Join(externalDir, "new.txt")
	if _, err := os.Stat(created); err != nil {
		t.Errorf("file was not actually created: %v", err)
	}

	// Rename inside the same allowlisted folder.
	renamed := filepath.Join(externalDir, "renamed.txt")
	renameBody, _ := json.Marshal(map[string]any{"old_path": created, "new_path": renamed})
	req = httptest.NewRequest(http.MethodPost, "/api/files/rename", bytes.NewReader(renameBody))
	req.Header.Set("X-Sprout-Client-ID", clientID)
	rec = httptest.NewRecorder()
	ws.handleAPIRenameItem(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("allowlisted rename: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Delete the renamed file.
	deleteBody, _ := json.Marshal(map[string]any{"path": renamed})
	req = httptest.NewRequest(http.MethodDelete, "/api/files/delete", bytes.NewReader(deleteBody))
	req.Header.Set("X-Sprout-Client-ID", clientID)
	rec = httptest.NewRecorder()
	ws.handleAPIDeleteItem(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("allowlisted delete: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// A path in a DIFFERENT external folder must still be rejected.
	otherDir := t.TempDir()
	otherCreate, _ := json.Marshal(map[string]any{"path": filepath.Join(otherDir, "x.txt"), "type": "file"})
	req = httptest.NewRequest(http.MethodPost, "/api/files/create", bytes.NewReader(otherCreate))
	req.Header.Set("X-Sprout-Client-ID", clientID)
	rec = httptest.NewRecorder()
	ws.handleAPICreateFile(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-allowlisted folder create: expected 403, got %d (allowlist scope leaked!)", rec.Code)
	}
}

// TestListDirectory_AllowlistCovers lists an external folder once
// allowlisted, mirroring the read/write parity check.
func TestListDirectory_AllowlistCovers(t *testing.T) {
	workspaceRoot := t.TempDir()
	externalDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(externalDir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	clientID := "test-client"
	ctx := ws.getOrCreateClientContext(clientID)
	ctx.WorkspaceRoot = workspaceRoot
	chat := ctx.getOrCreateChatSession("default")

	// Baseline: forbidden without allowlist.
	req := httptest.NewRequest(http.MethodGet, "/api/files?dir="+externalDir, nil)
	req.Header.Set("X-Sprout-Client-ID", clientID)
	rec := httptest.NewRecorder()
	ws.handleAPIFiles(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("baseline list: expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	chat.Agent.AddSessionAllowedFolder(externalDir)

	req = httptest.NewRequest(http.MethodGet, "/api/files?dir="+externalDir, nil)
	req.Header.Set("X-Sprout-Client-ID", clientID)
	rec = httptest.NewRecorder()
	ws.handleAPIFiles(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("allowlisted list: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Sanity check the JSON shape — we expect "a.txt" somewhere in the body.
	if !bytes.Contains(rec.Body.Bytes(), []byte("a.txt")) {
		t.Errorf("expected a.txt in directory listing, got: %s", rec.Body.String())
	}
}
