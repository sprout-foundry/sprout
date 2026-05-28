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

func TestHandleAPIFileCheckModified(t *testing.T) {
	t.Run("non-POST returns 405", func(t *testing.T) {
		server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
		req := httptest.NewRequest(http.MethodGet, "/api/file/check-modified", nil)
		rec := httptest.NewRecorder()

		server.handleAPIFileCheckModified(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405, got %d", rec.Code)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		dir := t.TempDir()
		server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
		server.workspaceRoot = dir
		// Ensure client context gets the right workspaceRoot
		server.getOrCreateClientContext(defaultWebClientID).WorkspaceRoot = dir

		req := httptest.NewRequest(http.MethodPost, "/api/file/check-modified", bytes.NewReader([]byte("not json")))
		rec := httptest.NewRecorder()

		server.handleAPIFileCheckModified(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rec.Code)
		}
	})

	t.Run("POST with modified files returns changed entries", func(t *testing.T) {
		dir := t.TempDir()
		server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
		server.workspaceRoot = dir
		server.getOrCreateClientContext(defaultWebClientID).WorkspaceRoot = dir

		// Create a file in workspace
		filePath := filepath.Join(dir, "test.txt")
		if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
			t.Fatal(err)
		}

		// Send an old mtime so it will be detected as modified
		reqBody := checkModifiedRequest{
			Files: map[string]int64{
				filePath: 0, // old mtime — file has been modified
			},
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/file/check-modified", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		server.handleAPIFileCheckModified(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", rec.Code, rec.Body.String())
		}

		var resp checkModifiedResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if len(resp.Modified) == 0 {
			t.Fatalf("expected at least one modified entry, got %d", len(resp.Modified))
		}

		modified := resp.Modified[0]
		if modified.Path != filePath {
			t.Errorf("expected path=%s, got %s", filePath, modified.Path)
		}
		if modified.ModTime == 0 {
			t.Error("expected non-zero mod_time for existing file")
		}
	})

	t.Run("POST with unchanged files returns empty modified array", func(t *testing.T) {
		dir := t.TempDir()
		server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
		server.workspaceRoot = dir
		server.getOrCreateClientContext(defaultWebClientID).WorkspaceRoot = dir

		filePath := filepath.Join(dir, "unchanged.txt")
		if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
			t.Fatal(err)
		}

		info, _ := os.Stat(filePath)
		reqBody := checkModifiedRequest{
			Files: map[string]int64{
				filePath: info.ModTime().Unix(), // same mtime — not modified
			},
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/file/check-modified", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		server.handleAPIFileCheckModified(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var resp checkModifiedResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if len(resp.Modified) != 0 {
			t.Errorf("expected empty modified array, got %d entries", len(resp.Modified))
		}
	})

	t.Run("POST with path outside workspace is skipped", func(t *testing.T) {
		dir := t.TempDir()
		server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
		server.workspaceRoot = dir
		server.getOrCreateClientContext(defaultWebClientID).WorkspaceRoot = dir

		// Request a file outside the workspace — should be skipped
		reqBody := checkModifiedRequest{
			Files: map[string]int64{
				"/etc/passwd": 0,
			},
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/file/check-modified", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		server.handleAPIFileCheckModified(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var resp checkModifiedResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if len(resp.Modified) != 0 {
			t.Errorf("expected 0 modified entries for out-of-workspace file, got %d", len(resp.Modified))
		}
	})
}
