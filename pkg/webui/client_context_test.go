//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestSanitizeClientID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"valid-client", "valid-client"},
		{"client/with/slashes", "clientwithslashes"},
		{"client\\with\\backslashes", "clientwithbackslashes"},
		{"../traversal", "traversal"},
		{"", "default"},
		{"  spaces  ", "  spaces  "},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeClientID(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeClientID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveClientID(t *testing.T) {
	ws := &ReactWebServer{}

	t.Run("nil request returns default", func(t *testing.T) {
		got := ws.resolveClientID(nil)
		if got != "default" {
			t.Errorf("expected default, got %q", got)
		}
	})

	t.Run("header takes precedence", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?client_id=param", nil)
		req.Header.Set("X-Sprout-Client-ID", "header")
		got := ws.resolveClientID(req)
		if got != "header" {
			t.Errorf("expected header, got %q", got)
		}
	})

	t.Run("query param used when header missing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?client_id=query", nil)
		got := ws.resolveClientID(req)
		if got != "query" {
			t.Errorf("expected query, got %q", got)
		}
	})

	t.Run("empty header falls back to query param", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?client_id=query", nil)
		req.Header.Set("X-Sprout-Client-ID", "  ")
		got := ws.resolveClientID(req)
		if got != "query" {
			t.Errorf("expected query, got %q", got)
		}
	})

	t.Run("empty everything returns default", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		got := ws.resolveClientID(req)
		if got != "default" {
			t.Errorf("expected default, got %q", got)
		}
	})
}

// Renamed to avoid conflict with coverage_test.go
func TestNewWebClientContext_EmptyAgentStateSnapshot(t *testing.T) {
	data := emptyAgentStateSnapshot()
	if len(data) == 0 {
		t.Fatal("expected non-empty agent state snapshot")
	}
	if data[0] != '{' {
		t.Fatalf("expected JSON, got first byte %d", data[0])
	}
}

func TestNewWebClientContext(t *testing.T) {
	ctx := newWebClientContext("/workspace", "", "", "", "")
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if ctx.WorkspaceRoot != "/workspace" {
		t.Errorf("expected workspace /workspace, got %q", ctx.WorkspaceRoot)
	}
	if ctx.Terminal == nil {
		t.Error("expected non-nil Terminal")
	}
	if ctx.FileConsents == nil {
		t.Error("expected non-nil FileConsents")
	}
	if len(ctx.AgentState) == 0 {
		t.Error("expected non-empty AgentState")
	}
	if ctx.LastSeenAt.IsZero() {
		t.Error("expected non-zero LastSeenAt")
	}
	// Check default chat session exists
	if ctx.ChatSessions == nil {
		t.Error("expected ChatSessions to be initialized")
	}
	if ctx.DefaultChatID == "" {
		t.Error("expected DefaultChatID to be set")
	}
	if ctx.DefaultChatID != "default" {
		t.Errorf("expected DefaultChatID 'default', got %q", ctx.DefaultChatID)
	}
}

func TestNewWebClientContextTrimsSSHFields(t *testing.T) {
	ctx := newWebClientContext("/ws", "  host  ", "  key  ", "  url  ", "  home  ")
	if ctx.SSHHostAlias != "host" {
		t.Errorf("expected 'host', got %q", ctx.SSHHostAlias)
	}
	if ctx.SSHSessionKey != "key" {
		t.Errorf("expected 'key', got %q", ctx.SSHSessionKey)
	}
}

func TestTouchClientLastSeen(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ws.mutex.Lock()
	ws.clientContexts = map[string]*webClientContext{
		"test-client": {
			LastSeenAt: time.Now().Add(-1 * time.Hour),
		},
	}
	ws.mutex.Unlock()

	oldTime := ws.clientContexts["test-client"].LastSeenAt
	ws.touchClientLastSeen("test-client")
	newTime := ws.clientContexts["test-client"].LastSeenAt

	if !newTime.After(oldTime) {
		t.Error("expected LastSeenAt to be updated")
	}
}

func TestGetOrCreateClientContext(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = "/workspace"

	ctx1 := ws.getOrCreateClientContext("client-1")
	ctx2 := ws.getOrCreateClientContext("client-1")

	if ctx1 != ctx2 {
		t.Fatal("expected same context for same client ID")
	}
	if ctx1.WorkspaceRoot != "/workspace" {
		t.Errorf("expected workspace root /workspace, got %q", ctx1.WorkspaceRoot)
	}
}

func TestGetOrCreateClientContextDefault(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = "/workspace"

	ctx := ws.getOrCreateClientContext("")
	if ctx.WorkspaceRoot != "/workspace" {
		t.Errorf("expected default client to use workspace root, got %q", ctx.WorkspaceRoot)
	}
}

func TestClearClientSSHContextForSessionKey(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ws.mutex.Lock()
	ws.clientContexts = map[string]*webClientContext{
		"client-1": {SSHSessionKey: "session-key-1", SSHHostAlias: "host1"},
		"client-2": {SSHSessionKey: "session-key-2", SSHHostAlias: "host2"},
	}
	ws.mutex.Unlock()

	ws.clearClientSSHContextForSessionKey("session-key-1")

	ctx1 := ws.clientContexts["client-1"]
	ctx2 := ws.clientContexts["client-2"]

	if ctx1.SSHHostAlias != "" {
		t.Errorf("expected SSHHostAlias cleared, got %q", ctx1.SSHHostAlias)
	}
	if ctx1.SSHSessionKey != "" {
		t.Errorf("expected SSHSessionKey cleared, got %q", ctx1.SSHSessionKey)
	}
	if ctx2.SSHHostAlias != "host2" {
		t.Errorf("expected client-2 SSHHostAlias unchanged, got %q", ctx2.SSHHostAlias)
	}
}

func TestClearClientSSHContextForSessionKeyEmpty(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Should not panic with empty session key
	ws.clearClientSSHContextForSessionKey("")
	ws.clearClientSSHContextForSessionKey("  ")
}

func TestHasActiveQueryForClient(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ws.mutex.Lock()
	ws.clientContexts = map[string]*webClientContext{
		"client-1": {ActiveQuery: true},
		"client-2": {ActiveQuery: false},
	}
	ws.mutex.Unlock()

	if !ws.hasActiveQueryForClient("client-1") {
		t.Error("expected client-1 to have active query")
	}
	if ws.hasActiveQueryForClient("client-2") {
		t.Error("expected client-2 to not have active query")
	}
	if ws.hasActiveQueryForClient("nonexistent") {
		t.Error("expected nonexistent client to not have active query")
	}
}

func TestSetClientQueryActive(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ws.setClientQueryActive("client-1", true)
	ctx := ws.clientContexts["client-1"]
	if !ctx.ActiveQuery {
		t.Error("expected ActiveQuery to be true")
	}

	ws.setClientQueryActive("client-1", false)
	ctx = ws.clientContexts["client-1"]
	if ctx.ActiveQuery {
		t.Error("expected ActiveQuery to be false")
	}
}

func TestClearCachedAgent(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ws.mutex.Lock()
	ws.clientContexts = map[string]*webClientContext{
		"client-1": {
			Agent:          nil, // No real agent to test, but we test the function doesn't panic
			ChatSessions:   map[string]*chatSession{},
			DefaultChatID:  "default",
		},
	}
	ws.mutex.Unlock()

	ws.clearCachedAgent("client-1") // Should not panic
}

func TestCleanupInactiveClientContextsZeroMaxIdle(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	removed := ws.cleanupInactiveClientContexts(0)
	if removed != 0 {
		t.Errorf("expected 0 removed for zero maxIdle, got %d", removed)
	}
}

func TestCleanupInactiveClientContextsPreservesDefault(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ws.mutex.Lock()
	ws.clientContexts = map[string]*webClientContext{
		"default": {LastSeenAt: time.Now().Add(-2 * time.Hour)},
	}
	ws.mutex.Unlock()

	removed := ws.cleanupInactiveClientContexts(time.Hour)
	if removed != 0 {
		t.Errorf("expected default to be preserved, got %d removed", removed)
	}
}

func TestCleanupInactiveClientContextsPreservesConnected(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ws.mutex.Lock()
	ws.clientContexts = map[string]*webClientContext{
		"connected-client": {LastSeenAt: time.Now().Add(-2 * time.Hour)},
	}
	ws.mutex.Unlock()

	// Register the client as connected
	ws.connections.Store("conn1", &ConnectionInfo{ClientID: "connected-client"})

	removed := ws.cleanupInactiveClientContexts(time.Hour)
	if removed != 0 {
		t.Errorf("expected connected client to be preserved, got %d removed", removed)
	}
}

func TestCleanupInactiveClientContextsPreservesActiveQuery(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ws.mutex.Lock()
	ws.clientContexts = map[string]*webClientContext{
		"active-client": {
			LastSeenAt:  time.Now().Add(-2 * time.Hour),
			ActiveQuery: true,
		},
	}
	ws.mutex.Unlock()

	removed := ws.cleanupInactiveClientContexts(time.Hour)
	if removed != 0 {
		t.Errorf("expected active query client to be preserved, got %d removed", removed)
	}
}

func TestResolveWorkspaceRootForChat(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ws.mutex.Lock()
	ws.clientContexts = map[string]*webClientContext{
		"default": {
			WorkspaceRoot: "/workspace",
			ChatSessions:  map[string]*chatSession{
				"default": {WorktreePath: ""},
			},
			DefaultChatID: "default",
		},
	}
	ws.mutex.Unlock()

	result := ws.resolveWorkspaceRootForChat("default", "default")
	if result != "/workspace" {
		t.Errorf("expected /workspace, got %q", result)
	}
}

func TestResolveWorkspaceRootForChatWithWorktree(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	ws.mutex.Lock()
	ws.clientContexts = map[string]*webClientContext{
		"default": {
			WorkspaceRoot: "/workspace",
			ChatSessions:  map[string]*chatSession{
				"default": {WorktreePath: "/workspace/wt"},
			},
			DefaultChatID: "default",
		},
	}
	ws.mutex.Unlock()

	result := ws.resolveWorkspaceRootForChat("default", "default")
	if result != "/workspace/wt" {
		t.Errorf("expected /workspace/wt, got %q", result)
	}
}

func TestResolveWorkspaceRootForChatMissingClient(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	result := ws.resolveWorkspaceRootForChat("nonexistent", "default")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}
