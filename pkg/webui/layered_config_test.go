package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestSanitizeClientID_Normal(t *testing.T) {
	// Test normal IDs pass through by creating requests with headers
	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	
	// Test various normal client IDs
	testCases := []struct {
		name     string
		clientID string
		expected string
	}{
		{"simple", "client1", "client1"},
		{"with-hyphen", "client-1", "client-1"},
		{"with-underscore", "client_1", "client_1"},
		{"with-numbers", "client123", "client123"},
		{"mixed", "my-client_123", "my-client_123"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(webClientIDHeader, tc.clientID)
			
			got := ws.resolveClientID(req)
			if got != tc.expected {
				t.Errorf("resolveClientID(%q) = %q, want %q", tc.clientID, got, tc.expected)
			}
		})
	}
}

func TestSanitizeClientID_PathTraversal(t *testing.T) {
	// Test that .. and / are removed
	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	
	testCases := []struct {
		name        string
		clientID    string
		expected    string
		description string
	}{
		{"with-dots", "client/1", "client1", "forward slash removed"},
		{"with-double-dot", "client..1", "client1", "double dot removed"},
		{"mixed-traversal", "../client/1", "client1", "mixed traversal sequences removed"},
		{"multiple-slashes", "client//1", "client1", "multiple forward slashes removed"},
		{"multiple-double-dots", "client..1..test", "client1test", "multiple double dots removed"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(webClientIDHeader, tc.clientID)
			
			got := ws.resolveClientID(req)
			if got != tc.expected {
				t.Errorf("resolveClientID(%q) = %q, want %q (%s)", tc.clientID, got, tc.expected, tc.description)
			}
		})
	}
}

func TestSanitizeClientID_Empty(t *testing.T) {
	// Test that empty ID returns default
	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	
	// Test with empty header
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(webClientIDHeader, "")
	
	got := ws.resolveClientID(req)
	if got != defaultWebClientID {
		t.Errorf("resolveClientID(empty) = %q, want %q", got, defaultWebClientID)
	}
	
	// Test with no header at all
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	got2 := ws.resolveClientID(req2)
	if got2 != defaultWebClientID {
		t.Errorf("resolveClientID(no header) = %q, want %q", got2, defaultWebClientID)
	}
	
	// Test with header that gets sanitized to empty
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.Header.Set(webClientIDHeader, "/..\\..")
	
	got3 := ws.resolveClientID(req3)
	if got3 != defaultWebClientID {
		t.Errorf("resolveClientID(sanitized to empty) = %q, want %q", got3, defaultWebClientID)
	}
}

func TestSanitizeClientID_BackslashTraversal(t *testing.T) {
	// Test that \\.. is removed
	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	
	testCases := []struct {
		name        string
		clientID    string
		expected    string
		description string
	}{
		{"with-backslash", "client\\1", "client1", "backslash removed"},
		{"backslash-dot", "client\\..1", "client1", "backslash with double dots removed"},
		{"complex-backslash", "client\\\\..\\..test", "clienttest", "complex backslash sequences removed"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(webClientIDHeader, tc.clientID)
			
			got := ws.resolveClientID(req)
			if got != tc.expected {
				t.Errorf("resolveClientID(%q) = %q, want %q (%s)", tc.clientID, got, tc.expected, tc.description)
			}
		})
	}
}

func TestGetLayeredConfigManager_CreatesPerClientDir(t *testing.T) {
	// Test that getLayeredConfigManager creates session dir for client
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolatedHome, ".config"))
	t.Setenv("USERPROFILE", isolatedHome)

	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	
	// Create a client context first
	clientID := "test-client"
	ctx := ws.getOrCreateClientContext(clientID)
	
	// Set a workspace root for the client
	workspaceDir := t.TempDir()
	ctx.WorkspaceRoot = workspaceDir
	
	// Get the layered config manager
	cm, err := ws.getLayeredConfigManager(clientID)
	if err != nil {
		t.Fatalf("getLayeredConfigManager failed: %v", err)
	}
	
	if cm == nil {
		t.Fatal("getLayeredConfigManager returned nil config manager")
	}
	
	// Verify the config manager is functional
	config := cm.GetConfig()
	if config == nil {
		t.Error("config manager returned nil config")
	}
}

func TestGetLayeredConfigManager_Isolation(t *testing.T) {
	// Test that two clients with different workspaces get config managers
	// scoped to their respective workspace directories.
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolatedHome, ".config"))
	t.Setenv("USERPROFILE", isolatedHome)

	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	
	// Create two client contexts with different workspaces
	clientA := "client-a"
	clientB := "client-b"
	
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	
	ctxA := ws.getOrCreateClientContext(clientA)
	ctxA.WorkspaceRoot = workspaceA
	
	ctxB := ws.getOrCreateClientContext(clientB)
	ctxB.WorkspaceRoot = workspaceB
	
	// Get config managers for both clients
	cmA, err := ws.getLayeredConfigManager(clientA)
	if err != nil {
		t.Fatalf("getLayeredConfigManager for client A failed: %v", err)
	}
	
	cmB, err := ws.getLayeredConfigManager(clientB)
	if err != nil {
		t.Fatalf("getLayeredConfigManager for client B failed: %v", err)
	}
	
	// Both should be valid config managers
	if cmA == nil || cmB == nil {
		t.Fatal("one or both config managers are nil")
	}
	
	// They should be different instances (different workspace configs)
	if cmA == cmB {
		t.Error("config managers for different clients should be different instances")
	}
}

// --- Scoped PUT settings tests ---

func makeSettingsRequest(ws *ReactWebServer, method, urlPath string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, urlPath, strings.NewReader(body))
	req.Header.Set("X-Ledit-Client-ID", "test-client")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleAPISettings(rec, req)
	return rec
}

func TestHandlePutSessionSettings(t *testing.T) {
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolatedHome, ".config"))
	t.Setenv("USERPROFILE", isolatedHome)
	t.Setenv("CI", "1")

	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	clientID := "test-client"
	ctx := ws.getOrCreateClientContext(clientID)
	chat := ctx.getOrCreateChatSession("default")
	chat.Provider = "openai"
	chat.Model = "gpt-4"

	// Verify setup
	activeChatID := ctx.getActiveChatID()
	session := ctx.getChatSession(activeChatID)
	if session == nil {
		t.Fatalf("setup: expected chat session for %q, got nil", activeChatID)
	}

	body := `{"reasoning_effort": "high", "model": "gpt-4o-mini"}`
	rec := makeSettingsRequest(ws, http.MethodPut, "/api/settings?layer=session", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	chat.mu.Lock()
	overrides := chat.ConfigOverrides
	chat.mu.Unlock()

	if overrides == nil {
		t.Fatal("ConfigOverrides should not be nil after PUT")
	}
	if overrides["model"] != "gpt-4o-mini" {
		t.Errorf("expected model override gpt-4o-mini, got %v", overrides["model"])
	}
	if overrides["reasoning_effort"] != "high" {
		t.Errorf("expected reasoning_effort high, got %v", overrides["reasoning_effort"])
	}
}

func TestHandlePutWorkspaceSettings(t *testing.T) {
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolatedHome, ".config"))
	t.Setenv("USERPROFILE", isolatedHome)

	workspaceRoot := t.TempDir()
	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	clientID := "test-client"
	ctx := ws.getOrCreateClientContext(clientID)
	ctx.WorkspaceRoot = workspaceRoot

	body := `{"reasoning_effort": "low"}`
	rec := makeSettingsRequest(ws, http.MethodPut, "/api/settings?layer=workspace", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify workspace config file was created
	workspaceConfigPath := filepath.Join(workspaceRoot, ".ledit", "config.json")
	data, err := os.ReadFile(workspaceConfigPath)
	if err != nil {
		t.Fatalf("workspace config file should exist: %v", err)
	}

	if !strings.Contains(string(data), `"low"`) {
		t.Errorf("workspace config should contain reasoning_effort=low, got: %s", string(data))
	}
}

func TestHandlePutGlobalSettings(t *testing.T) {
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	os.Unsetenv("XDG_CONFIG_HOME") // Ensure no leftover from other tests
	os.Unsetenv("LEDIT_CONFIG")
	t.Setenv("USERPROFILE", isolatedHome)

	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	clientID := "test-client"
	ws.getOrCreateClientContext(clientID)

	body := `{"reasoning_effort": "medium"}`
	rec := makeSettingsRequest(ws, http.MethodPut, "/api/settings?layer=global", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: body=%s", rec.Code, rec.Body.String())
	}

	configPath := filepath.Join(isolatedHome, ".ledit", "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("global config file should exist: %v", err)
	}

	if !strings.Contains(string(data), `"medium"`) {
		t.Errorf("global config should contain reasoning_effort=medium, got: %s", string(data))
	}
}

func TestHandleAPISettingsPutDefault_NoLayer(t *testing.T) {
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolatedHome, ".config"))
	t.Setenv("USERPROFILE", isolatedHome)

	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	clientID := "test-client"
	ws.getOrCreateClientContext(clientID)

	body := `{"reasoning_effort": "high"}`
	rec := makeSettingsRequest(ws, http.MethodPut, "/api/settings", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandlePutWorkspaceSettings_CopyFromGlobal tests the "copy global to workspace" flow
// that the Settings panel uses when clicking "Create Workspace Config".
func TestHandlePutWorkspaceSettings_CopyFromGlobal(t *testing.T) {
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolatedHome, ".config"))
	t.Setenv("USERPROFILE", isolatedHome)

	workspaceRoot := t.TempDir()
	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	clientID := "test-client"
	ctx := ws.getOrCreateClientContext(clientID)
	ctx.WorkspaceRoot = workspaceRoot

	// Step 1: Set some global settings
	globalBody := `{"reasoning_effort": "high", "history_scope": "global", "version": "2.0", "last_used_provider": "openai"}`
	rec := makeSettingsRequest(ws, http.MethodPut, "/api/settings?layer=global", globalBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("global PUT failed: %d: %s", rec.Code, rec.Body.String())
	}

	// Step 2: GET global settings (simulates the frontend "getSettingsLayer('global')")
	req := httptest.NewRequest(http.MethodGet, "/api/settings?layer=global", nil)
	req.Header.Set("X-Ledit-Client-ID", clientID)
	rec = httptest.NewRecorder()
	ws.handleAPISettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("global GET failed: %d: %s", rec.Code, rec.Body.String())
	}

	// Step 3: PUT global data to workspace (simulates "updateSettings(globalData, 'workspace')")
	rec = makeSettingsRequest(ws, http.MethodPut, "/api/settings?layer=workspace", rec.Body.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("workspace PUT failed: %d: %s", rec.Code, rec.Body.String())
	}

	// Step 4: GET workspace settings and verify data was preserved
	req = httptest.NewRequest(http.MethodGet, "/api/settings?layer=workspace", nil)
	req.Header.Set("X-Ledit-Client-ID", clientID)
	rec = httptest.NewRecorder()
	ws.handleAPISettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("workspace GET failed: %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	// Verify key fields were preserved
	for _, expected := range []string{`"reasoning_effort":"high"`, `"history_scope":"global"`, `"version":"2.0"`, `"last_used_provider":"openai"`} {
		if !strings.Contains(body, expected) {
			t.Errorf("workspace config missing %s, got: %s", expected, body)
		}
	}
}
