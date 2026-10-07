package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// These tests exercise the coder-role gate in
// newAgentWithConfigManagerInner: the main conversation loop uses the
// explicitly-set roles.coder entry, explicit model arguments always win,
// and legacy aliases must not leak into the primary conversation. They
// reuse cliPathTestEnv (agent_creation_test.go) for a real GenericProvider
// backed by httptest, so the agent's provider/model can be asserted
// directly.

// cliRoleModel is the model the tests assign to the coder role — distinct
// from cliTestModel (the conversation's model) so a role that is NOT
// applied is observable.
const cliRoleModel = "cli-coder-role-model"

// setRoleTestConfig mutates the cliPathTestEnv manager's config in place.
func setRoleTestConfig(t *testing.T, manager *configuration.Manager, mutate func(cfg *configuration.Config)) {
	t.Helper()
	if err := manager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		mutate(cfg)
		return nil
	}); err != nil {
		t.Fatalf("update test config: %v", err)
	}
}

// TestCoderRole_ExplicitEntrySelectsMainLoop verifies case (a): with
// roles.coder explicitly set and no explicit model, the created agent uses
// the role's provider/model instead of the conversation's.
func TestCoderRole_ExplicitEntrySelectsMainLoop(t *testing.T) {
	manager, _, cleanup := cliPathTestEnv(t, 200_000)
	defer cleanup()
	setRoleTestConfig(t, manager, func(cfg *configuration.Config) {
		cfg.Roles = map[string]configuration.RoleConfig{
			configuration.RoleCoder: {Provider: cliTestProviderName, Model: cliRoleModel},
		}
	})

	ag, err := newAgentWithConfigManagerInner(manager, t.TempDir(), "")
	if err != nil {
		t.Fatalf("newAgentWithConfigManagerInner failed: %v", err)
	}
	defer ag.Shutdown()

	if got := ag.GetProviderType(); got != api.LMStudioClientType {
		t.Errorf("GetProviderType() = %q, want %q (role provider)", got, api.LMStudioClientType)
	}
	if got := ag.GetModel(); got != cliRoleModel {
		t.Errorf("GetModel() = %q, want %q (role model, not the conversation's %q)", got, cliRoleModel, cliTestModel)
	}
}

// TestCoderRole_UnsetKeepsConversation verifies case (b): with no roles
// section at all, the main loop keeps the conversation's provider/model
// exactly as before roles.
func TestCoderRole_UnsetKeepsConversation(t *testing.T) {
	manager, _, cleanup := cliPathTestEnv(t, 200_000)
	defer cleanup()

	ag, err := newAgentWithConfigManagerInner(manager, t.TempDir(), "")
	if err != nil {
		t.Fatalf("newAgentWithConfigManagerInner failed: %v", err)
	}
	defer ag.Shutdown()

	if got := ag.GetProviderType(); got != api.LMStudioClientType {
		t.Errorf("GetProviderType() = %q, want %q (conversation provider)", got, api.LMStudioClientType)
	}
	if got := ag.GetModel(); got != cliTestModel {
		t.Errorf("GetModel() = %q, want %q (conversation model)", got, cliTestModel)
	}
}

// TestCoderRole_LegacyAliasDoesNotLeak verifies case (c): legacy subagent
// settings alias the coder role for RESOLUTION but must not
// select the primary conversation's model — only an explicit roles.coder
// entry does that. A subagent_model setting alone leaves the conversation
// model untouched.
func TestCoderRole_LegacyAliasDoesNotLeak(t *testing.T) {
	manager, _, cleanup := cliPathTestEnv(t, 200_000)
	defer cleanup()
	setRoleTestConfig(t, manager, func(cfg *configuration.Config) {
		cfg.SubagentProvider = cliTestProviderName
		cfg.SubagentModel = cliRoleModel
		// The completion alias must not leak either.
		cfg.CompletionModel = cliRoleModel
	})

	ag, err := newAgentWithConfigManagerInner(manager, t.TempDir(), "")
	if err != nil {
		t.Fatalf("newAgentWithConfigManagerInner failed: %v", err)
	}
	defer ag.Shutdown()

	if got := ag.GetModel(); got != cliTestModel {
		t.Errorf("GetModel() = %q, want %q — legacy aliases must not select the main conversation's model", got, cliTestModel)
	}

	// Sanity: the same settings DO alias the role for ResolveRole — the
	// leak is specifically in the main-loop gate.
	if provider, model := manager.GetConfig().ResolveRole(configuration.RoleCoder); provider != cliTestProviderName || model != cliRoleModel {
		t.Errorf("ResolveRole(coder) with legacy aliases = (%q, %q), want (%q, %q)", provider, model, cliTestProviderName, cliRoleModel)
	}
}

// TestCoderRole_ExplicitModelWins verifies case (d): an explicit model
// argument (a --model flag) beats the configured coder role.
func TestCoderRole_ExplicitModelWins(t *testing.T) {
	manager, _, cleanup := cliPathTestEnv(t, 200_000)
	defer cleanup()
	setRoleTestConfig(t, manager, func(cfg *configuration.Config) {
		cfg.Roles = map[string]configuration.RoleConfig{
			configuration.RoleCoder: {Provider: cliTestProviderName, Model: cliRoleModel},
		}
	})

	ag, err := newAgentWithConfigManagerInner(manager, t.TempDir(), cliTestModel)
	if err != nil {
		t.Fatalf("newAgentWithConfigManagerInner with explicit model failed: %v", err)
	}
	defer ag.Shutdown()

	if got := ag.GetModel(); got != cliTestModel {
		t.Errorf("GetModel() = %q, want %q (explicit model beats the role)", got, cliTestModel)
	}
}

// TestCoderRole_ExplicitSpecifierWins verifies case (d) for the
// "provider:model" specifier form the CLI surfaces pass (e.g.
// --provider/--model composed by providerModelSpec): the role is ignored.
func TestCoderRole_ExplicitSpecifierWins(t *testing.T) {
	manager, _, cleanup := cliPathTestEnv(t, 200_000)
	defer cleanup()
	setRoleTestConfig(t, manager, func(cfg *configuration.Config) {
		cfg.Roles = map[string]configuration.RoleConfig{
			configuration.RoleCoder: {Provider: cliTestProviderName, Model: cliRoleModel},
		}
	})

	ag, err := newAgentWithConfigManagerInner(manager, t.TempDir(), cliTestProviderName+":"+cliTestModel)
	if err != nil {
		t.Fatalf("newAgentWithConfigManagerInner with explicit specifier failed: %v", err)
	}
	defer ag.Shutdown()

	if got := ag.GetModel(); got != cliTestModel {
		t.Errorf("GetModel() = %q, want %q (explicit specifier beats the role)", got, cliTestModel)
	}
}

// TestCoderRole_ProviderOnlyResolvesProviderModel verifies the field
// fallback: a roles.coder entry with only a provider resolves to that
// provider's configured model — the same value the conversation would
// resolve to, so the gate firing is observable through the provider type.
func TestCoderRole_ProviderOnlyResolvesProviderModel(t *testing.T) {
	manager, _, cleanup := cliPathTestEnv(t, 200_000)
	defer cleanup()
	setRoleTestConfig(t, manager, func(cfg *configuration.Config) {
		cfg.Roles = map[string]configuration.RoleConfig{
			configuration.RoleCoder: {Provider: cliTestProviderName},
		}
	})

	ag, err := newAgentWithConfigManagerInner(manager, t.TempDir(), "")
	if err != nil {
		t.Fatalf("newAgentWithConfigManagerInner failed: %v", err)
	}
	defer ag.Shutdown()

	if got := ag.GetProviderType(); got != api.LMStudioClientType {
		t.Errorf("GetProviderType() = %q, want %q (role provider)", got, api.LMStudioClientType)
	}
	if got := ag.GetModel(); got != cliTestModel {
		t.Errorf("GetModel() = %q, want %q (the provider's configured model)", got, cliTestModel)
	}
}
