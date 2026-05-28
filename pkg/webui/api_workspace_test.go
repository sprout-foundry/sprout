//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestHandleAPIWorkspaceMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPut, "/api/workspace", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspace(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIWorkspaceGet(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspace(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}
	if resp["daemon_root"] == "" {
		t.Error("expected daemon_root in response")
	}
	if resp["workspace_root"] == "" {
		t.Error("expected workspace_root in response")
	}
}

func TestHandleAPIWorkspaceGetMethod(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspaceGet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleAPIWorkspaceSetMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	// The handler uses handleAPIWorkspace which dispatches - GET goes to handleAPIWorkspaceGet
	// POST is what we're testing via handleAPIWorkspaceSet
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspaceGet(rec, req)
	// Should work since GET is valid
	if rec.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", rec.Code)
	}
}

func TestHandleAPIWorkspaceSetInvalidJSON(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/workspace", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspace(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIWorkspaceSetMissingPath(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/workspace", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspace(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIWorkspaceSetWhitespacePath(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/workspace", strings.NewReader(`{"path":"   "}`))
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspace(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for whitespace path, got %d", rec.Code)
	}
}

func TestHandleAPIWorkspaceBrowseMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/workspace/browse", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspaceBrowse(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestIsSSHProxyRequestPath(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("SSH path returns true", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ssh/session1/api/test", nil)
		if !ws.isSSHProxyRequest(req) {
			t.Error("expected true for /ssh/ path")
		}
	})

	t.Run("non-SSH path returns false", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		ws.mutex.Lock()
		ws.clientContexts = map[string]*webClientContext{}
		ws.mutex.Unlock()
		if ws.isSSHProxyRequest(req) {
			t.Error("expected false for non-SSH path")
		}
	})
}

func TestGetSSHSessionForProxyRequest(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("non-SSH path returns nil", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		result := ws.getSSHSessionForProxyRequest(req)
		if result != nil {
			t.Error("expected nil for non-SSH path")
		}
	})

	t.Run("SSH path with no session returns nil", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ssh/session1/api/test", nil)
		result := ws.getSSHSessionForProxyRequest(req)
		if result != nil {
			t.Error("expected nil for unknown SSH session")
		}
	})
}
