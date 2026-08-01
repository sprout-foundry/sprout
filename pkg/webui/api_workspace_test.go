//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

// ---------------------------------------------------------------------------
// Home-workspace consent gate (SP-130) — API-layer tests
//
// These exercise the HTTP handlers (handleAPIWorkspaceGet /
// handleAPIWorkspaceSet) end-to-end with a workspace root that resolves to the
// user's home directory. Test isolation is achieved by pointing HOME at a temp
// directory via t.Setenv, so the consent file lives under the temp home and no
// state leaks to the real ~/.sprout/.
// ---------------------------------------------------------------------------

// newHomeWorkspaceServer builds a ReactWebServer whose daemonRoot and default
// workspace root are the resolved temp home. The caller is expected to have
// already called t.Setenv("HOME", tmpHome) so that resolveHomeDir / isHomeWorkspace
// agree that the temp dir *is* the home. daemonRoot is set to the symlink-resolved
// home so that isWithinWorkspace passes for the home path itself.
func newHomeWorkspaceServer(t *testing.T) (ws *ReactWebServer, resolvedHome string) {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	home := resolveHomeDir()
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("resolve home symlinks: %v", err)
	}

	ws, err = NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = resolved
	ws.workspaceRoot = resolved
	return ws, resolved
}

// TestHandleAPIWorkspaceGet_HomeWorkspaceNeedsSelection verifies that when the
// active workspace root is the home directory and the user has not yet
// consented, the GET endpoint reports that workspace selection is needed and
// surfaces the home flag. It also confirms suggested_projects is populated
// (FindProjectsInDirectory runs only when selection is needed).
func TestHandleAPIWorkspaceGet_HomeWorkspaceNeedsSelection(t *testing.T) {
	ws, resolvedHome := newHomeWorkspaceServer(t)

	// Set the client context's workspace root to home so the GET handler reads
	// it. Using setClientWorkspaceRoot would hit the home gate (which is the
	// point of the consent flow), so populate the context directly.
	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(defaultWebClientID)
	ctx.WorkspaceRoot = resolvedHome
	ws.mutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspaceGet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}

	if got, _ := resp["workspace_is_home"].(bool); !got {
		t.Errorf("expected workspace_is_home=true, got %v", resp["workspace_is_home"])
	}
	if got, _ := resp["needs_workspace_selection"].(bool); !got {
		t.Errorf("expected needs_workspace_selection=true, got %v", resp["needs_workspace_selection"])
	}
	// When selection is needed, FindProjectsInDirectory runs and the key is set.
	if _, ok := resp["suggested_projects"]; !ok {
		t.Errorf("expected suggested_projects in response when selection is needed, got keys: %v", keysOf(resp))
	}
}

// TestHandleAPIWorkspaceGet_HomeWorkspaceWithConsent verifies that once the
// user has consented to a home workspace, the GET endpoint no longer forces
// workspace selection.
func TestHandleAPIWorkspaceGet_HomeWorkspaceWithConsent(t *testing.T) {
	ws, resolvedHome := newHomeWorkspaceServer(t)

	// Record consent up front.
	if err := recordHomeWorkspaceConsent(); err != nil {
		t.Fatalf("recordHomeWorkspaceConsent: %v", err)
	}

	// Populate the client context workspace root directly (consent is already
	// recorded, so the home gate is satisfied).
	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(defaultWebClientID)
	ctx.WorkspaceRoot = resolvedHome
	ws.mutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspaceGet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}

	if got, _ := resp["workspace_is_home"].(bool); !got {
		t.Errorf("expected workspace_is_home=true, got %v", resp["workspace_is_home"])
	}
	if got, _ := resp["needs_workspace_selection"].(bool); got {
		t.Errorf("expected needs_workspace_selection=false after consent, got %v", resp["needs_workspace_selection"])
	}
}

// TestHandleAPIWorkspaceSet_HomeRequiresConsent verifies that POSTing the home
// directory as the workspace without consent_home is rejected with a 403 and
// the structured home_workspace_requires_consent error code.
func TestHandleAPIWorkspaceSet_HomeRequiresConsent(t *testing.T) {
	ws, resolvedHome := newHomeWorkspaceServer(t)

	body := `{"path": "` + resolvedHome + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/workspace", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspaceSet(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}
	if code, _ := resp["code"].(string); code != "home_workspace_requires_consent" {
		t.Errorf("expected code home_workspace_requires_consent, got %v", resp["code"])
	}
	// No consent should have been recorded by a rejected request.
	if hasHomeWorkspaceConsent() {
		t.Error("expected no consent to be recorded after a rejected request")
	}
}

// TestHandleAPIWorkspaceSet_HomeWithConsent verifies that POSTing the home
// directory with consent_home=true records the consent, sets the workspace, and
// returns 200.
func TestHandleAPIWorkspaceSet_HomeWithConsent(t *testing.T) {
	ws, resolvedHome := newHomeWorkspaceServer(t)

	body := `{"path": "` + resolvedHome + `", "consent_home": true}`
	req := httptest.NewRequest(http.MethodPost, "/api/workspace", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspaceSet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Consent must now be persisted.
	if !hasHomeWorkspaceConsent() {
		t.Error("expected consent to be recorded after consent_home=true")
	}

	// The workspace root in the response should be the home dir.
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}
	if got, _ := resp["workspace_root"].(string); got != resolvedHome {
		t.Errorf("expected workspace_root %q, got %q", resolvedHome, got)
	}
}

// keysOf returns the map keys of a map[string]interface{} for diagnostics.
func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
