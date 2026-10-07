//go:build !js

package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestPopulateAgentStats_IncludesRoleUsage pins that the /api/stats payload
// carries the per-role token/cost breakdown: each
// entry keyed by role with its tokens and cost, so the WebUI can attribute
// spend to the model role each call was made under.
func TestPopulateAgentStats_IncludesRoleUsage(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	client := agent.NewMockLLMProviderWithLimit(128_000)
	a, err := agent.NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient failed: %v", err)
	}
	t.Cleanup(func() { a.Shutdown() })

	role := a.GetRole()
	if role == "" {
		t.Fatalf("precondition: agent role = %q, want non-empty", role)
	}
	a.TrackMetricsFromResponse(200, 80, 280, 0.02, 0, 0, 0, 0)

	stats := map[string]interface{}{}
	populateAgentStats(stats, a)

	ru, ok := stats["role_usage"]
	if !ok {
		t.Fatal("stats[\"role_usage\"] missing, want the per-role breakdown")
	}
	roles, ok := ru.([]agent.RoleUsage)
	if !ok {
		t.Fatalf("stats[\"role_usage\"] has type %T, want []agent.RoleUsage", ru)
	}
	if len(roles) == 0 {
		t.Fatalf("role_usage is empty, want the per-role totals")
	}
	found := false
	for _, entry := range roles {
		if entry.Role != role {
			continue
		}
		found = true
		if entry.PromptTokens != 200 || entry.CompletionTokens != 80 || entry.Tokens != 280 {
			t.Errorf("%s role tokens = %+v, want 200 prompt / 80 completion / 280 total", entry.Role, entry)
		}
	}
	if !found {
		t.Errorf("role_usage = %+v, want an entry for role %q", roles, role)
	}
}

// TestPopulateAgentStats_EmptyRoleUsage pins that a fresh agent with no usage
// records an empty (non-nil) role_usage, so the WebUI renders the breakdown
// section without a nil map.
func TestPopulateAgentStats_EmptyRoleUsage(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	client := agent.NewMockLLMProviderWithLimit(128_000)
	a, err := agent.NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient failed: %v", err)
	}
	t.Cleanup(func() { a.Shutdown() })

	stats := map[string]interface{}{}
	populateAgentStats(stats, a)

	ru, ok := stats["role_usage"]
	if !ok {
		t.Fatal("stats[\"role_usage\"] missing, want an empty (non-nil) slice")
	}
	roles, ok := ru.([]agent.RoleUsage)
	if !ok {
		t.Fatalf("stats[\"role_usage\"] has type %T, want []agent.RoleUsage", ru)
	}
	if len(roles) != 0 {
		t.Errorf("no-usage agent role_usage = %+v, want empty", roles)
	}
}
