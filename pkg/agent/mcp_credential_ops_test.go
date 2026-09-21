package agent

// Tests for the mcp_refresh credential operations (set-credential /
// remove-credential): the secret must reach the credential backend, the
// config must carry only the placeholder, and a remove must clean both.

import (
	"context"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

func TestMCPRefreshSetCredential_StoresAndWires(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir()+"/.sprout")
	credentials.ResetStorageBackend()
	t.Cleanup(func() {
		_ = credentials.DeleteFromActiveBackend("mcp/figma/FIGMA_TOKEN")
		credentials.ResetStorageBackend()
	})

	a := newTestAgent(t)
	ctx := context.Background()

	addOut, err := handleMCPRefresh(ctx, a, map[string]any{
		"operation": "add",
		"name":      "figma",
		"type":      "http",
		"url":       "https://mcp.figma.com/sse",
	})
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if !strings.Contains(addOut, "added") {
		t.Errorf("unexpected add output: %s", addOut)
	}

	setOut, err := handleMCPRefresh(ctx, a, map[string]any{
		"operation": "set-credential",
		"name":      "figma",
		"env_var":   "FIGMA_TOKEN",
		"value":     "tok_foo_123",
	})
	if err != nil {
		t.Fatalf("set-credential failed: %v", err)
	}
	if strings.Contains(setOut, "tok_foo_123") {
		t.Fatalf("set-credential output must not echo the secret: %s", setOut)
	}

	// Backend holds the value.
	stored, _, err := credentials.GetFromActiveBackend("mcp/figma/FIGMA_TOKEN")
	if err != nil || stored != "tok_foo_123" {
		t.Fatalf("credential not stored correctly: value=%q err=%v", stored, err)
	}

	// Config carries only the placeholder.
	cfg := a.GetConfigManager().GetConfig()
	server := cfg.MCP.Servers["figma"]
	if server.Credentials["FIGMA_TOKEN"] != mcp.SecretRef("figma", "FIGMA_TOKEN") {
		t.Errorf("expected placeholder in Credentials, got %q", server.Credentials["FIGMA_TOKEN"])
	}
	if v, ok := server.Env["FIGMA_TOKEN"]; ok {
		t.Errorf("Env twin should be removed, got %q", v)
	}

	// list reports the credential as set.
	listOut, err := handleMCPRefresh(ctx, a, map[string]any{"operation": "list"})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if !strings.Contains(listOut, `"set"`) {
		t.Errorf("expected credential status \"set\" in list output: %s", listOut)
	}

	// remove-credential cleans both sides.
	if _, err := handleMCPRefresh(ctx, a, map[string]any{
		"operation": "remove-credential",
		"name":      "figma",
		"env_var":   "FIGMA_TOKEN",
	}); err != nil {
		t.Fatalf("remove-credential failed: %v", err)
	}
	if stored, _, _ := credentials.GetFromActiveBackend("mcp/figma/FIGMA_TOKEN"); stored != "" {
		t.Errorf("credential should be deleted from the backend, got %q", stored)
	}
	cfg2 := a.GetConfigManager().GetConfig()
	if _, still := cfg2.MCP.Servers["figma"].Credentials["FIGMA_TOKEN"]; still {
		t.Error("placeholder should be unwired after remove-credential")
	}
}

func TestMCPRefreshSetCredential_UnknownServerFails(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir()+"/.sprout")
	credentials.ResetStorageBackend()
	t.Cleanup(credentials.ResetStorageBackend)

	a := newTestAgent(t)
	_, err := handleMCPRefresh(context.Background(), a, map[string]any{
		"operation": "set-credential",
		"name":      "nosuch",
		"env_var":   "TOKEN",
		"value":     "x",
	})
	if err == nil {
		t.Fatal("expected set-credential on unknown server to fail")
	}
	if stored, _, _ := credentials.GetFromActiveBackend("mcp/nosuch/TOKEN"); stored != "" {
		t.Errorf("backend should not hold an orphaned credential, got %q", stored)
	}
}
