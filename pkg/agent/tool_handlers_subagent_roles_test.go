package agent

// Item 150.3 (SP-150 §150b): the subagent spawn and parallel-dispatch paths
// resolve provider/model through the role resolver (Config.ResolveRole)
// instead of the per-setting getters. These tests pin the call-site
// behavior, including the preserved parent-inheritance gate on the RAW
// legacy subagent fields: when those fields are unset, the parent agent's
// provider/model still win (in a normal session the last-used provider is
// the conversation provider, so the two are equivalent).

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// newRolesTestAgent builds a minimal agent with an isolated config manager
// and no client: GetProvider/GetModel report "unknown", so the parent
// inheritance gate never fires and role-resolved values survive. The state
// manager is pre-set so persona lookups (GetActivePersona) work.
func newRolesTestAgent(t *testing.T) *Agent {
	t.Helper()
	mgr, err := configuration.NewManagerWithDir(t.TempDir() + "/.sprout")
	if err != nil {
		t.Fatalf("NewManagerWithDir failed: %v", err)
	}
	return &Agent{
		configManager: mgr,
		state:         NewAgentStateManager(false),
	}
}

// setRoles updates the agent's roles section in place (no save).
func setRoles(t *testing.T, a *Agent, roles map[string]configuration.RoleConfig) {
	t.Helper()
	if err := a.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.Roles = roles
		return nil
	}); err != nil {
		t.Fatalf("set roles: %v", err)
	}
}

// TestResolveSubagentProviderModel_UnknownPersonaResolvesCoderRole pins the
// not-found default branch: an unknown (or disabled) persona falls back to
// the default subagent config, which resolves through the coder role.
func TestResolveSubagentProviderModel_UnknownPersonaResolvesCoderRole(t *testing.T) {
	agent := newRolesTestAgent(t)
	setRoles(t, agent, map[string]configuration.RoleConfig{
		configuration.RoleCoder: {Provider: "openrouter", Model: "coder-role-model"},
	})

	provider, model, _, _, err := resolveSubagentProviderModel(agent, "no-such-persona-xyz", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openrouter" || model != "coder-role-model" {
		t.Errorf("unknown-persona spawn resolved to %s/%s, want the coder role (openrouter/coder-role-model)", provider, model)
	}
}

// TestResolveSubagentProviderModel_RoleEntryBeatsLegacyAlias pins the
// role-read direction at the call site: with both the legacy subagent
// settings and roles.coder set, the explicit roles entry wins (the roles
// section is authoritative), and the parent-inheritance gate stays off
// because the legacy field is set.
func TestResolveSubagentProviderModel_RoleEntryBeatsLegacyAlias(t *testing.T) {
	agent := newRolesTestAgent(t)
	if err := agent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.SubagentProvider = "zai"
		c.SubagentModel = "sub-m"
		c.Roles = map[string]configuration.RoleConfig{
			configuration.RoleCoder: {Provider: "openrouter", Model: "role-m"},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	provider, model, _, _, err := resolveSubagentProviderModel(agent, "no-such-persona-xyz", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openrouter" || model != "role-m" {
		t.Errorf("role entry must beat the legacy alias: got %s/%s, want openrouter/role-m", provider, model)
	}
}

// TestResolveSubagentProviderModel_LegacySubagentSettingsAliasCoderRole
// pins that the pre-role subagent settings still flow through the coder
// role's alias (item 150.2) on the persona path: a catalog persona with no
// explicit provider/model falls back to the general subagent getters.
func TestResolveSubagentProviderModel_LegacySubagentSettingsAliasCoderRole(t *testing.T) {
	agent := newRolesTestAgent(t)
	if err := agent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.SubagentProvider = "zai"
		c.SubagentModel = "sub-m"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	provider, model, _, _, err := resolveSubagentProviderModel(agent, "coder", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if provider != "zai" || model != "sub-m" {
		t.Errorf("coder persona resolved to %s/%s, want the legacy subagent settings (zai/sub-m)", provider, model)
	}
}

// TestResolveSubagentProviderModel_ReviewerRoleOverride pins the reviewer
// persona override: the reviewer role (SP-150 §150b) wins ahead of both the
// generic subagent settings and parent inheritance. The parent here reports
// "unknown", so the override is the only source.
func TestResolveSubagentProviderModel_ReviewerRoleOverride(t *testing.T) {
	agent := newRolesTestAgent(t)
	setRoles(t, agent, map[string]configuration.RoleConfig{
		configuration.RoleReviewer: {Provider: "openai", Model: "review-role-model"},
	})

	provider, model, _, _, err := resolveSubagentProviderModel(agent, "reviewer", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openai" || model != "review-role-model" {
		t.Errorf("reviewer persona resolved to %s/%s, want the reviewer role (openai/review-role-model)", provider, model)
	}
}

// TestResolveSubagentProviderModel_ReviewerRoleOverrideBeatsParent pins the
// override on a fully-initialized agent whose parent provider ("test")
// would otherwise win via the inheritance gate: the reviewer role still
// wins.
func TestResolveSubagentProviderModel_ReviewerRoleOverrideBeatsParent(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	setRoles(t, parent, map[string]configuration.RoleConfig{
		configuration.RoleReviewer: {Provider: "openai", Model: "review-role-model"},
	})

	provider, model, _, _, err := resolveSubagentProviderModel(parent, "reviewer", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openai" || model != "review-role-model" {
		t.Errorf("reviewer persona resolved to %s/%s, want the reviewer role ahead of the parent", provider, model)
	}
}

// TestResolveParallelSubagentConfig_RoleCoderEntryWinsOverAlias pins the
// parallel path: parallel subagents resolve through the coder role, and an
// explicit roles entry beats the legacy subagent alias. The legacy field is
// set, so the inheritance gate stays off.
func TestResolveParallelSubagentConfig_RoleCoderEntryWinsOverAlias(t *testing.T) {
	agent := newRolesTestAgent(t)
	if err := agent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.SubagentProvider = "zai"
		c.SubagentModel = "sub-m"
		c.Roles = map[string]configuration.RoleConfig{
			configuration.RoleCoder: {Provider: "openrouter", Model: "role-m"},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	provider, model := resolveParallelSubagentConfig(agent)
	if provider != "openrouter" || model != "role-m" {
		t.Errorf("parallel subagents resolved to %s/%s, want the coder role entry (openrouter/role-m)", provider, model)
	}
}

// TestResolveParallelSubagentConfig_LegacySubagentSettingsAlias pins that
// with no roles section the legacy subagent settings flow through the
// coder role's alias on the parallel path.
func TestResolveParallelSubagentConfig_LegacySubagentSettingsAlias(t *testing.T) {
	agent := newRolesTestAgent(t)
	if err := agent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.SubagentProvider = "zai"
		c.SubagentModel = "sub-m"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	provider, model := resolveParallelSubagentConfig(agent)
	if provider != "zai" || model != "sub-m" {
		t.Errorf("parallel subagents resolved to %s/%s, want the legacy subagent settings (zai/sub-m)", provider, model)
	}
}

// TestResolveParallelSubagentConfig_ParentInheritsWhenLegacyFieldsUnset
// pins the preserved gate: with the legacy subagent fields unset, the
// parent agent's provider/model win over the role-resolved fallback,
// exactly as before the role resolver (in a normal session the last-used
// provider is the conversation provider, so the result is equivalent).
func TestResolveParallelSubagentConfig_ParentInheritsWhenLegacyFieldsUnset(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	setRoles(t, parent, map[string]configuration.RoleConfig{
		configuration.RoleCoder: {Provider: "openrouter", Model: "role-m"},
	})
	// The parent's own provider/model are the test client's ("test").
	if parent.GetProvider() == "" || parent.GetProvider() == "unknown" {
		t.Fatalf("test precondition: parent provider = %q", parent.GetProvider())
	}

	provider, model := resolveParallelSubagentConfig(parent)
	if provider != parent.GetProvider() || model != parent.GetModel() {
		t.Errorf("parent inheritance broken: resolved %s/%s, want the parent's %s/%s",
			provider, model, parent.GetProvider(), parent.GetModel())
	}
}
