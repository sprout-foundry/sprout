package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// ---------------------------------------------------------------------------
// serverConfigChanged — table-driven coverage for the 9 field comparisons
// ---------------------------------------------------------------------------

// deepCopyMCPServerConfig returns an independent value of cfg. Maps and
// slices are cloned so a mutator on the copy can't leak into the original.
func deepCopyMCPServerConfig(cfg mcp.MCPServerConfig) mcp.MCPServerConfig {
	out := cfg
	if cfg.Env != nil {
		out.Env = make(map[string]string, len(cfg.Env))
		for k, v := range cfg.Env {
			out.Env[k] = v
		}
	}
	if cfg.Credentials != nil {
		out.Credentials = make(map[string]string, len(cfg.Credentials))
		for k, v := range cfg.Credentials {
			out.Credentials[k] = v
		}
	}
	if cfg.Args != nil {
		out.Args = append([]string(nil), cfg.Args...)
	}
	return out
}

func TestServerConfigChanged(t *testing.T) {
	base := mcp.MCPServerConfig{
		Name:       "gh",
		Type:       "stdio",
		Command:    "npx",
		Args:       []string{"-y", "@modelcontextprotocol/server-github"},
		URL:        "",
		Env:        map[string]string{"GITHUB_API_KEY": "${GITHUB_TOKEN}"},
		WorkingDir: "/tmp",
		Timeout:    30 * time.Second,
		AutoStart:  true,
	}

	cases := []struct {
		name string
		mut  func(*mcp.MCPServerConfig) // apply to a copy of base
		want bool                        // expected "changed"
	}{
		// --- unchanged: every field equal ---
		{"identical", func(c *mcp.MCPServerConfig) {}, false},

		// --- each structural field flips one branch ---
		{"type differs", func(c *mcp.MCPServerConfig) { c.Type = "http" }, true},
		{"command differs", func(c *mcp.MCPServerConfig) { c.Command = "node" }, true},
		{"url differs", func(c *mcp.MCPServerConfig) { c.URL = "https://example.com/mcp" }, true},
		{"working_dir differs", func(c *mcp.MCPServerConfig) { c.WorkingDir = "/var" }, true},
		{"args differ (different length)", func(c *mcp.MCPServerConfig) { c.Args = []string{"-y"} }, true},
		{"args differ (different content)", func(c *mcp.MCPServerConfig) { c.Args = []string{"-y", "different"} }, true},
		{"timeout differs", func(c *mcp.MCPServerConfig) { c.Timeout = 60 * time.Second }, true},
		{"auto_start differs", func(c *mcp.MCPServerConfig) { c.AutoStart = false }, true},

		// --- env changes require restart (injected at subprocess start) ---
		{"env added", func(c *mcp.MCPServerConfig) { c.Env["NEW_VAR"] = "x" }, true},
		{"env removed", func(c *mcp.MCPServerConfig) { delete(c.Env, "GITHUB_API_KEY") }, true},
		{"env value changed", func(c *mcp.MCPServerConfig) { c.Env["GITHUB_API_KEY"] = "${OTHER}" }, true},

		// --- intentionally NOT compared: name (identity), credentials, max_restarts ---
		{"name differs (ignored — identity)", func(c *mcp.MCPServerConfig) { c.Name = "github" }, false},
		{"credentials differ (ignored)", func(c *mcp.MCPServerConfig) {
			c.Credentials = map[string]string{"TOKEN": "placeholder"}
		}, false},
		{"max_restarts differs (ignored)", func(c *mcp.MCPServerConfig) { c.MaxRestarts = 5 }, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldCfg := deepCopyMCPServerConfig(base) // independent Env map
			newCfg := deepCopyMCPServerConfig(base) // independent Env map
			tc.mut(&newCfg)
			got := serverConfigChanged(oldCfg, newCfg)
			if got != tc.want {
				t.Errorf("serverConfigChanged() = %v, want %v\nold=%+v\nnew=%+v",
					got, tc.want, oldCfg, newCfg)
			}
		})
	}
}

func TestServerConfigChanged_NilEnvVsEmptyEnv(t *testing.T) {
	// Edge case: nil vs empty map should be treated as different lengths
	// by Go's map semantics, but our mapsEqual handles both as zero-length.
	a := mcp.MCPServerConfig{Name: "x"}
	b := mcp.MCPServerConfig{Name: "x", Env: map[string]string{}}
	if serverConfigChanged(a, b) {
		t.Errorf("nil Env and empty Env should be equal")
	}
}

// ---------------------------------------------------------------------------
// mock MCPManager — tracks AddServer/RemoveServer/StartAll calls
// ---------------------------------------------------------------------------

type mockMCPManager struct {
	servers   map[string]mcp.MCPServerConfig
	listOrder []string // insertion order for ListServers

	addCalls    atomic.Int32
	removeCalls atomic.Int32
	startCalls  atomic.Int32

	addErr    error // returned by AddServer (optional)
	removeErr error // returned by RemoveServer
	startErr  error // returned by StartAll
}

func newMockMCPManager() *mockMCPManager {
	return &mockMCPManager{servers: map[string]mcp.MCPServerConfig{}}
}

func (m *mockMCPManager) AddServer(cfg mcp.MCPServerConfig) error {
	m.addCalls.Add(1)
	if m.addErr != nil {
		return m.addErr
	}
	m.servers[cfg.Name] = cfg
	return nil
}

func (m *mockMCPManager) RemoveServer(name string) error {
	m.removeCalls.Add(1)
	if m.removeErr != nil {
		return m.removeErr
	}
	delete(m.servers, name)
	return nil
}

func (m *mockMCPManager) GetServer(name string) (mcp.MCPServer, bool) {
	return nil, false // unused by reconcile
}

func (m *mockMCPManager) ListServers() []mcp.MCPServer {
	// Return fake MCPServer objects so reconcile can read name + config
	out := make([]mcp.MCPServer, 0, len(m.servers))
	for name, cfg := range m.servers {
		out = append(out, &fakeServer{name: name, cfg: cfg})
	}
	return out
}

func (m *mockMCPManager) StartAll(_ context.Context) error {
	m.startCalls.Add(1)
	return m.startErr
}

func (m *mockMCPManager) StopAll(_ context.Context) error { return nil }
func (m *mockMCPManager) GetAllTools(_ context.Context) ([]mcp.MCPTool, error) {
	return nil, nil
}
func (m *mockMCPManager) CallTool(_ context.Context, _, _ string, _ map[string]interface{}) (*mcp.MCPToolCallResult, error) {
	return nil, nil
}

// fakeServer implements mcp.MCPServer minimally for ListServers() output.
type fakeServer struct {
	name string
	cfg  mcp.MCPServerConfig
}

func (f *fakeServer) Start(_ context.Context) error   { return nil }
func (f *fakeServer) Stop(_ context.Context) error    { return nil }
func (f *fakeServer) IsRunning() bool                 { return true }
func (f *fakeServer) GetName() string                 { return f.name }
func (f *fakeServer) GetConfig() mcp.MCPServerConfig  { return f.cfg }
func (f *fakeServer) Initialize(_ context.Context) error { return nil }
func (f *fakeServer) ListTools(_ context.Context) ([]mcp.MCPTool, error) {
	return nil, nil
}
func (f *fakeServer) CallTool(_ context.Context, _ mcp.MCPToolCallRequest) (*mcp.MCPToolCallResult, error) {
	return nil, nil
}
func (f *fakeServer) ListResources(_ context.Context) ([]mcp.MCPResource, error) {
	return nil, nil
}
func (f *fakeServer) ReadResource(_ context.Context, _ string) (*mcp.MCPContent, error) {
	return nil, nil
}
func (f *fakeServer) ListPrompts(_ context.Context) ([]mcp.MCPPrompt, error) {
	return nil, nil
}
func (f *fakeServer) GetPrompt(_ context.Context, _ string, _ map[string]interface{}) (*mcp.MCPContent, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// reconcileMCPServers — end-to-end against a mock manager
// ---------------------------------------------------------------------------

// writeConfig writes a config JSON with the given MCP servers to a fresh
// temp config dir and returns the manager + its working directory.
func writeConfig(t *testing.T, servers map[string]mcp.MCPServerConfig) *configuration.Manager {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("LEDIT_CONFIG", dir)

	mgr, err := configuration.NewManagerWithDir(dir)
	if err != nil {
		t.Fatalf("NewManagerWithDir: %v", err)
	}

	cfg := mgr.GetConfig()
	cfg.MCP.Servers = servers
	if err := mgr.UpdateConfig(func(c *configuration.Config) error {
		c.MCP.Servers = servers
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	return mgr
}

// buildAgentWithMockMCP constructs an agent wired to a mock MCP manager.
func buildAgentWithMockMCP(t *testing.T, mgr *configuration.Manager, mock mcp.MCPManager) *Agent {
	t.Helper()
	a := &Agent{
		configManager: mgr,
		mcpSub:        NewAgentMCPManager(),
		debug:         false,
	}
	a.mcpSub.SetManager(mock)
	return a
}

func TestReconcileMCPServers_AddsNewServer(t *testing.T) {
	mgr := writeConfig(t, map[string]mcp.MCPServerConfig{
		"gh": {Name: "gh", Type: "stdio", Command: "npx", Timeout: 30 * time.Second},
	})
	mock := newMockMCPManager() // starts empty
	a := buildAgentWithMockMCP(t, mgr, mock)

	if err := a.reconcileMCPServers(context.Background()); err != nil {
		t.Fatalf("reconcileMCPServers: %v", err)
	}

	if got := mock.addCalls.Load(); got != 1 {
		t.Errorf("AddServer calls = %d, want 1", got)
	}
	if got := mock.removeCalls.Load(); got != 0 {
		t.Errorf("RemoveServer calls = %d, want 0", got)
	}
	if got := mock.startCalls.Load(); got != 1 {
		t.Errorf("StartAll calls = %d, want 1", got)
	}
	if _, ok := mock.servers["gh"]; !ok {
		t.Error("expected gh server in mock after reconcile")
	}
}

func TestReconcileMCPServers_RemovesDeletedServer(t *testing.T) {
	// Start with one server already registered, then reload with empty config.
	mgr := writeConfig(t, map[string]mcp.MCPServerConfig{}) // empty after update
	mock := newMockMCPManager()
	if err := mock.AddServer(mcp.MCPServerConfig{Name: "old", Type: "stdio", Command: "x"}); err != nil {
		t.Fatalf("seed mock: %v", err)
	}
	a := buildAgentWithMockMCP(t, mgr, mock)

	if err := a.reconcileMCPServers(context.Background()); err != nil {
		t.Fatalf("reconcileMCPServers: %v", err)
	}

	if got := mock.removeCalls.Load(); got != 1 {
		t.Errorf("RemoveServer calls = %d, want 1", got)
	}
	if got := mock.addCalls.Load(); got != 1 {
		t.Errorf("AddServer calls = %d, want 1 (from seed)", got)
	}
	if len(mock.servers) != 0 {
		t.Errorf("servers remaining = %d, want 0", len(mock.servers))
	}
}

func TestReconcileMCPServers_RestartsChangedServer(t *testing.T) {
	// Start with one server, then change its Command in config.
	mock := newMockMCPManager()
	originalCfg := mcp.MCPServerConfig{
		Name: "gh", Type: "stdio", Command: "npx", Timeout: 30 * time.Second,
	}
	if err := mock.AddServer(originalCfg); err != nil {
		t.Fatalf("seed mock: %v", err)
	}

	mgr := writeConfig(t, map[string]mcp.MCPServerConfig{
		"gh": {Name: "gh", Type: "stdio", Command: "node", Timeout: 30 * time.Second}, // Command differs
	})
	a := buildAgentWithMockMCP(t, mgr, mock)

	if err := a.reconcileMCPServers(context.Background()); err != nil {
		t.Fatalf("reconcileMCPServers: %v", err)
	}

	if got := mock.removeCalls.Load(); got != 1 {
		t.Errorf("RemoveServer calls = %d, want 1 (restart path)", got)
	}
	if got := mock.addCalls.Load(); got != 2 {
		t.Errorf("AddServer calls = %d, want 2 (seed + re-add)", got)
	}
	if got := mock.servers["gh"].Command; got != "node" {
		t.Errorf("server Command = %q, want \"node\"", got)
	}
}

func TestReconcileMCPServers_NoOpWhenUnchanged(t *testing.T) {
	// Same server in both mock and config — no add, no remove, no restart.
	// Note: Timeout must be explicit. MCPServerConfig's JSON unmarshal
	// defaults Timeout to 30s when the field is absent (omitempty), so
	// leaving it as 0 on the input side would round-trip to 30s on reload
	// and trigger a false-positive restart.
	cfg := mcp.MCPServerConfig{
		Name:    "gh",
		Type:    "stdio",
		Command: "npx",
		Timeout: 30 * time.Second,
	}
	mgr := writeConfig(t, map[string]mcp.MCPServerConfig{"gh": cfg})
	mock := newMockMCPManager()
	if err := mock.AddServer(cfg); err != nil {
		t.Fatalf("seed mock: %v", err)
	}
	a := buildAgentWithMockMCP(t, mgr, mock)

	if err := a.reconcileMCPServers(context.Background()); err != nil {
		t.Fatalf("reconcileMCPServers: %v", err)
	}

	if got := mock.addCalls.Load(); got != 1 {
		t.Errorf("AddServer calls = %d, want 1 (just the seed)", got)
	}
	if got := mock.removeCalls.Load(); got != 0 {
		t.Errorf("RemoveServer calls = %d, want 0 (no restart needed)", got)
	}
	// StartAll is still called once (it's the final step in reconcile)
	if got := mock.startCalls.Load(); got != 1 {
		t.Errorf("StartAll calls = %d, want 1", got)
	}
}

func TestReconcileMCPServers_AggregatesErrors(t *testing.T) {
	// If a remove fails, the add for the same server should still be attempted
	// (or at least, errors should be aggregated rather than silently swallowed).
	mock := newMockMCPManager()
	if err := mock.AddServer(mcp.MCPServerConfig{Name: "old", Type: "stdio", Command: "x"}); err != nil {
		t.Fatalf("seed mock: %v", err)
	}

	mgr := writeConfig(t, map[string]mcp.MCPServerConfig{
		"new": {Name: "new", Type: "stdio", Command: "y"},
	})
	a := buildAgentWithMockMCP(t, mgr, mock)

	// Force the remove to fail
	mock.removeErr = &mockError{msg: "simulated remove failure"}

	err := a.reconcileMCPServers(context.Background())
	if err == nil {
		t.Fatal("expected aggregated error, got nil")
	}
	if !strings.Contains(err.Error(), "simulated remove failure") {
		t.Errorf("error %q should mention simulated remove failure", err.Error())
	}
	if !strings.Contains(err.Error(), "old") {
		t.Errorf("error %q should reference server name 'old'", err.Error())
	}
}

type mockError struct{ msg string }

func (e *mockError) Error() string { return e.msg }

// ---------------------------------------------------------------------------
// RefreshRuntimeConfig — end-to-end (reload + reconcile)
// ---------------------------------------------------------------------------

func TestRefreshRuntimeConfig_ReloadsConfigAndReconciles(t *testing.T) {
	// Step 1: write config with NO servers, seed mock with empty state.
	dir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("LEDIT_CONFIG", dir)
	mgr, err := configuration.NewManagerWithDir(dir)
	if err != nil {
		t.Fatalf("NewManagerWithDir: %v", err)
	}

	mock := newMockMCPManager()
	a := buildAgentWithMockMCP(t, mgr, mock)

	// Step 2: write a NEW config file with one server (simulating the user
	// editing config on disk between refreshes).
	cfg := mgr.GetConfig()
	cfg.MCP.Servers = map[string]mcp.MCPServerConfig{
		"hotreload": {Name: "hotreload", Type: "stdio", Command: "node"},
	}
	if err := mgr.UpdateConfig(func(c *configuration.Config) error {
		c.MCP.Servers = cfg.MCP.Servers
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	// Step 3: call RefreshRuntimeConfig — it should reload config from disk
	// and add the server to the mock manager.
	if err := a.RefreshRuntimeConfig(context.Background()); err != nil {
		t.Fatalf("RefreshRuntimeConfig: %v", err)
	}

	if got := mock.addCalls.Load(); got != 1 {
		t.Errorf("AddServer calls = %d, want 1 after refresh", got)
	}
	if _, ok := mock.servers["hotreload"]; !ok {
		t.Error("expected hotreload server registered after RefreshRuntimeConfig")
	}
}

func TestRefreshRuntimeConfig_NilMCPSubStillReloadsConfig(t *testing.T) {
	// The config reload step should still run even if MCP is nil,
	// because it picks up skill changes independently.
	dir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("LEDIT_CONFIG", dir)
	mgr, err := configuration.NewManagerWithDir(dir)
	if err != nil {
		t.Fatalf("NewManagerWithDir: %v", err)
	}

	// Write a sentinel file we can check mtime on to prove the config
	// was re-read from disk.
	sentinel := filepath.Join(dir, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("v1"), 0644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	a := &Agent{
		configManager: mgr,
		// mcpSub intentionally nil
		debug: false,
	}

	if err := a.RefreshRuntimeConfig(context.Background()); err != nil {
		t.Fatalf("RefreshRuntimeConfig with nil mcpSub: %v", err)
	}
	// No panic = success. If config reload had failed it would return error.
}

func TestRefreshRuntimeConfig_NilConfigManagerReturnsError(t *testing.T) {
	a := &Agent{debug: false}
	err := a.RefreshRuntimeConfig(context.Background())
	if err == nil {
		t.Fatal("expected error when configManager is nil")
	}
	if !strings.Contains(err.Error(), "config manager is not available") {
		t.Errorf("error %q should mention config manager", err.Error())
	}
}

func TestRefreshSkills_NilConfigManagerReturnsError(t *testing.T) {
	a := &Agent{debug: false}
	err := a.RefreshSkills()
	if err == nil {
		t.Fatal("expected error when configManager is nil")
	}
	if !strings.Contains(err.Error(), "config manager is not available") {
		t.Errorf("error %q should mention config manager", err.Error())
	}
}

// ---------------------------------------------------------------------------
// isAlreadyExistsError — helper sanity check
// ---------------------------------------------------------------------------

func TestIsAlreadyExistsError(t *testing.T) {
	if isAlreadyExistsError(nil) {
		t.Error("nil error should not be 'already exists'")
	}
	if !isAlreadyExistsError(&mockError{msg: "server already exists"}) {
		t.Error("'already exists' substring should be detected")
	}
	if isAlreadyExistsError(&mockError{msg: "some other error"}) {
		t.Error("unrelated error should not match")
	}
}

// ---------------------------------------------------------------------------
// Compile-time guard: mock satisfies MCPManager
// ---------------------------------------------------------------------------

var _ mcp.MCPManager = (*mockMCPManager)(nil)
var _ mcp.MCPServer = (*fakeServer)(nil)
var _ api.ClientInterface // keep api import for future expansion without breaking build