//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// setupRoleTest creates a temp directory with a RoleManager and returns
// the manager and temp dir. Cleanup is handled by t.TempDir().
func setupRoleTest(t *testing.T) (*configuration.RoleManager, string) {
	t.Helper()
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global", "roles")
	workspaceDir := filepath.Join(tmpDir, "workspace", "roles")
	rm := configuration.NewRoleManager(globalDir, workspaceDir)
	return rm, tmpDir
}

// createTestRole saves a valid role to the RoleManager.
func createTestRole(t *testing.T, rm *configuration.RoleManager, name string) {
	t.Helper()
	cfg := configuration.RoleConfig{
		Name:         name,
		Description:  "Test role " + name,
		SystemPrompt: "You are " + name,
	}
	if err := rm.Save(cfg, "workspace"); err != nil {
		t.Fatalf("failed to create test role %q: %v", name, err)
	}
}

// newRolesTestServer creates a ReactWebServer with minimal fields for role testing.
func newRolesTestServer(tmpDir string) *ReactWebServer {
	return &ReactWebServer{
		agent:         nil,
		daemonRoot:    tmpDir,
		workspaceRoot: tmpDir,
	}
}

// ---------------------------------------------------------------------------
// RoleManager unit tests
// ---------------------------------------------------------------------------

func TestRoleManager_SaveAndResolve(t *testing.T) {
	rm, _ := setupRoleTest(t)

	cfg := configuration.RoleConfig{
		Name:         "coder",
		Description:  "A coding assistant",
		SystemPrompt: "You are a helpful coder",
	}
	if err := rm.Save(cfg, "workspace"); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	resolved, err := rm.Resolve("coder")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if resolved.Name != "coder" {
		t.Errorf("expected name 'coder', got %q", resolved.Name)
	}
	if resolved.Description != "A coding assistant" {
		t.Errorf("expected description 'A coding assistant', got %q", resolved.Description)
	}
}

func TestRoleManager_ResolveNotFound(t *testing.T) {
	rm, _ := setupRoleTest(t)

	_, err := rm.Resolve("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent role")
	}
}

func TestRoleManager_List(t *testing.T) {
	rm, _ := setupRoleTest(t)

	// Empty list
	roles, err := rm.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(roles) != 0 {
		t.Errorf("expected empty list, got %d roles", len(roles))
	}

	// Create roles
	createTestRole(t, rm, "coder")
	createTestRole(t, rm, "reviewer")

	roles, err = rm.List()
	if err != nil {
		t.Fatalf("List failed after creates: %v", err)
	}
	if len(roles) != 2 {
		t.Errorf("expected 2 roles, got %d", len(roles))
	}

	names := make(map[string]bool)
	for _, r := range roles {
		names[r.Name] = true
	}
	if !names["coder"] {
		t.Error("expected 'coder' in list")
	}
	if !names["reviewer"] {
		t.Error("expected 'reviewer' in list")
	}
}

func TestRoleManager_Delete(t *testing.T) {
	rm, _ := setupRoleTest(t)
	createTestRole(t, rm, "temp-role")

	if !rm.Exists("temp-role") {
		t.Fatal("role should exist after creation")
	}

	if err := rm.Delete("temp-role"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if rm.Exists("temp-role") {
		t.Error("role should not exist after deletion")
	}
}

func TestRoleManager_DeleteNotFound(t *testing.T) {
	rm, _ := setupRoleTest(t)

	err := rm.Delete("nonexistent")
	if err == nil {
		t.Fatal("expected error when deleting nonexistent role")
	}
}

func TestRoleManager_OverwriteViaSave(t *testing.T) {
	rm, _ := setupRoleTest(t)
	createTestRole(t, rm, "updatable")

	updated := configuration.RoleConfig{
		Name:         "updatable",
		Description:  "Updated description",
		SystemPrompt: "Updated prompt",
	}
	if err := rm.Save(updated, "workspace"); err != nil {
		t.Fatalf("Save (update) failed: %v", err)
	}

	resolved, err := rm.Resolve("updatable")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if resolved.Description != "Updated description" {
		t.Errorf("expected updated description, got %q", resolved.Description)
	}
}

func TestRoleManager_Exists(t *testing.T) {
	rm, _ := setupRoleTest(t)

	if rm.Exists("missing") {
		t.Error("Exists should return false for missing role")
	}

	createTestRole(t, rm, "present")
	if !rm.Exists("present") {
		t.Error("Exists should return true for existing role")
	}
}

func TestRoleManager_InvalidName(t *testing.T) {
	rm, _ := setupRoleTest(t)

	cfg := configuration.RoleConfig{
		Name:        "../etc/passwd",
		Description: "Malicious",
	}
	err := rm.Save(cfg, "workspace")
	if err == nil {
		t.Fatal("expected error for invalid role name with path traversal")
	}
}

// ---------------------------------------------------------------------------
// HTTP handler tests — sub-handler level (bypasses getConfigManager)
// ---------------------------------------------------------------------------

func TestHandleRolesList_Empty(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	ws := newRolesTestServer(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api/roles", nil)
	rec := httptest.NewRecorder()
	ws.handleRolesList(rm, rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	roles, ok := resp["roles"].([]interface{})
	if !ok {
		t.Fatalf("expected 'roles' key in response, got %T", resp["roles"])
	}
	if len(roles) != 0 {
		t.Errorf("expected empty roles array, got %d", len(roles))
	}
}

func TestHandleRolesList_WithRoles(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	createTestRole(t, rm, "coder")
	createTestRole(t, rm, "reviewer")
	ws := newRolesTestServer(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api/roles", nil)
	rec := httptest.NewRecorder()
	ws.handleRolesList(rm, rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	roles := resp["roles"].([]interface{})
	if len(roles) != 2 {
		t.Errorf("expected 2 roles, got %d", len(roles))
	}
}

func TestHandleRolesGet_Found(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	createTestRole(t, rm, "coder")
	ws := newRolesTestServer(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api/roles/coder", nil)
	rec := httptest.NewRecorder()
	ws.handleRolesGet(rm, rec, req, "coder")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var role configuration.RoleConfig
	if err := json.NewDecoder(rec.Body).Decode(&role); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if role.Name != "coder" {
		t.Errorf("expected name 'coder', got %q", role.Name)
	}
}

func TestHandleRolesGet_NotFound(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	ws := newRolesTestServer(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api/roles/nonexistent", nil)
	rec := httptest.NewRecorder()
	ws.handleRolesGet(rm, rec, req, "nonexistent")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleRolesCreate_Success(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	ws := newRolesTestServer(tmpDir)

	body := `{"name":"new-role","description":"A new role","system_prompt":"You are new"}`
	req := httptest.NewRequest(http.MethodPost, "/api/roles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleRolesCreate(rm, rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var role configuration.RoleConfig
	if err := json.NewDecoder(rec.Body).Decode(&role); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if role.Name != "new-role" {
		t.Errorf("expected name 'new-role', got %q", role.Name)
	}
	if role.Description != "A new role" {
		t.Errorf("expected description 'A new role', got %q", role.Description)
	}

	// Verify it's actually persisted
	if !rm.Exists("new-role") {
		t.Error("role should exist after creation")
	}
}

func TestHandleRolesCreate_Duplicate(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	createTestRole(t, rm, "existing")
	ws := newRolesTestServer(tmpDir)

	body := `{"name":"existing","description":"Duplicate"}`
	req := httptest.NewRequest(http.MethodPost, "/api/roles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleRolesCreate(rm, rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for duplicate, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleRolesCreate_NoName(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	ws := newRolesTestServer(tmpDir)

	body := `{"description":"No name"}`
	req := httptest.NewRequest(http.MethodPost, "/api/roles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleRolesCreate(rm, rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleRolesCreate_InvalidBody(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	ws := newRolesTestServer(tmpDir)

	req := httptest.NewRequest(http.MethodPost, "/api/roles", strings.NewReader("not json at all"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleRolesCreate(rm, rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleRolesCreate_InvalidName(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	ws := newRolesTestServer(tmpDir)

	body := `{"name":"../bad","description":"Invalid name"}`
	req := httptest.NewRequest(http.MethodPost, "/api/roles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleRolesCreate(rm, rec, req)

	// The role doesn't exist yet, so it passes the Exists check,
	// but Save should fail with name validation
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for invalid name, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleRolesUpdate_Success(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	createTestRole(t, rm, "updatable")
	ws := newRolesTestServer(tmpDir)

	body := `{"name":"updatable","description":"Updated description","system_prompt":"Updated prompt"}`
	req := httptest.NewRequest(http.MethodPut, "/api/roles/updatable", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleRolesUpdate(rm, rec, req, "updatable")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var role configuration.RoleConfig
	if err := json.NewDecoder(rec.Body).Decode(&role); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if role.Description != "Updated description" {
		t.Errorf("expected 'Updated description', got %q", role.Description)
	}
}

func TestHandleRolesUpdate_NotFound(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	ws := newRolesTestServer(tmpDir)

	body := `{"name":"nonexistent","description":"Doesn't matter"}`
	req := httptest.NewRequest(http.MethodPut, "/api/roles/nonexistent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleRolesUpdate(rm, rec, req, "nonexistent")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleRolesUpdate_InvalidBody(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	createTestRole(t, rm, "existing")
	ws := newRolesTestServer(tmpDir)

	req := httptest.NewRequest(http.MethodPut, "/api/roles/existing", strings.NewReader("bad json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleRolesUpdate(rm, rec, req, "existing")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleRolesDelete_Success(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	createTestRole(t, rm, "deletable")
	ws := newRolesTestServer(tmpDir)

	req := httptest.NewRequest(http.MethodDelete, "/api/roles/deletable", nil)
	rec := httptest.NewRecorder()
	ws.handleRolesDelete(rm, rec, req, "deletable")

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify it's actually deleted
	if rm.Exists("deletable") {
		t.Error("role should not exist after deletion")
	}
}

func TestHandleRolesDelete_NotFound(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	ws := newRolesTestServer(tmpDir)

	req := httptest.NewRequest(http.MethodDelete, "/api/roles/nonexistent", nil)
	rec := httptest.NewRecorder()
	ws.handleRolesDelete(rm, rec, req, "nonexistent")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Full handler routing tests (via handleAPIRoles)
// ---------------------------------------------------------------------------

func TestHandleAPIRoles_MethodNotAllowed(t *testing.T) {
	// We need a server that can resolve a config manager. With nil agent
	// and no workspace, the fallback creates a config from default dirs.
	// The handler should still respond correctly even if config manager
	// resolves via fallback.
	ws := &ReactWebServer{agent: nil}

	// PATCH is not supported
	req := httptest.NewRequest(http.MethodPatch, "/api/roles", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIRoles(rec, req)

	// May get 503 if config dir unavailable, or 405 if it gets through
	// The key test is that it doesn't panic
	code := rec.Code
	if code != http.StatusMethodNotAllowed && code != http.StatusServiceUnavailable {
		t.Errorf("expected 405 or 503, got %d: %s", code, rec.Body.String())
	}
}

func TestHandleAPIRoles_PutMissingName(t *testing.T) {
	ws := &ReactWebServer{agent: nil}

	req := httptest.NewRequest(http.MethodPut, "/api/roles", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIRoles(rec, req)

	code := rec.Code
	if code != http.StatusBadRequest && code != http.StatusServiceUnavailable {
		t.Errorf("expected 400 or 503, got %d: %s", code, rec.Body.String())
	}
}

func TestHandleAPIRoles_DeleteMissingName(t *testing.T) {
	ws := &ReactWebServer{agent: nil}

	req := httptest.NewRequest(http.MethodDelete, "/api/roles", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIRoles(rec, req)

	code := rec.Code
	if code != http.StatusBadRequest && code != http.StatusServiceUnavailable {
		t.Errorf("expected 400 or 503, got %d: %s", code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Full lifecycle test
// ---------------------------------------------------------------------------

func TestHandleRolesLifecycle(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	ws := newRolesTestServer(tmpDir)

	// 1. Create
	body := `{"name":"lifecycle","description":"Lifecycle test","system_prompt":"You are lifecycle"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/roles", strings.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	ws.handleRolesCreate(rm, createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}

	// 2. Read
	getReq := httptest.NewRequest(http.MethodGet, "/api/roles/lifecycle", nil)
	getRec := httptest.NewRecorder()
	ws.handleRolesGet(rm, getRec, getReq, "lifecycle")
	if getRec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
	var created configuration.RoleConfig
	json.NewDecoder(getRec.Body).Decode(&created)
	if created.Name != "lifecycle" {
		t.Errorf("expected name 'lifecycle', got %q", created.Name)
	}

	// 3. Update
	updateBody := `{"name":"lifecycle","description":"Updated lifecycle","system_prompt":"Updated prompt"}`
	updateReq := httptest.NewRequest(http.MethodPut, "/api/roles/lifecycle", strings.NewReader(updateBody))
	updateRec := httptest.NewRecorder()
	ws.handleRolesUpdate(rm, updateRec, updateReq, "lifecycle")
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", updateRec.Code, updateRec.Body.String())
	}

	// Verify update
	getReq2 := httptest.NewRequest(http.MethodGet, "/api/roles/lifecycle", nil)
	getRec2 := httptest.NewRecorder()
	ws.handleRolesGet(rm, getRec2, getReq2, "lifecycle")
	var updated configuration.RoleConfig
	json.NewDecoder(getRec2.Body).Decode(&updated)
	if updated.Description != "Updated lifecycle" {
		t.Errorf("expected updated description, got %q", updated.Description)
	}

	// 4. Delete
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/roles/lifecycle", nil)
	deleteRec := httptest.NewRecorder()
	ws.handleRolesDelete(rm, deleteRec, deleteReq, "lifecycle")
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}

	// 5. Verify deleted
	getReq3 := httptest.NewRequest(http.MethodGet, "/api/roles/lifecycle", nil)
	getRec3 := httptest.NewRecorder()
	ws.handleRolesGet(rm, getRec3, getReq3, "lifecycle")
	if getRec3.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", getRec3.Code)
	}
}

// ---------------------------------------------------------------------------
// URL-decoded role name test
// ---------------------------------------------------------------------------

func TestHandleRolesGet_URLEncodedName(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	// Create a role with a hyphenated name (common URL-safe name)
	createTestRole(t, rm, "my-role")
	ws := newRolesTestServer(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api/roles/my-role", nil)
	rec := httptest.NewRecorder()
	ws.handleRolesGet(rm, rec, req, "my-role")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var role configuration.RoleConfig
	json.NewDecoder(rec.Body).Decode(&role)
	if role.Name != "my-role" {
		t.Errorf("expected name 'my-role', got %q", role.Name)
	}
}

// ---------------------------------------------------------------------------
// Error response format test
// ---------------------------------------------------------------------------

func TestHandleRolesError_JSONFormat(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	ws := newRolesTestServer(tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api/roles/nonexistent", nil)
	rec := httptest.NewRecorder()
	ws.handleRolesGet(rm, rec, req, "nonexistent")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if _, hasError := errResp["error"]; !hasError {
		t.Error("expected 'error' key in error response")
	}
}

// ---------------------------------------------------------------------------
// Table-driven tests for status codes
// ---------------------------------------------------------------------------

func TestHandleRolesStatusCodes(t *testing.T) {
	rm, tmpDir := setupRoleTest(t)
	ws := newRolesTestServer(tmpDir)
	createTestRole(t, rm, "existing")

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		handler    string // "list", "create", "get", "update", "delete"
		roleName   string // used for get/update/delete
	}{
		{
			name:       "list returns ok",
			method:     http.MethodGet,
			path:       "/api/roles",
			wantStatus: http.StatusOK,
			handler:    "list",
		},
		{
			name:       "create no name returns 400",
			method:     http.MethodPost,
			path:       "/api/roles",
			body:       `{"description":"no name"}`,
			wantStatus: http.StatusBadRequest,
			handler:    "create",
		},
		{
			name:       "create bad json returns 400",
			method:     http.MethodPost,
			path:       "/api/roles",
			body:       "not-json",
			wantStatus: http.StatusBadRequest,
			handler:    "create",
		},
		{
			name:       "get not found returns 404",
			method:     http.MethodGet,
			path:       "/api/roles/missing",
			wantStatus: http.StatusNotFound,
			handler:    "get",
			roleName:   "missing",
		},
		{
			name:       "delete not found returns 404",
			method:     http.MethodDelete,
			path:       "/api/roles/missing",
			wantStatus: http.StatusNotFound,
			handler:    "delete",
			roleName:   "missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyReader *strings.Reader
			if tt.body != "" {
				bodyReader = strings.NewReader(tt.body)
			} else {
				bodyReader = strings.NewReader("")
			}
			req := httptest.NewRequest(tt.method, tt.path, bodyReader)
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()

			switch tt.handler {
			case "list":
				ws.handleRolesList(rm, rec, req)
			case "create":
				ws.handleRolesCreate(rm, rec, req)
			case "get":
				ws.handleRolesGet(rm, rec, req, tt.roleName)
			case "update":
				ws.handleRolesUpdate(rm, rec, req, tt.roleName)
			case "delete":
				ws.handleRolesDelete(rm, rec, req, tt.roleName)
			}

			if rec.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}


