//go:build !js

package cmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/testutil"
)

// =============================================================================
// Test helpers
// =============================================================================

// setupMCPTestEnv creates a temp config dir and saves/restores relevant env vars.
// Returns the temp dir path and a cleanup function.
func setupMCPTestEnv(t *testing.T) (string, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)
	// Clear github token to prevent auto-discovery adding servers
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")
	// Disable auto-discovery to ensure empty server list in tests
	t.Setenv("LEDIT_MCP_AUTO_DISCOVER", "false")
	return tmpDir, func() {}
}

// shouldSkipIfRealMCPConfigExists skips the test if ~/.config/sprout/mcp_config.json
// already exists. Tests that modify the real MCP config cannot be safely isolated
// because pkg/mcp/config.go:getConfigDir() reads from ~/.config/sprout/ and does not
// respect $LEDIT_CONFIG.
func shouldSkipIfRealMCPConfigExists(t *testing.T) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "sprout", "mcp_config.json")); err == nil {
		t.Skipf("skipping: ~/.config/sprout/mcp_config.json exists; this test reads/loads the real MCP config")
	}
}

// replaceStdinWithClosedPipe replaces os.Stdin with a pipe whose write end is
// immediately closed (simulating EOF). Returns a restore function.
func replaceStdinWithClosedPipe(t *testing.T) (restore func()) {
	t.Helper()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	w.Close() // Immediately close write end to simulate EOF
	os.Stdin = r
	return func() {
		r.Close()
		os.Stdin = oldStdin
	}
}

// Test 1: runMCPList
// =============================================================================

func TestRunMCPList(t *testing.T) {
	_, cleanup := setupMCPTestEnv(t)
	defer cleanup()

	out := testutil.CaptureStdout(t, func() {
		if err := runMCPList(); err != nil {
			t.Fatalf("runMCPList returned error: %v", err)
		}
	})

	// Should always print header regardless of config content
	if !strings.Contains(out, "MCP Configuration") {
		t.Errorf("expected 'MCP Configuration' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Enabled:") {
		t.Errorf("expected 'Enabled:' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Total servers:") {
		t.Errorf("expected 'Total servers:' in output, got:\n%s", out)
	}
}

// =============================================================================
// Test 2: runMCPTest with empty servers
// =============================================================================

func TestRunMCPTest_EmptyServers(t *testing.T) {
	_, cleanup := setupMCPTestEnv(t)
	defer cleanup()

	shouldSkipIfRealMCPConfigExists(t)

	// Replace stdin with closed pipe in case the function tries to read
	restoreStdin := replaceStdinWithClosedPipe(t)
	defer restoreStdin()

	out := testutil.CaptureStdout(t, func() {
		if err := runMCPTest(""); err != nil {
			t.Fatalf("runMCPTest returned error: %v", err)
		}
	})

	if !strings.Contains(out, "No MCP servers configured") {
		t.Errorf("expected 'No MCP servers configured' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "sprout mcp add") {
		t.Errorf("expected 'sprout mcp add' in output, got:\n%s", out)
	}
}

// =============================================================================
// Test 3: runMCPRemove with no servers
// =============================================================================

func TestRunMCPRemove_NoServers(t *testing.T) {
	_, cleanup := setupMCPTestEnv(t)
	defer cleanup()

	shouldSkipIfRealMCPConfigExists(t)

	// Replace stdin with closed pipe in case the function tries to read
	restoreStdin := replaceStdinWithClosedPipe(t)
	defer restoreStdin()

	var rmErr error
	out := testutil.CaptureStdout(t, func() {
		rmErr = runMCPRemove("")
	})

	if rmErr != nil {
		t.Errorf("runMCPRemove with empty servers should return nil, got: %v", rmErr)
	}

	if !strings.Contains(out, "No MCP servers configured") {
		t.Errorf("expected 'No MCP servers configured' in output, got:\n%s", out)
	}
}

// =============================================================================
// Test 4: runMCPTest with non-existent server
// =============================================================================

func TestRunMCPTest_NonExistentServer(t *testing.T) {
	_, cleanup := setupMCPTestEnv(t)
	defer cleanup()

	shouldSkipIfRealMCPConfigExists(t)

	err := runMCPTest("xyz-nonexistent-server-12345")
	if err == nil {
		t.Fatal("expected error for non-existent server, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

// =============================================================================
// Test 5: runMCPRemove with non-existent server
// =============================================================================

func TestRunMCPRemove_NonExistentServer(t *testing.T) {
	_, cleanup := setupMCPTestEnv(t)
	defer cleanup()

	shouldSkipIfRealMCPConfigExists(t)

	err := runMCPRemove("xyz-nonexistent-server-12345")
	if err == nil {
		t.Fatal("expected error for non-existent server, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

// =============================================================================
// Test 6: MCP command registration
// =============================================================================

func TestMCPCommandRegistration(t *testing.T) {
	// Verify mcpCmd is properly configured
	if mcpCmd.Use != "mcp" {
		t.Errorf("expected mcpCmd.Use = 'mcp', got %q", mcpCmd.Use)
	}
	if mcpCmd.Short == "" {
		t.Error("expected mcpCmd.Short to be set")
	}

	// Verify parent-child relationships
	expectedSubcommands := map[string]bool{
		"add":    false,
		"remove": false,
		"list":   false,
		"test":   false,
	}

	for _, cmd := range mcpCmd.Commands() {
		if _, ok := expectedSubcommands[cmd.Name()]; ok {
			expectedSubcommands[cmd.Name()] = true
		}
	}

	for name, found := range expectedSubcommands {
		if !found {
			t.Errorf("expected subcommand %q to be registered under mcpCmd", name)
		}
	}
}

func TestMCPSubcommandProperties(t *testing.T) {
	tests := []struct {
		name string
		cmd  *cobra.Command
		use  string
	}{
		{
			name: "mcpAddCmd",
			cmd:  mcpAddCmd,
			use:  "add",
		},
		{
			name: "mcpRemoveCmd",
			cmd:  mcpRemoveCmd,
			use:  "remove [server-name]",
		},
		{
			name: "mcpListCmd",
			cmd:  mcpListCmd,
			use:  "list",
		},
		{
			name: "mcpTestCmd",
			cmd:  mcpTestCmd,
			use:  "test [server-name]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cmd == nil {
				t.Fatalf("command %q is nil", tt.name)
			}
			if tt.cmd.Use != tt.use {
				t.Errorf("expected Use = %q, got %q", tt.use, tt.cmd.Use)
			}
			if tt.cmd.Short == "" {
				t.Errorf("expected Short to be set for %q", tt.name)
			}
			if tt.cmd.Run == nil && tt.cmd.RunE == nil {
				t.Errorf("expected Run or RunE to be set for %q", tt.name)
			}
		})
	}
}

// =============================================================================
// Test 7: setupGitMCPServer with EOF stdin
// =============================================================================

func TestSetupGitMCPServer_EOFStdin(t *testing.T) {
	_, cleanup := setupMCPTestEnv(t)
	defer cleanup()

	shouldSkipIfRealMCPConfigExists(t)

	restoreStdin := replaceStdinWithClosedPipe(t)
	defer restoreStdin()

	mcpCfg := mcp.MCPConfig{
		Servers: make(map[string]mcp.MCPServerConfig),
		Enabled: true,
	}

	err := setupGitMCPServer(&mcpCfg, bufio.NewReader(os.Stdin))
	if err == nil {
		t.Fatal("expected error from setupGitMCPServer with EOF stdin, got nil")
	}
	errMsg := strings.ToLower(err.Error())
	if !strings.Contains(errMsg, "read") && !strings.Contains(errMsg, "eof") {
		t.Errorf("expected read/eof error, got: %v", err)
	}
}

// =============================================================================
// Test 9: setupCustomMCPServer with EOF stdin
// =============================================================================

func TestSetupCustomMCPServer_EOFStdin(t *testing.T) {
	_, cleanup := setupMCPTestEnv(t)
	defer cleanup()

	shouldSkipIfRealMCPConfigExists(t)

	restoreStdin := replaceStdinWithClosedPipe(t)
	defer restoreStdin()

	mcpCfg := mcp.MCPConfig{
		Servers: make(map[string]mcp.MCPServerConfig),
		Enabled: true,
	}
	registry := mcp.NewMCPServerRegistry()

	err := setupCustomMCPServer(&mcpCfg, bufio.NewReader(os.Stdin), registry)
	if err == nil {
		t.Fatal("expected error from setupCustomMCPServer with EOF stdin, got nil")
	}
	errMsg := strings.ToLower(err.Error())
	if !strings.Contains(errMsg, "read") && !strings.Contains(errMsg, "eof") {
		t.Errorf("expected read/eof error, got: %v", err)
	}
}

// =============================================================================
// guidedSetupFor dispatch tests
//
// These verify that the three rich guided setup functions (Git, Playwright,
// Chrome DevTools) are reachable from the `mcp add` flow — the picker shows
// these template IDs and runMCPAdd dispatches via guidedSetupFor. Each
// template ID that the registry exposes for these servers must map to the
// corresponding guided flow.
// =============================================================================

func TestGuidedSetupFor_AllTemplateIDsDispatched(t *testing.T) {
	// Every template ID that should route to a guided flow, mapped to the
	// function that must handle it. Both the canonical registry IDs and any
	// aliases (e.g. "git") are covered.
	cases := []struct {
		templateID string
	}{
		{"git"},
		{"git-uvx"},
		{"playwright"},
		{"chrome-devtools"},
	}
	for _, tc := range cases {
		t.Run(tc.templateID, func(t *testing.T) {
			fn, ok := guidedSetupFor(tc.templateID)
			if !ok {
				t.Fatalf("guidedSetupFor(%q) returned ok=false; this guided flow is unreachable from `mcp add`", tc.templateID)
			}
			if fn == nil {
				t.Fatalf("guidedSetupFor(%q) returned nil function", tc.templateID)
			}
		})
	}
}

func TestGuidedSetupFor_GenericTemplateIDsHaveNoGuidedFlow(t *testing.T) {
	// Generic templates route to the generic template-driven path, not a
	// guided flow, so guidedSetupFor must return ok=false for them.
	for _, id := range []string{"http-generic", "stdio-generic", "", "unknown"} {
		if _, ok := guidedSetupFor(id); ok {
			t.Errorf("guidedSetupFor(%q) should return ok=false", id)
		}
	}
}

func TestGuidedSetupFor_EveryGuidedSetupFunctionIsReachable(t *testing.T) {
	// All three guided setup functions must be reachable AND mapped to the
	// correct function. Build a set of the functions reached across all known
	// guided template IDs and confirm each of the three setup functions
	// appears at least once. We identify the function by a distinctive
	// install-method option string (printed via promptInstallMethod -> fmt,
	// which is reliably captured) rather than the banner, since the banner
	// is printed via differing mechanisms (console.GlyphInfo vs fmt.Println)
	// across flows.
	cases := []struct {
		templateID string
		wantOption string // distinctive substring in the captured install picker
		wantName   string // logical name for the seen-set
	}{
		{"git", "uvx (recommended)", "git"},
		{"playwright", "Official Playwright MCP Server", "playwright"},
		{"chrome-devtools", "Default settings (recommended)", "chrome-devtools"},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		fn, ok := guidedSetupFor(tc.templateID)
		if !ok || fn == nil {
			t.Fatalf("guidedSetupFor(%q) returned ok=%v fn=nil", tc.templateID, ok)
		}
		mcpCfg := mcp.MCPConfig{Servers: make(map[string]mcp.MCPServerConfig), Enabled: true}
		// "\n" advances past any pre-picker prompt (e.g. git reads the repo
		// path before its picker); the subsequent read hits EOF and the flow
		// returns an error/cancel, but the picker has already printed.
		out := testutil.CaptureStdout(t, func() {
			_ = fn(&mcpCfg, bufio.NewReader(strings.NewReader("\n")))
		})
		if !strings.Contains(out, tc.wantOption) {
			t.Errorf("guidedSetupFor(%q) did not show expected option %q; got:\n%s", tc.templateID, tc.wantOption, out)
		}
		seen[tc.wantName] = true
	}
	for _, name := range []string{"git", "playwright", "chrome-devtools"} {
		if !seen[name] {
			t.Errorf("guided setup function %q was never reached by any template ID", name)
		}
	}
}

// =============================================================================
// Direct coverage: setupPlaywrightMCPServer & setupChromeDevToolsMCPServer
// with EOF stdin (mirrors the existing setupGit EOF test).
// =============================================================================

func TestSetupPlaywrightMCPServer_EOFStdin(t *testing.T) {
	_, cleanup := setupMCPTestEnv(t)
	defer cleanup()

	shouldSkipIfRealMCPConfigExists(t)

	mcpCfg := mcp.MCPConfig{
		Servers: make(map[string]mcp.MCPServerConfig),
		Enabled: true,
	}

	err := setupPlaywrightMCPServer(&mcpCfg, bufio.NewReader(strings.NewReader("")))
	if err == nil {
		t.Fatal("expected error from setupPlaywrightMCPServer with EOF stdin, got nil")
	}
	errMsg := strings.ToLower(err.Error())
	if !strings.Contains(errMsg, "read") && !strings.Contains(errMsg, "eof") {
		t.Errorf("expected read/eof error, got: %v", err)
	}
}

func TestSetupChromeDevToolsMCPServer_EOFStdin(t *testing.T) {
	_, cleanup := setupMCPTestEnv(t)
	defer cleanup()

	shouldSkipIfRealMCPConfigExists(t)

	mcpCfg := mcp.MCPConfig{
		Servers: make(map[string]mcp.MCPServerConfig),
		Enabled: true,
	}

	err := setupChromeDevToolsMCPServer(&mcpCfg, bufio.NewReader(strings.NewReader("")))
	if err == nil {
		t.Fatal("expected error from setupChromeDevToolsMCPServer with EOF stdin, got nil")
	}
	errMsg := strings.ToLower(err.Error())
	if !strings.Contains(errMsg, "read") && !strings.Contains(errMsg, "eof") {
		t.Errorf("expected read/eof error, got: %v", err)
	}
}

// =============================================================================
// Registry: playwright template is present (needed so the picker shows it).
// =============================================================================

func TestNewMCPServerRegistry_HasPlaywrightTemplate(t *testing.T) {
	r := mcp.NewMCPServerRegistry()
	tmpl, ok := r.GetTemplate("playwright")
	if !ok {
		t.Fatal("expected 'playwright' template in registry so it appears in the mcp add picker")
	}
	if tmpl.Type != "stdio" {
		t.Errorf("expected playwright template type 'stdio', got %q", tmpl.Type)
	}
}
