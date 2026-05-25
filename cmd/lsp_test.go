//go:build !js

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/lsp/proxy"
)

// =============================================================================
// runLSPList
// =============================================================================

func TestRunLSPList(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	output := captureStdout(t, func() {
		err := runLSPList()
		if err != nil {
			t.Fatalf("runLSPList() error: %v", err)
		}
	})

	if !strings.Contains(output, "Language") {
		t.Error("expected output to contain 'Language' column header")
	}
	if !strings.Contains(output, "Server Binary") {
		t.Error("expected output to contain 'Server Binary' column header")
	}
	if !strings.Contains(output, "Status") {
		t.Error("expected output to contain 'Status' column header")
	}
	// Should list known servers
	if !strings.Contains(output, "gopls") {
		t.Error("expected output to contain 'gopls'")
	}
	if !strings.Contains(output, "typescript-language-server") {
		t.Error("expected output to contain 'typescript-language-server'")
	}
	if !strings.Contains(output, "rust-analyzer") {
		t.Error("expected output to contain 'rust-analyzer'")
	}
	if !strings.Contains(output, "pylsp") {
		t.Error("expected output to contain 'pylsp'")
	}
	// Should show status for at least some servers
	if !strings.Contains(output, "installed") && !strings.Contains(output, "not found") {
		t.Error("expected output to contain 'installed' or 'not found' status")
	}
}

// =============================================================================
// runLSPInstall
// =============================================================================

func TestRunLSPInstall_Python(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	output := captureStdout(t, func() {
		err := runLSPInstall("python")
		if err != nil {
			t.Fatalf("runLSPInstall('python') error: %v", err)
		}
	})

	if !strings.Contains(output, "Language Server Installation") {
		t.Error("expected output to contain 'Language Server Installation'")
	}
	if !strings.Contains(output, "pylsp") {
		t.Error("expected output to contain 'pylsp' binary")
	}
	if !strings.Contains(output, "pip install") {
		t.Error("expected output to contain 'pip install' instruction")
	}
}

func TestRunLSPInstall_Go(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	output := captureStdout(t, func() {
		err := runLSPInstall("go")
		if err != nil {
			t.Fatalf("runLSPInstall('go') error: %v", err)
		}
	})

	if !strings.Contains(output, "gopls") {
		t.Error("expected output to contain 'gopls'")
	}
	if !strings.Contains(output, "go install") {
		t.Error("expected output to contain 'go install' instruction")
	}
}

func TestRunLSPInstall_TypeScript(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	output := captureStdout(t, func() {
		err := runLSPInstall("typescript")
		if err != nil {
			t.Fatalf("runLSPInstall('typescript') error: %v", err)
		}
	})

	if !strings.Contains(output, "typescript-language-server") {
		t.Error("expected output to contain 'typescript-language-server'")
	}
	if !strings.Contains(output, "npm install") {
		t.Error("expected output to contain 'npm install' instruction")
	}
}

func TestRunLSPInstall_CaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	output := captureStdout(t, func() {
		err := runLSPInstall("Go")
		if err != nil {
			t.Fatalf("runLSPInstall('Go') error: %v", err)
		}
	})

	if !strings.Contains(output, "gopls") {
		t.Error("expected case-insensitive match for 'Go' to find gopls")
	}
}

func TestRunLSPInstall_WhitespaceTrimmed(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	output := captureStdout(t, func() {
		err := runLSPInstall("  rust  ")
		if err != nil {
			t.Fatalf("runLSPInstall('  rust  ') error: %v", err)
		}
	})

	if !strings.Contains(output, "rust-analyzer") {
		t.Error("expected whitespace-trimmed 'rust' to find rust-analyzer")
	}
}

func TestRunLSPInstall_UnknownLanguage(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	err := runLSPInstall("nonexistent")
	if err == nil {
		t.Error("expected error for unknown language 'nonexistent'")
	}
}

func TestRunLSPInstall_EmptyLanguage(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	err := runLSPInstall("")
	if err == nil {
		t.Error("expected error for empty language string")
	}
}

func TestRunLSPInstall_Shell(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	output := captureStdout(t, func() {
		err := runLSPInstall("shell")
		if err != nil {
			t.Fatalf("runLSPInstall('shell') error: %v", err)
		}
	})

	if !strings.Contains(output, "bash-language-server") {
		t.Error("expected output to contain 'bash-language-server'")
	}
	if !strings.Contains(output, "npm install") {
		t.Error("expected output to contain 'npm install' for shell language server")
	}
}

func TestRunLSPInstall_WithCustomOverride(t *testing.T) {
	// Create a config with a custom server override
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	// Write a config.json with a custom language server override
	configPath := filepath.Join(tmpDir, "config.json")
	configData := `{
		"version": "2.0",
		"language_servers": [
			{
				"id": "go",
				"binary": "custom-gopls",
				"args": ["--custom-flag"],
				"language_ids": ["go"]
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configData), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	output := captureStdout(t, func() {
		err := runLSPInstall("go")
		if err != nil {
			t.Fatalf("runLSPInstall('go') error: %v", err)
		}
	})

	// Should show the custom binary, not the default
	if !strings.Contains(output, "custom-gopls") {
		t.Error("expected output to contain custom binary 'custom-gopls'")
	}
	if !strings.Contains(output, "--custom-flag") {
		t.Error("expected output to contain custom args '--custom-flag'")
	}
}

// =============================================================================
// runLSPStatus
// =============================================================================

func TestRunLSPStatus(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	output := captureStdout(t, func() {
		err := runLSPStatus()
		if err != nil {
			t.Fatalf("runLSPStatus() error: %v", err)
		}
	})

	if !strings.Contains(output, "Language Server Status") {
		t.Error("expected output to contain 'Language Server Status'")
	}
	if !strings.Contains(output, "Language:") {
		t.Error("expected output to contain 'Language:' field")
	}
	if !strings.Contains(output, "Binary:") {
		t.Error("expected output to contain 'Binary:' field")
	}
	if !strings.Contains(output, "Status:") {
		t.Error("expected output to contain 'Status:' field")
	}
	if !strings.Contains(output, "Install:") {
		t.Error("expected output to contain 'Install:' field")
	}
	// Should show at least some server info
	if !strings.Contains(output, "gopls") {
		t.Error("expected output to contain 'gopls'")
	}
}

// =============================================================================
// loadLanguageServers
// =============================================================================

func TestLoadLanguageServers(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	servers := loadLanguageServers()

	if len(servers) == 0 {
		t.Error("expected at least one language server")
	}

	// Should contain known server IDs
	serverIDs := make(map[string]bool)
	for _, s := range servers {
		serverIDs[s.ID] = true
	}

	expectedIDs := []string{"go", "typescript", "python", "rust"}
	for _, id := range expectedIDs {
		if !serverIDs[id] {
			t.Errorf("expected server ID %q to be present", id)
		}
	}
}

func TestLoadLanguageServers_WithNoConfig(t *testing.T) {
	// Point config to a non-existent dir so LoadOrInitConfig fails
	t.Setenv("LEDIT_CONFIG", "/nonexistent/sprout-test")
	t.Setenv("SPROUT_CONFIG", "/nonexistent/sprout-test")

	servers := loadLanguageServers()

	// Should still return defaults when config can't be loaded
	if len(servers) == 0 {
		t.Error("expected default servers even when config is unavailable")
	}

	// Should still have the go server
	found := false
	for _, s := range servers {
		if s.ID == "go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'go' server in defaults")
	}
}

func TestLoadLanguageServers_WithCustomOverride(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	// Write a config.json with a custom language server override
	configPath := filepath.Join(tmpDir, "config.json")
	configData := `{
		"version": "2.0",
		"language_servers": [
			{
				"id": "go",
				"binary": "my-gopls",
				"args": ["--stdio"],
				"language_ids": ["go"]
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(configData), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	servers := loadLanguageServers()

	// Find the go server and check it was overridden
	found := false
	for _, s := range servers {
		if s.ID == "go" {
			found = true
			if s.Binary != "my-gopls" {
				t.Errorf("expected overridden binary 'my-gopls', got %q", s.Binary)
			}
			break
		}
	}
	if !found {
		t.Error("expected 'go' server to be present after override")
	}
}

// =============================================================================
// langServerStatus
// =============================================================================

func TestLangServerStatus_Installed(t *testing.T) {
	// gopls is likely installed in this environment
	servers := loadLanguageServers()
	for _, s := range servers {
		if s.ID == "go" {
			status := langServerStatus(s)
			if status != "installed" && status != "not found" {
				t.Errorf("unexpected status %q, expected 'installed' or 'not found'", status)
			}
			return
		}
	}
	t.Fatal("could not find 'go' server to test status")
}

func TestLangServerStatus_NotFound(t *testing.T) {
	servers := loadLanguageServers()
	// Find a server that's unlikely to be installed
	for _, s := range servers {
		if s.ID == "csharp" {
			status := langServerStatus(s)
			// Could be installed on some systems, just verify the output is valid
			if status != "installed" && status != "not found" {
				t.Errorf("unexpected status %q, expected 'installed' or 'not found'", status)
			}
			return
		}
	}
	t.Fatal("could not find 'csharp' server to test status")
}

// =============================================================================
// LSP command integration (cobra command flow)
// =============================================================================

func TestLspCmd_NoArgs_ShowsHelp(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	output := captureStdout(t, func() {
		lspCmd.Run(lspCmd, []string{})
	})

	// Running lsp with no args should show help text
	if !strings.Contains(output, "lsp") {
		t.Error("expected help output to contain 'lsp'")
	}
}

// =============================================================================
// Verify proxy package imports are correct (compile-time check)
// =============================================================================

func TestProxyPackageTypes(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	// Verify that loadLanguageServers returns proxy.LanguageServerConfig
	servers := loadLanguageServers()
	if len(servers) == 0 {
		t.Fatal("no servers loaded")
	}

	// Check that we can access fields on the returned type
	for _, s := range servers {
		_ = s.ID
		_ = s.Binary
		_ = s.Args
		_ = s.LanguageIDs
		_ = s.InstallHint
	}
}

// =============================================================================
// normalize language ID edge cases
// =============================================================================

func TestRunLSPInstall_UpperCaseLanguage(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	// Test with various case variations
	for _, lang := range []string{"PYTHON", "Python", "pYtHoN"} {
		err := runLSPInstall(lang)
		if err != nil {
			t.Errorf("runLSPInstall(%q) error: %v", lang, err)
		}
	}
}

// Ensure we can find servers by their IDs
func TestFindServerByID(t *testing.T) {
	servers := proxy.DefaultLanguageServers()

	// Find a known server
	goServer := proxy.FindLanguageServerByID("go", servers)
	if goServer == nil {
		t.Error("expected to find 'go' server by ID")
	}
	if goServer != nil && goServer.Binary != "gopls" {
		t.Errorf("expected 'gopls' binary, got %q", goServer.Binary)
	}

	// Find a non-existent server
	missing := proxy.FindLanguageServerByID("nonexistent", servers)
	if missing != nil {
		t.Error("expected nil for non-existent server ID")
	}
}

// Test the language ID lookup (not the server ID)
func TestFindLanguageServerByID(t *testing.T) {
	servers := proxy.DefaultLanguageServers()

	// Find by language ID (e.g., "javascript" is part of the typescript server)
	tsServer := proxy.FindLanguageServer("javascript", servers)
	if tsServer == nil {
		t.Error("expected to find 'javascript' language server")
	}
	if tsServer != nil && tsServer.ID != "typescript" {
		t.Errorf("expected 'typescript' server for 'javascript', got %q", tsServer.ID)
	}

	// Find a language that doesn't exist
	missing := proxy.FindLanguageServer("nonexistent", servers)
	if missing != nil {
		t.Error("expected nil for non-existent language")
	}
}
