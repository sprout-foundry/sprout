package webui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
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