//go:build !js

package cmd

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/cliui"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/testutil"
)

func TestResolvePreferredCustomProviderModelAllowsNumericSelection(t *testing.T) {
	models := []configuration.ProviderDiscoveryModel{
		{ID: "qwen3.5-4b"},
		{ID: "qwen3.5-35-A3B"},
	}

	selected, err := resolvePreferredCustomProviderModel("2", models)
	if err != nil {
		t.Fatalf("expected numeric selection to succeed, got error: %v", err)
	}
	if selected != "qwen3.5-35-A3B" {
		t.Fatalf("expected second discovered model, got %q", selected)
	}
}

func TestResolvePreferredCustomProviderModelRejectsUnknownName(t *testing.T) {
	models := []configuration.ProviderDiscoveryModel{
		{ID: "qwen3.5-4b"},
	}

	if _, err := resolvePreferredCustomProviderModel("missing-model", models); err == nil {
		t.Fatal("expected unknown discovered model to fail validation")
	}
}

// TestRunCustomModelAdd_NonInteractiveGuard confirms the wizard fails
// fast with a non-interactive error when stdin isn't a terminal.
// `go test` pipes stdin, so this test naturally exercises the guard
// path without needing to fake a TTY.
func TestRunCustomModelAdd_NonInteractiveGuard(t *testing.T) {
	err := runCustomModelAdd()
	if err == nil {
		t.Fatal("expected non-interactive error from runCustomModelAdd, got nil")
	}
	if !strings.Contains(err.Error(), "non-interactive") &&
		!strings.Contains(err.Error(), "interactive terminal") {
		t.Errorf("error should mention interactive terminal requirement, got: %v", err)
	}
}

// runCustomModelAddKnown needs to be exported so tests in the same package
// can drive it directly with synthetic readers.
func TestRunCustomModelAddKnown_NoAuthRequired(t *testing.T) {
	// A "no-auth" provider like ollama shouldn't prompt for credentials.
	known := configuration.KnownProviderInfo{
		Source:         "custom",
		Name:           "my-local",
		DisplayName:    "my-local",
		EnvVar:         "",
		RequiresAPIKey: false,
		Endpoint:       "http://localhost:11434/v1",
		DefaultModel:   "llama3",
	}

	// Pass an empty reader; the function should print a message and return
	// without reading any input.
	reader := bufio.NewReader(strings.NewReader(""))
	if err := runCustomModelAddKnown(reader, known); err != nil {
		t.Errorf("runCustomModelAddKnown returned error: %v", err)
	}
}

func TestRunCustomModelAddKnown_UserDeclines(t *testing.T) {
	// User says "n" to "Set the API key now?"
	known := configuration.KnownProviderInfo{
		Source:         "custom",
		Name:           "ai-worker",
		DisplayName:    "ai-worker",
		EnvVar:         "AI_WORKER_API_KEY",
		RequiresAPIKey: true,
		Endpoint:       "http://192.168.1.134:8033/v1/chat/completions",
		DefaultModel:   "qwen3.6-27b",
		ContextSize:    200000,
	}

	// Make sure env var is unset (we explicitly want the "no credentials
	// configured" path to fire)
	t.Setenv("AI_WORKER_API_KEY", "")

	// Configure test-only credential backend so this test doesn't touch
	// the real keyring/file store.
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()

	reader := bufio.NewReader(strings.NewReader("n\n"))
	if err := runCustomModelAddKnown(reader, known); err != nil {
		t.Errorf("runCustomModelAddKnown returned error: %v", err)
	}
}

func TestRunCustomModelAddKnown_EmptyKey(t *testing.T) {
	// User says "y" to set, then submits an empty key. Should be a no-op.
	known := configuration.KnownProviderInfo{
		Source:         "custom",
		Name:           "ai-worker",
		DisplayName:    "ai-worker",
		EnvVar:         "AI_WORKER_API_KEY",
		RequiresAPIKey: true,
		Endpoint:       "http://192.168.1.134:8033/v1/chat/completions",
		DefaultModel:   "qwen3.6-27b",
	}

	t.Setenv("AI_WORKER_API_KEY", "")
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()

	// "y" + empty key — should not store anything.
	reader := bufio.NewReader(strings.NewReader("y\n\n"))
	if err := runCustomModelAddKnown(reader, known); err != nil {
		t.Errorf("runCustomModelAddKnown returned error: %v", err)
	}

	// Verify no credential was stored.
	resolved, err := credentials.ResolveProvider("ai-worker")
	if err == nil && strings.TrimSpace(resolved.Value) != "" {
		t.Errorf("Expected no credential for ai-worker, got %q", resolved.Value)
	}
}
func TestRunCustomModelAddKnown_UserAcceptsAndStoresKey(t *testing.T) {
	// User says "y" to "Set the API key now?" and provides a non-empty key.
	// The credential should land in the active backend so `/provider
	// <name>` will resolve it on the next try.
	known := configuration.KnownProviderInfo{
		Source:         "custom",
		Name:           "ai-worker",
		DisplayName:    "ai-worker",
		EnvVar:         "AI_WORKER_API_KEY",
		RequiresAPIKey: true,
		Endpoint:       "http://192.168.1.134:8033/v1/chat/completions",
		DefaultModel:   "qwen3.6-27b",
		ContextSize:    200000,
	}

	t.Setenv("AI_WORKER_API_KEY", "")
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()

	// "y" + a real-looking key. The wizard should:
	//   1. Print "Set the API key for ai-worker now via the credential backend? [Y/n]"
	//   2. Read "y\n"
	//   3. Print "API key (will be stored; or set AI_WORKER_API_KEY):"
	//   4. Read "sk-test-12345\n"
	//   5. Store via credentials.SetToActiveBackend
	//   6. Print "Stored credential for ai-worker"
	reader := bufio.NewReader(strings.NewReader("y\nsk-test-12345\n"))
	if err := runCustomModelAddKnown(reader, known); err != nil {
		t.Fatalf("runCustomModelAddKnown returned error: %v", err)
	}

	// Verify the credential was stored.
	resolved, err := credentials.ResolveProvider("ai-worker")
	if err != nil {
		t.Fatalf("ResolveProvider failed: %v", err)
	}
	if strings.TrimSpace(resolved.Value) != "sk-test-12345" {
		t.Errorf("Expected stored credential sk-test-12345, got %q", resolved.Value)
	}
}

// =============================================================================
// Test helpers
// =============================================================================

// setupMCPTestEnv creates a temp config dir and saves/restores relevant env vars.
// Returns the temp dir path and a cleanup function.
func setupMCPTestEnv(t *testing.T) (string, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	// Clear github token to prevent auto-discovery adding servers
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")
	// Disable auto-discovery to ensure empty server list in tests
	t.Setenv("SPROUT_MCP_AUTO_DISCOVER", "false")
	return tmpDir, func() {}
}

// shouldSkipIfRealMCPConfigExists skips the test if ~/.config/sprout/mcp_config.json
// already exists. Tests that modify the real MCP config cannot be safely isolated
// because pkg/mcp/config.go:getConfigDir() reads from ~/.config/sprout/ and does not
// respect $SPROUT_CONFIG.
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

// SP-048-5d
func TestBuildPromptPrefix(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"", "sprout> "},
		{"  ", "sprout> "},
		{"claude-opus-4-7", "claude-opus-4-7 ▸ "},
		{"  trim-me  ", "trim-me ▸ "},
	}
	for _, c := range cases {
		if got := cliui.BuildPromptPrefix(c.model); got != c.want {
			t.Errorf("cliui.BuildPromptPrefix(%q) = %q, want %q", c.model, got, c.want)
		}
	}
}

// SP-048-5c
func TestCompactTokens(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{-5, "0"},
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{12345, "12.3k"},
	}
	for _, c := range cases {
		if got := cliui.CompactTokens(c.in); got != c.want {
			t.Errorf("cliui.CompactTokens(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SP-048-5c
func TestCompactTokens_Boundaries(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		// Exactly 10_000 → "10.0k"
		{10000, "10.0k"},
		// 99_999 → "100.0k" (rounded)
		{99999, "100.0k"},
		// 1_000_000 → "1000.0k"
		{1_000_000, "1000.0k"},
		// Precision: 1001 → "1.0k" (truncates to 1 decimal)
		{1001, "1.0k"},
		// Precision: 1009 → "1.0k"
		{1009, "1.0k"},
		// Precision: 1010 → "1.0k"
		{1010, "1.0k"},
		// Precision: 1050 → "1.1k" (rounds up)
		{1050, "1.1k"},
	}
	for _, c := range cases {
		if got := cliui.CompactTokens(c.in); got != c.want {
			t.Errorf("cliui.CompactTokens(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SP-048-5c
func TestCompactCost(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{-0.5, "$0.00"},
		{0.0, "$0.0000"},
		{0.0023, "$0.0023"},
		{0.05, "$0.050"},
		{0.999, "$0.999"},
		{1.0, "$1.00"},
		{12.34, "$12.34"},
	}
	for _, c := range cases {
		if got := cliui.CompactCost(c.in); got != c.want {
			t.Errorf("cliui.CompactCost(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SP-048-5c
func TestCompactDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{200 * time.Millisecond, "200ms"},
		{999 * time.Millisecond, "999ms"},
		{time.Second, "1.0s"},
		{2500 * time.Millisecond, "2.5s"},
		{59*time.Second + 999*time.Millisecond, "60.0s"},
		{75 * time.Second, "1m15s"},
		{125 * time.Second, "2m5s"},
	}
	for _, c := range cases {
		if got := cliui.CompactDuration(c.in); got != c.want {
			t.Errorf("cliui.CompactDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SP-048-5a
func TestHumanizeAge(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{2 * time.Minute, "2m ago"},
		{59 * time.Minute, "59m ago"},
		{2 * time.Hour, "2h ago"},
		{23 * time.Hour, "23h ago"},
		{25 * time.Hour, "1d ago"},
		{5 * 24 * time.Hour, "5d ago"},
	}
	for _, c := range cases {
		if got := humanizeAge(c.in); got != c.want {
			t.Errorf("humanizeAge(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SP-048-5a
func TestFormatTurnStatsLine_Color(t *testing.T) {
	// Save and restore NO_COLOR so other tests aren't affected.
	old := os.Getenv("NO_COLOR")
	defer os.Setenv("NO_COLOR", old)

	// With color enabled (default): should contain ANSI dim codes.
	os.Unsetenv("NO_COLOR")
	os.Unsetenv("FORCE_COLOR")
	out := cliui.FormatTurnStatsLine(1200, 4800, 0.04, 6*time.Second+100*time.Millisecond, 0)
	if !strings.Contains(out, "\033[2m") {
		t.Errorf("with color, expected ANSI dim code in output: %q", out)
	}
	if !strings.Contains(out, "\033[0m") {
		t.Errorf("with color, expected ANSI reset code in output: %q", out)
	}
	if !strings.Contains(out, "1.2k in / 4.8k out") {
		t.Errorf("expected token summary in output: %q", out)
	}
	if !strings.Contains(out, "$0.040") {
		t.Errorf("expected cost in output: %q", out)
	}
	if !strings.Contains(out, "6.1s") {
		t.Errorf("expected duration in output: %q", out)
	}

	// With color disabled: no ANSI codes.
	os.Setenv("NO_COLOR", "1")
	out = cliui.FormatTurnStatsLine(1200, 4800, 0.04, 6*time.Second+100*time.Millisecond, 0)
	if strings.Contains(out, "\033[2m") || strings.Contains(out, "\033[0m") {
		t.Errorf("without color, no ANSI codes expected: %q", out)
	}
	if !strings.Contains(out, "1.2k in / 4.8k out") {
		t.Errorf("expected token summary in output: %q", out)
	}
}

// Time-to-first-token tests.

func TestFormatTurnStatsLine_TTFT_Hidden_WhenZero(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	out := cliui.FormatTurnStatsLine(100, 200, 0.01, 1*time.Second, 0)
	if strings.Contains(out, "ttft") {
		t.Errorf("zero ttft should not render segment, got %q", out)
	}
}

func TestFormatTurnStatsLine_TTFT_Shown_WhenNonZero(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	out := cliui.FormatTurnStatsLine(100, 200, 0.01, 2*time.Second, 800*time.Millisecond)
	if !strings.Contains(out, "ttft 800ms") {
		t.Errorf("expected 'ttft 800ms' segment, got %q", out)
	}
}

func TestFormatTurnStatsLine_TTFT_YellowAbove2s(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	out := cliui.FormatTurnStatsLine(100, 200, 0.01, 3*time.Second, 3*time.Second)
	if !strings.Contains(out, "\033[33m") {
		t.Errorf("ttft >2s should render yellow; got %q", out)
	}
}

func TestFormatTurnStatsLine_TTFT_RedAbove5s(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	out := cliui.FormatTurnStatsLine(100, 200, 0.01, 6*time.Second, 6*time.Second)
	if !strings.Contains(out, "\033[31m") {
		t.Errorf("ttft >5s should render red; got %q", out)
	}
}

func TestFormatTurnStatsLine_TTFT_NoColorWhenDisabled(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	out := cliui.FormatTurnStatsLine(100, 200, 0.01, 6*time.Second, 6*time.Second)
	if strings.Contains(out, "\033[31m") || strings.Contains(out, "\033[33m") {
		t.Errorf("NO_COLOR should suppress threshold colors; got %q", out)
	}
}

// SP-048-5a
func TestFormatTurnStatsLine_Durations(t *testing.T) {
	old := os.Getenv("NO_COLOR")
	defer os.Setenv("NO_COLOR", old)
	os.Setenv("NO_COLOR", "1") // strip ANSI for easier assertions

	// Sub-second
	out := cliui.FormatTurnStatsLine(100, 200, 0.0023, 450*time.Millisecond, 0)
	if !strings.Contains(out, "450ms") {
		t.Errorf("expected '450ms' in %q", out)
	}

	// Seconds
	out = cliui.FormatTurnStatsLine(50, 60, 0.001, 3*time.Second, 0)
	if !strings.Contains(out, "3.0s") {
		t.Errorf("expected '3.0s' in %q", out)
	}

	// Minutes
	out = cliui.FormatTurnStatsLine(500, 600, 0.12, 1*time.Minute+30*time.Second, 0)
	if !strings.Contains(out, "1m30s") {
		t.Errorf("expected '1m30s' in %q", out)
	}
}

// SP-048-5a
func TestFormatTurnStatsLine_EdgeCases(t *testing.T) {
	old := os.Getenv("NO_COLOR")
	defer os.Setenv("NO_COLOR", old)
	os.Setenv("NO_COLOR", "1")

	// Zero deltas still produce output (the caller is responsible for
	// filtering out zero-token turns).
	out := cliui.FormatTurnStatsLine(0, 0, 0, 0, 0)
	if !strings.Contains(out, "0 in / 0 out") {
		t.Errorf("expected zero stats in %q", out)
	}

	// Negative cost (shouldn't happen) — omitted entirely since
	// zero/negative cost means "no pricing for this model."
	out = cliui.FormatTurnStatsLine(100, 200, -0.5, 2*time.Second, 0)
	if strings.Contains(out, "$") {
		t.Errorf("expected no cost segment for negative cost, got %q", out)
	}
}

// SP-048-5a
func TestShouldShowTurnStats(t *testing.T) {
	// In a test harness, stderr is not a TTY (no terminal attached), so
	// cliui.ShouldShowTurnStats() must return false. This is correct: the
	// function checks stderr (not stdout) because printPerTurnSummary
	// writes to os.Stderr.
	if cliui.ShouldShowTurnStats() {
		t.Error("cliui.ShouldShowTurnStats() should return false in a non-TTY test environment")
	}
}

// SP-048-5a
func TestTruncateLabel(t *testing.T) {
	cases := []struct {
		s    string
		max  int
		want string
	}{
		{"", 10, ""},
		{"short", 10, "short"},
		{"exactlyten", 10, "exactlyten"},
		{"over the limit by a lot", 10, "over the …"},
		{"x", 1, "x"},
		{"longer", 1, "l"},
	}
	for _, c := range cases {
		if got := truncateLabel(c.s, c.max); got != c.want {
			t.Errorf("truncateLabel(%q, %d) = %q, want %q", c.s, c.max, got, c.want)
		}
	}
}

// SP-048-5b
func TestFirstRunState_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, ".sprout", "state.json")

	// Missing file → ReadFile error → nil state, error returned.
	if _, err := loadFirstRunState(statePath); err == nil {
		t.Error("loadFirstRunState should return an error when file doesn't exist")
	}

	// Save and reload.
	in := &sproutState{
		SeenFirstRunHint: []string{"/home/u/proj-a", "/home/u/proj-b"},
	}
	if err := saveFirstRunState(statePath, in); err != nil {
		t.Fatalf("saveFirstRunState: %v", err)
	}
	out, err := loadFirstRunState(statePath)
	if err != nil {
		t.Fatalf("loadFirstRunState: %v", err)
	}
	if len(out.SeenFirstRunHint) != 2 ||
		out.SeenFirstRunHint[0] != "/home/u/proj-a" ||
		out.SeenFirstRunHint[1] != "/home/u/proj-b" {
		t.Errorf("round-trip mismatch: %+v", out.SeenFirstRunHint)
	}

	// File should be valid JSON.
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	var verify map[string]any
	if err := json.Unmarshal(raw, &verify); err != nil {
		t.Errorf("file is not valid JSON: %v", err)
	}
}

// SP-048-5c
func TestCompactCost_Boundaries(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		// Exactly at the 0.01 threshold → switches from 4-decimal to 3-decimal
		{0.0099, "$0.0099"},
		{0.01, "$0.010"},
		{0.011, "$0.011"},
		// 0.9999 < 1.0 so uses $%.3f, which rounds to $1.000 (format rounding, not threshold)
		{0.9999, "$1.000"},
		// Exactly at the 1.0 threshold → switches from 3-decimal to 2-decimal
		{1.0, "$1.00"},
		{1.001, "$1.00"},
		// Large costs
		{100.50, "$100.50"},
		{999.99, "$999.99"},
		{1234.56, "$1234.56"},
	}
	for _, c := range cases {
		if got := cliui.CompactCost(c.in); got != c.want {
			t.Errorf("cliui.CompactCost(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SP-048-5c
func TestCompactDuration_Boundaries(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		// Zero duration
		{0, "0ms"},
		// Negative duration: Milliseconds() returns negative value, not clamped
		{-1 * time.Second, "-1000ms"},
		// Exact 1000ms boundary: should render as "1.0s" not "1000ms"
		{1000 * time.Millisecond, "1.0s"},
		// 59.9s still seconds
		{59900 * time.Millisecond, "59.9s"},
		// Exactly 1 minute
		{60 * time.Second, "1m0s"},
		// Just over 1 minute
		{61 * time.Second, "1m1s"},
		// Large duration: hours with remainder
		{3*time.Hour + 30*time.Minute + 45*time.Second, "210m45s"},
	}
	for _, c := range cases {
		if got := cliui.CompactDuration(c.in); got != c.want {
			t.Errorf("cliui.CompactDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SP-048-5a
func TestFormatTurnStatsLine_ExactFormat(t *testing.T) {
	old := os.Getenv("NO_COLOR")
	defer os.Setenv("NO_COLOR", old)
	os.Setenv("NO_COLOR", "1")

	out := cliui.FormatTurnStatsLine(100, 200, 0.05, 3*time.Second, 0)

	// Verify the line format: "⎯ this turn: X in / Y out · $Z · Ts ⎯\n"
	// Strip trailing newline for assertions
	out = strings.TrimSuffix(out, "\n")

	if !strings.HasPrefix(out, "⎯ this turn: ") {
		t.Errorf("expected line to start with '⎯ this turn: ', got: %q", out)
	}
	if !strings.HasSuffix(out, " ⎯") {
		t.Errorf("expected line to end with ' ⎯', got: %q", out)
	}
	if !strings.Contains(out, "·") {
		t.Errorf("expected '·' separators in output: %q", out)
	}
	// Verify the structure between delimiters
	inner := strings.TrimPrefix(out, "⎯ this turn: ")
	inner = strings.TrimSuffix(inner, " ⎯")
	// Should have exactly 2 "·" separators creating 3 segments: tokens · cost · duration
	parts := strings.Split(inner, " · ")
	if len(parts) != 3 {
		t.Errorf("expected 3 segments separated by ' · ', got %d in: %q", len(parts), inner)
	}

	// Verify specific segment contents
	if !strings.Contains(parts[0], "100 in / 200 out") {
		t.Errorf("first segment should contain token counts: %q", parts[0])
	}
	if parts[1] != "$0.050" {
		t.Errorf("second segment should be cost, got: %q", parts[1])
	}
	if parts[2] != "3.0s" {
		t.Errorf("third segment should be duration, got: %q", parts[2])
	}
}

// SP-048-5a
func TestFormatTurnStatsLine_LargeValues(t *testing.T) {
	old := os.Getenv("NO_COLOR")
	defer os.Setenv("NO_COLOR", old)
	os.Setenv("NO_COLOR", "1")

	cases := []struct {
		name       string
		prompt     int
		completion int
		cost       float64
		elapsed    time.Duration
		wants      []string // substrings that must appear
	}{
		{
			name:       "millions of tokens",
			prompt:     1_500_000,
			completion: 800_000,
			cost:       50.25,
			elapsed:    120 * time.Second,
			wants:      []string{"1500.0k in", "800.0k out", "$50.25", "2m0s"},
		},
		{
			name:       "zero cost (omitted — no pricing)",
			prompt:     100,
			completion: 200,
			cost:       0,
			elapsed:    1 * time.Second,
			wants:      []string{"100 in", "200 out", "1.0s"},
		},
		{
			name:       "sub-millisecond (0ms)",
			prompt:     50,
			completion: 100,
			cost:       0.005,
			elapsed:    500 * time.Millisecond,
			wants:      []string{"50 in", "100 out", "$0.0050", "500ms"},
		},
		{
			name:       "cost >= $1.00",
			prompt:     5000,
			completion: 3000,
			cost:       2.5,
			elapsed:    15 * time.Second,
			wants:      []string{"5.0k in", "3.0k out", "$2.50", "15.0s"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := cliui.FormatTurnStatsLine(c.prompt, c.completion, c.cost, c.elapsed, 0)
			for _, want := range c.wants {
				if !strings.Contains(out, want) {
					t.Errorf("expected %q in output: %q", want, out)
				}
			}
		})
	}
}

// SP-048-5a
func TestFormatTurnStatsLine_ForceColor(t *testing.T) {
	oldNOColor := os.Getenv("NO_COLOR")
	oldForceColor := os.Getenv("FORCE_COLOR")
	defer func() {
		os.Setenv("NO_COLOR", oldNOColor)
		os.Setenv("FORCE_COLOR", oldForceColor)
	}()

	// NO_COLOR always wins over FORCE_COLOR (no-color.org precedence).
	// When both are set, output should NOT contain ANSI codes.
	os.Setenv("NO_COLOR", "1")
	os.Setenv("FORCE_COLOR", "1")
	out := cliui.FormatTurnStatsLine(100, 200, 0.05, 3*time.Second, 0)
	if strings.Contains(out, "\033[2m") {
		t.Errorf("NO_COLOR should win over FORCE_COLOR, expected no ANSI codes in: %q", out)
	}

	// FORCE_COLOR alone (no NO_COLOR) should enable ANSI codes.
	os.Setenv("NO_COLOR", "")
	os.Setenv("FORCE_COLOR", "1")
	out = cliui.FormatTurnStatsLine(100, 200, 0.05, 3*time.Second, 0)
	if !strings.Contains(out, "\033[2m") {
		t.Errorf("FORCE_COLOR alone should enable ANSI codes, expected dim code in: %q", out)
	}
}

// SP-048-5a
func TestFormatTurnStatsLine_CostThresholds(t *testing.T) {
	old := os.Getenv("NO_COLOR")
	defer os.Setenv("NO_COLOR", old)
	os.Setenv("NO_COLOR", "1")

	cases := []struct {
		name string
		cost float64
		want string
	}{
		{"exactly $0.01", 0.01, "$0.010"},
		{"exactly $0.99", 0.99, "$0.990"},
		{"exactly $1.00", 1.0, "$1.00"},
		{"exactly $10.00", 10.0, "$10.00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := cliui.FormatTurnStatsLine(100, 200, c.cost, 1*time.Second, 0)
			if !strings.Contains(out, c.want) {
				t.Errorf("expected %q in output: %q", c.want, out)
			}
		})
	}
}

// SP-048-5a
func TestPrintPerTurnSummary_SuppressedInTestEnv(t *testing.T) {
	// In a test environment, stderr is not a TTY, so cliui.ShouldShowTurnStats()
	// returns false and printPerTurnSummary produces no output. We verify
	// this by capturing stderr. We pass nil for the agent because the early
	// return in cliui.ShouldShowTurnStats() means the agent is never dereferenced.
	old := os.Stderr
	defer func() { os.Stderr = old }()

	r, w, _ := os.Pipe()
	os.Stderr = w

	cliui.PrintPerTurnSummary(nil, time.Now().Add(-time.Second), 0, 0)

	w.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read from pipe: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("printPerTurnSummary should produce no output in non-TTY env, got: %q", got)
	}
}
