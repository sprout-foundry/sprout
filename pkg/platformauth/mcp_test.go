package platformauth

import (
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// isolate points the config dir and the credential store at a temp dir.
func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPROUT_CONFIG", filepath.Join(home, ".sprout"))
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()
	t.Cleanup(credentials.ResetStorageBackend)
}

func TestConfigureMCP_AddsThePlatformServerWithTheTokenInTheStore(t *testing.T) {
	isolate(t)
	// An auto-discovered server is not written to the file by the sign-in.
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "ghp_test")
	if err := ConfigureMCP("https://app.example.com/", "spt_first"); err != nil {
		t.Fatal(err)
	}
	// Signing in again rotates the token in place.
	if err := ConfigureMCP("https://app.example.com", "spt_second"); err != nil {
		t.Fatal(err)
	}
	cfg, err := mcp.LoadGlobalMCPConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("the file holds servers the person did not add: %v", cfg.Servers)
	}
	server, ok := cfg.Servers[MCPServerName]
	if !ok {
		t.Fatalf("no %q server in %v", MCPServerName, cfg.Servers)
	}
	if server.Type != "http" || server.URL != "https://app.example.com/mcp" || !server.AutoStart || !cfg.Enabled {
		t.Fatalf("server = %+v, enabled = %v", server, cfg.Enabled)
	}
	header := server.Headers["Authorization"]
	if !mcp.IsSecretRef(header) {
		t.Fatalf("the config holds the token itself: %q", header)
	}
	stored, _, err := credentials.GetFromActiveBackend(mcp.HeaderCredentialKey(MCPServerName, "Authorization"))
	if err != nil || stored != "Bearer spt_second" {
		t.Fatalf("stored = %q, %v", stored, err)
	}

	removed, err := RemoveMCP()
	if err != nil || !removed {
		t.Fatalf("RemoveMCP = %v, %v", removed, err)
	}
	cfg, _ = mcp.LoadGlobalMCPConfigFile()
	if _, ok := cfg.Servers[MCPServerName]; ok {
		t.Fatal("the server is still configured after RemoveMCP")
	}
	if stored, _, _ := credentials.GetFromActiveBackend(mcp.HeaderCredentialKey(MCPServerName, "Authorization")); stored != "" {
		t.Fatalf("the credential survived RemoveMCP: %q", stored)
	}
	if removed, err := RemoveMCP(); err != nil || removed {
		t.Fatalf("second RemoveMCP = %v, %v", removed, err)
	}
}
