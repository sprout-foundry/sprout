package cmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/mcp"
	"github.com/spf13/cobra"
)

// =============================================================================
// Test helpers
// =============================================================================

// setupMCPTestEnv creates a temp config dir and saves/restores relevant env vars.
// Returns the temp dir path and a cleanup function.
func setupMCPTestEnv(t *testing.T) (string, func()) {
	t.Helper()
	origConfig := os.Getenv("LEDIT_CONFIG")
	origGithub := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN")
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	// Clear github token to prevent auto-discovery adding servers
	if origGithub != "" {
		t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")
	}
	cleanup := func() {
		if origGithub != "" {
			t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", origGithub)
		}
		if origConfig != "" {
			os.Setenv("LEDIT_CONFIG", origConfig)
		}
	}
	return tmpDir, cleanup
}

// shouldSkipIfRealMCPConfigExists skips the test if ~/.ledit/mcp_config.json
// already exists. Tests that modify the real MCP config cannot be safely isolated
// because pkg/mcp/config.go:getConfigDir() reads from ~/.ledit/ and does not
// respect $LEDIT_CONFIG.
func shouldSkipIfRealMCPConfigExists(t *testing.T) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	if _, err := os.Stat(filepath.Join(home, ".ledit", "mcp_config.json")); err == nil {
		t.Skipf("skipping: ~/.ledit/mcp_config.json exists; this test reads/loads the real MCP config")
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

	out := captureStdout(t, func() {
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

	out := captureStdout(t, func() {
		if err := runMCPTest(""); err != nil {
			t.Fatalf("runMCPTest returned error: %v", err)
		}
	})

	if !strings.Contains(out, "No MCP servers configured") {
		t.Errorf("expected 'No MCP servers configured' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "ledit mcp add") {
		t.Errorf("expected 'ledit mcp add' in output, got:\n%s", out)
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
	out := captureStdout(t, func() {
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
			if tt.cmd.Run == nil {
				t.Errorf("expected Run to be set for %q", tt.name)
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
// Test 8: setupGitHubMCPServer with EOF stdin
// =============================================================================

func TestSetupGitHubMCPServer_EOFStdin(t *testing.T) {
	_, cleanup := setupMCPTestEnv(t)
	defer cleanup()

	shouldSkipIfRealMCPConfigExists(t)

	restoreStdin := replaceStdinWithClosedPipe(t)
	defer restoreStdin()

	mcpCfg := mcp.MCPConfig{
		Servers: make(map[string]mcp.MCPServerConfig),
		Enabled: true,
	}

	err := setupGitHubMCPServer(&mcpCfg, bufio.NewReader(os.Stdin))
	if err == nil {
		t.Fatal("expected error from setupGitHubMCPServer with EOF stdin, got nil")
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
// Test 10: promptForGitHubToken with EOF stdin
// =============================================================================

func TestPromptForGitHubToken_EOFStdin(t *testing.T) {
	// Clear the env var so promptForGitHubToken tries to read from stdin
	origToken := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN")
	if origToken != "" {
		os.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")
		defer os.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", origToken)
	}

	restoreStdin := replaceStdinWithClosedPipe(t)
	defer restoreStdin()

	token, err := promptForGitHubToken(bufio.NewReader(os.Stdin))

	if err == nil {
		t.Fatalf("expected error from promptForGitHubToken with EOF stdin, got nil (token=%q)", token)
	}
	errMsg := strings.ToLower(err.Error())
	if !strings.Contains(errMsg, "read") && !strings.Contains(errMsg, "required") {
		t.Errorf("expected read or required error, got: %v", err)
	}
}

// =============================================================================
// Test 11: promptForGitHubToken with env var set
// =============================================================================

func TestPromptForGitHubToken_EmptyTokenInput(t *testing.T) {
	// When GITHUB_PERSONAL_ACCESS_TOKEN is set, promptForGitHubToken returns it
	// without reading stdin. Test that behavior when env var is set.
	origToken := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN")
	os.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "test-token-12345")
	defer func() {
		if origToken != "" {
			os.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", origToken)
		} else {
			os.Unsetenv("GITHUB_PERSONAL_ACCESS_TOKEN")
		}
	}()

	token, err := promptForGitHubToken(bufio.NewReader(os.Stdin))
	if err != nil {
		t.Fatalf("expected no error when env var is set, got: %v", err)
	}
	if token != "test-token-12345" {
		t.Errorf("expected token from env var, got %q", token)
	}
}
