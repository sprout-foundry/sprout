package agent

// Tests for the headers parameter on mcp_refresh add: an HTTP server can be
// configured with custom headers, and a secret-looking value is migrated to
// the credential backend rather than persisted in config.json.

import (
	"context"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

func TestMCPRefreshAdd_HTTPHeaders(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir()+"/.sprout")
	credentials.ResetStorageBackend()
	t.Cleanup(func() {
		_ = credentials.DeleteFromActiveBackend(mcp.HeaderCredentialKey("stripe", "Authorization"))
		credentials.ResetStorageBackend()
	})

	a := newTestAgent(t)
	ctx := context.Background()

	out, err := handleMCPRefresh(ctx, a, map[string]any{
		"operation": "add",
		"name":      "stripe",
		"type":      "http",
		"url":       "https://mcp.stripe.com",
		"headers": map[string]any{
			"Authorization": "Bearer sk_test_secret",
			"X-Trace":       "abc",
		},
	})
	if err != nil {
		t.Fatalf("add with headers failed: %v", err)
	}
	if strings.Contains(out, "sk_test_secret") {
		t.Fatalf("add output must not echo the secret: %s", out)
	}

	server := a.GetConfigManager().GetConfig().MCP.Servers["stripe"]
	if server.Headers["X-Trace"] != "abc" {
		t.Errorf("non-secret header should be stored verbatim, got %q", server.Headers["X-Trace"])
	}
	if !mcp.IsSecretRef(server.Headers["Authorization"]) {
		t.Errorf("Authorization must be a credential placeholder, got %q", server.Headers["Authorization"])
	}

	stored, _, err := credentials.GetFromActiveBackend(mcp.HeaderCredentialKey("stripe", "Authorization"))
	if err != nil || stored != "Bearer sk_test_secret" {
		t.Fatalf("header credential not stored correctly: value=%q err=%v", stored, err)
	}
}
