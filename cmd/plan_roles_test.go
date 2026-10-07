//go:build !js

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// writeRoleTestConfig writes cfg into the isolated test config directory
// (NewTestManager's env vars) so plannerRoleSpec's own NewManagerSilent
// load sees exactly this config and never the developer's real one.
func writeRoleTestConfig(t *testing.T, cfg *configuration.Config) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal test config: %v", err)
	}
	dir, err := configuration.GetConfigDir()
	if err != nil {
		t.Fatalf("get isolated config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, configuration.ConfigFileName), data, 0o600); err != nil {
		t.Fatalf("write isolated test config: %v", err)
	}
}

// withPlanFlags pins the package-level plan flags for the duration of the
// test and restores them on cleanup.
func withPlanFlags(t *testing.T, model, provider string) {
	t.Helper()
	origModel, origProvider := planModel, planProvider
	planModel, planProvider = model, provider
	t.Cleanup(func() {
		planModel, planProvider = origModel, origProvider
	})
}

// TestPlanningAgentSpec_FlagsBeatPlannerRole verifies
// that explicit -p/-m flags always win over the planner role, and
// that a bare -p keeps today's fall-through to the conversation model.
func TestPlanningAgentSpec_FlagsBeatPlannerRole(t *testing.T) {
	_, cleanup := configuration.NewTestManager(t)
	defer cleanup()
	writeRoleTestConfig(t, &configuration.Config{
		LastUsedProvider: "openrouter",
		ProviderModels:   map[string]string{"openrouter": "openai/gpt-5"},
		Roles:            map[string]configuration.RoleConfig{configuration.RolePlanner: {Provider: "zai", Model: "GLM-4.6"}},
	})

	withPlanFlags(t, "gpt-4o", "")
	if got := planningAgentSpec(); got != "gpt-4o" {
		t.Errorf("-m alone: planningAgentSpec() = %q, want gpt-4o (flag beats role)", got)
	}

	withPlanFlags(t, "gpt-4o", "openrouter")
	if got := planningAgentSpec(); got != "openrouter:gpt-4o" {
		t.Errorf("-p -m: planningAgentSpec() = %q, want openrouter:gpt-4o (flags beat role)", got)
	}

	withPlanFlags(t, "", "openrouter")
	if got := planningAgentSpec(); got != "" {
		t.Errorf("bare -p: planningAgentSpec() = %q, want empty (conversation fallback, as before roles)", got)
	}
}

// TestPlanningAgentSpec_PlannerRole verifies the planner role selection:
// set → the resolved "provider:model" specifier, unset → "" (the
// conversation model, exactly as today). Field fallbacks follow
// ResolveRole: an empty provider falls back to the last-used provider, an
// empty model to that provider's configured model.
func TestPlanningAgentSpec_PlannerRole(t *testing.T) {
	_, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	withPlanFlags(t, "", "")

	t.Run("both fields set", func(t *testing.T) {
		writeRoleTestConfig(t, &configuration.Config{
			LastUsedProvider: "openrouter",
			ProviderModels:   map[string]string{"openrouter": "openai/gpt-5", "zai": "GLM-4.6"},
			Roles:            map[string]configuration.RoleConfig{configuration.RolePlanner: {Provider: "zai", Model: "glm-mini"}},
		})
		if got := planningAgentSpec(); got != "zai:glm-mini" {
			t.Errorf("planningAgentSpec() = %q, want zai:glm-mini", got)
		}
	})

	t.Run("provider-only role resolves the provider's configured model", func(t *testing.T) {
		writeRoleTestConfig(t, &configuration.Config{
			LastUsedProvider: "openrouter",
			ProviderModels:   map[string]string{"openrouter": "openai/gpt-5", "zai": "GLM-4.6"},
			Roles:            map[string]configuration.RoleConfig{configuration.RolePlanner: {Provider: "zai"}},
		})
		if got := planningAgentSpec(); got != "zai:GLM-4.6" {
			t.Errorf("planningAgentSpec() = %q, want zai:GLM-4.6", got)
		}
	})

	t.Run("model-only role keeps the last-used provider", func(t *testing.T) {
		writeRoleTestConfig(t, &configuration.Config{
			LastUsedProvider: "openrouter",
			ProviderModels:   map[string]string{"openrouter": "openai/gpt-5"},
			Roles:            map[string]configuration.RoleConfig{configuration.RolePlanner: {Model: "gpt-mini"}},
		})
		if got := planningAgentSpec(); got != "openrouter:gpt-mini" {
			t.Errorf("planningAgentSpec() = %q, want openrouter:gpt-mini", got)
		}
	})

	t.Run("unset role falls back to the conversation model", func(t *testing.T) {
		writeRoleTestConfig(t, &configuration.Config{
			LastUsedProvider: "openrouter",
			ProviderModels:   map[string]string{"openrouter": "openai/gpt-5"},
		})
		if got := planningAgentSpec(); got != "" {
			t.Errorf("planningAgentSpec() = %q, want empty (conversation model)", got)
		}
	})
}

// TestCreatePlanningAgent_PlannerRoleSet is a wiring smoke test: with the
// planner role set and no flags, createPlanningAgent must succeed and still
// load the planning prompt (the role specifier routes through the normal
// constructor).
func TestCreatePlanningAgent_PlannerRoleSet(t *testing.T) {
	_, cleanup := configuration.NewTestManager(t)
	defer cleanup()
	writeRoleTestConfig(t, &configuration.Config{
		LastUsedProvider: "openrouter",
		ProviderModels:   map[string]string{"openrouter": "openai/gpt-5", "zai": "GLM-4.6"},
		Roles:            map[string]configuration.RoleConfig{configuration.RolePlanner: {Provider: "zai", Model: "GLM-4.6"}},
	})
	withPlanFlags(t, "", "")

	a, err := createPlanningAgent()
	if err != nil {
		t.Fatalf("createPlanningAgent() with planner role set returned error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
	if prompt := a.GetSystemPrompt(); prompt == "" {
		t.Error("expected non-empty planning system prompt")
	}
	// The planning loop is the planner's, so the
	// plan agent is stamped the planner role (not the default coder).
	if got := a.GetRole(); got != configuration.RolePlanner {
		t.Errorf("plan agent role = %q, want %q (stamped by createPlanningAgent)", got, configuration.RolePlanner)
	}
}
