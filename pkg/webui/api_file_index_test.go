//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestHandleAPIFileIndex(t *testing.T) {
	setup := func(t *testing.T) (*ReactWebServer, string) {
		t.Helper()
		root := t.TempDir()
		server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
		if err != nil {
			t.Fatal(err)
		}
		server.workspaceRoot = root
		server.getOrCreateClientContext(defaultWebClientID).WorkspaceRoot = root
		return server, root
	}

	seed := func(t *testing.T, root string) {
		t.Helper()
		for _, rel := range []string{"main.go", "src/app.ts", "node_modules/x/index.js"} {
			abs := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(abs, []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("returns relative paths and skips node_modules", func(t *testing.T) {
		server, root := setup(t)
		seed(t, root)

		req := httptest.NewRequest(http.MethodGet, "/api/file-index", nil)
		rec := httptest.NewRecorder()
		server.handleAPIFileIndex(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Files []struct {
				Name string `json:"name"`
				Path string `json:"path"`
				Type string `json:"type"`
			} `json:"files"`
			Truncated bool `json:"truncated"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		paths := map[string]bool{}
		for _, f := range body.Files {
			paths[f.Path] = true
			if filepath.IsAbs(f.Path) {
				t.Errorf("path %q must be workspace-relative", f.Path)
			}
		}
		if !paths["main.go"] || !paths["src/app.ts"] {
			t.Errorf("expected main.go and src/app.ts, got %v", paths)
		}
		if paths["node_modules/x/index.js"] {
			t.Error("node_modules must be pruned")
		}
		if body.Truncated {
			t.Error("small tree must not be truncated")
		}
	})

	t.Run("non-GET returns 405", func(t *testing.T) {
		server, _ := setup(t)
		req := httptest.NewRequest(http.MethodPost, "/api/file-index", nil)
		rec := httptest.NewRecorder()
		server.handleAPIFileIndex(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})
}
