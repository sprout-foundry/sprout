//go:build !js

package cmd

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// A server configured before the setting existed has no recorded value, so it
// is framed by default. Editing it must report that effective value rather
// than the zero field, or the command claims the server was already opted out.
func TestApplyMCPTrustEdit_ReportsEffectiveValueBefore(t *testing.T) {
	cfg := mcp.MCPConfig{Servers: map[string]mcp.MCPServerConfig{
		"legacy": {Name: "legacy", Type: "stdio"},
	}}

	before, err := applyMCPTrustEdit(&cfg, "legacy", false)
	if err != nil {
		t.Fatalf("applyMCPTrustEdit: %v", err)
	}
	if !before {
		t.Error("before = false for an unrecorded server; want the default true")
	}
	if got := cfg.Servers["legacy"].TrustUntrustedContentOrDefault(); got {
		t.Error("opt-out did not stick")
	}
}

func TestApplyMCPTrustEdit_TogglesBothWays(t *testing.T) {
	cfg := mcp.MCPConfig{Servers: map[string]mcp.MCPServerConfig{
		"svc": {Name: "svc", Type: "stdio"},
	}}

	if _, err := applyMCPTrustEdit(&cfg, "svc", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	before, err := applyMCPTrustEdit(&cfg, "svc", true)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if before {
		t.Error("before = true when re-enabling a server that was opted out")
	}
	if !cfg.Servers["svc"].TrustUntrustedContentOrDefault() {
		t.Error("re-enable did not stick")
	}
}

func TestApplyMCPTrustEdit_UnknownServer(t *testing.T) {
	cfg := mcp.MCPConfig{Servers: map[string]mcp.MCPServerConfig{}}
	_, err := applyMCPTrustEdit(&cfg, "missing", true)
	if err == nil {
		t.Fatal("expected an error for an unknown server")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

// The flag has to exist on the command and be distinguishable from its
// absence, or `mcp edit --trust-untrusted-content=false` is indistinguishable
// from a bare `mcp edit` and could never turn framing off non-interactively.
func TestMCPEditCmd_TrustFlagIsDistinguishable(t *testing.T) {
	cmd := mcpEditCmd
	flag := cmd.Flags().Lookup("trust-untrusted-content")
	if flag == nil {
		t.Fatal("--trust-untrusted-content is not registered")
	}
	if cmd.Flags().Changed("trust-untrusted-content") {
		t.Error("flag reports as changed before any argument is parsed")
	}
}

func TestMCPEditCmd_RegisteredUnderMCP(t *testing.T) {
	found := false
	for _, c := range mcpCmd.Commands() {
		if c.Name() == "edit" {
			found = true
		}
	}
	if !found {
		t.Error("mcp edit is not registered as a subcommand of mcp")
	}
}
