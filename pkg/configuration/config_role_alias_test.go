package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file pins item 150.2: the existing per-setting model fields
// (subagent_model, commit_model, review and completion models) are read as
// aliases for their roles (SP-150 §150a). The alias mapping:
//
//	subagent settings  ⇔ coder role
//	commit settings    ⇔ commit role
//	review settings    ⇔ reviewer role
//	completion settings ⇔ coder role (no completion-specific role exists)
//
// The binding precedence: in a legacy getter an explicit legacy field always
// wins and the roles section only fills what the legacy field left unset, so
// a pre-role config behaves byte-for-byte as before. In the role-read
// direction (ResolveRole) an explicit roles-section entry wins over the
// legacy alias. TestRoleAlias_LegacyOnlyNoBreakage pins the no-breakage
// guarantee end to end.

// TestRoleAlias_Commit covers the commit_model/commit_provider ⇔ commit role
// alias. Each case asserts GetCommitModel, GetCommitProvider and
// ResolveRole(RoleCommit) agree.
func TestRoleAlias_Commit(t *testing.T) {
	t.Run("role_model_only_fills_model", func(t *testing.T) {
		cfg := &Config{
			LastUsedProvider: "openrouter",
			ProviderModels:   map[string]string{"openrouter": "openai/gpt-5"},
			Roles:            map[string]RoleConfig{RoleCommit: {Model: "gpt-4o"}},
		}
		// Model field comes from the role; provider falls back to last-used
		// because the role left the provider unset.
		assert.Equal(t, "gpt-4o", cfg.GetCommitModel())
		assert.Equal(t, "openrouter", cfg.GetCommitProvider())
		provider, model := cfg.ResolveRole(RoleCommit)
		assert.Equal(t, "openrouter", provider)
		assert.Equal(t, "gpt-4o", model)
	})

	t.Run("role_provider_only_fills_provider", func(t *testing.T) {
		cfg := &Config{
			LastUsedProvider: "openrouter",
			ProviderModels:   map[string]string{"openai": "gpt-4"},
			Roles:            map[string]RoleConfig{RoleCommit: {Provider: "openai"}},
		}
		// Provider field from the role; model resolves through that provider.
		assert.Equal(t, "openai", cfg.GetCommitProvider())
		assert.Equal(t, "gpt-4", cfg.GetCommitModel())
		provider, model := cfg.ResolveRole(RoleCommit)
		assert.Equal(t, "openai", provider)
		assert.Equal(t, "gpt-4", model)
	})

	t.Run("role_both_set", func(t *testing.T) {
		cfg := &Config{
			ProviderModels: map[string]string{"openai": "gpt-4"},
			Roles:          map[string]RoleConfig{RoleCommit: {Provider: "openai", Model: "gpt-4-turbo"}},
		}
		assert.Equal(t, "openai", cfg.GetCommitProvider())
		assert.Equal(t, "gpt-4-turbo", cfg.GetCommitModel())
		provider, model := cfg.ResolveRole(RoleCommit)
		assert.Equal(t, "openai", provider)
		assert.Equal(t, "gpt-4-turbo", model)
	})

	t.Run("legacy_commit_fields_win_over_role", func(t *testing.T) {
		cfg := &Config{
			CommitProvider: "legacy-p",
			CommitModel:    "legacy-m",
			Roles:          map[string]RoleConfig{RoleCommit: {Provider: "role-p", Model: "role-m"}},
		}
		// An explicit legacy field always wins over the roles section.
		assert.Equal(t, "legacy-p", cfg.GetCommitProvider())
		assert.Equal(t, "legacy-m", cfg.GetCommitModel())
	})

	t.Run("role_fills_unset_legacy_field", func(t *testing.T) {
		cfg := &Config{
			CommitModel: "legacy-m", // provider left unset
			Roles:       map[string]RoleConfig{RoleCommit: {Provider: "role-p"}},
		}
		// Legacy model wins; the role fills the provider field it left unset.
		assert.Equal(t, "legacy-m", cfg.GetCommitModel())
		assert.Equal(t, "role-p", cfg.GetCommitProvider())
	})

	t.Run("all_empty_conversation_default", func(t *testing.T) {
		// No roles, no legacy commit fields: today's shape (last-used
		// provider + its configured model).
		cfg := &Config{
			LastUsedProvider: "openrouter",
			ProviderModels:   map[string]string{"openrouter": "openai/gpt-5"},
		}
		assert.Equal(t, "openrouter", cfg.GetCommitProvider())
		assert.Equal(t, "openai/gpt-5", cfg.GetCommitModel())
		provider, model := cfg.ResolveRole(RoleCommit)
		assert.Equal(t, "openrouter", provider)
		assert.Equal(t, "openai/gpt-5", model)
	})
}

// TestRoleAlias_Review covers the review_model/review_provider ⇔ reviewer
// role alias and pins that review keeps its no-last-used-provider shape.
func TestRoleAlias_Review(t *testing.T) {
	t.Run("role_model_only_fills_model", func(t *testing.T) {
		cfg := &Config{
			LastUsedProvider: "openrouter",
			ProviderModels:   map[string]string{"openrouter": "openai/gpt-5"},
			Roles:            map[string]RoleConfig{RoleReviewer: {Model: "gpt-4-turbo"}},
		}
		assert.Equal(t, "gpt-4-turbo", cfg.GetReviewModel())
		// Review has NO last-used fallback: provider stays empty even though
		// LastUsedProvider is set and the role left the provider unset.
		assert.Equal(t, "", cfg.GetReviewProvider())
	})

	t.Run("role_provider_only_fills_provider", func(t *testing.T) {
		cfg := &Config{
			ProviderModels: map[string]string{"zai": "GLM-4.6"},
			Roles:          map[string]RoleConfig{RoleReviewer: {Provider: "zai"}},
		}
		assert.Equal(t, "zai", cfg.GetReviewProvider())
		assert.Equal(t, "GLM-4.6", cfg.GetReviewModel())
		provider, model := cfg.ResolveRole(RoleReviewer)
		assert.Equal(t, "zai", provider)
		assert.Equal(t, "GLM-4.6", model)
	})

	t.Run("role_both_set", func(t *testing.T) {
		cfg := &Config{
			Roles: map[string]RoleConfig{RoleReviewer: {Provider: "openai", Model: "gpt-4"}},
		}
		assert.Equal(t, "openai", cfg.GetReviewProvider())
		assert.Equal(t, "gpt-4", cfg.GetReviewModel())
	})

	t.Run("legacy_review_fields_win_over_role", func(t *testing.T) {
		cfg := &Config{
			ReviewProvider: "legacy-p",
			ReviewModel:    "legacy-m",
			Roles:          map[string]RoleConfig{RoleReviewer: {Provider: "role-p", Model: "role-m"}},
		}
		assert.Equal(t, "legacy-p", cfg.GetReviewProvider())
		assert.Equal(t, "legacy-m", cfg.GetReviewModel())
	})

	t.Run("all_empty_review_provider_stays_empty", func(t *testing.T) {
		// No roles, no review fields: the no-last-used shape is preserved —
		// the provider stays empty even with a last-used provider set.
		cfg := &Config{
			LastUsedProvider: "openrouter",
			ProviderModels:   map[string]string{"openrouter": "openai/gpt-5"},
		}
		assert.Equal(t, "", cfg.GetReviewProvider())
		assert.Equal(t, "", cfg.GetReviewModel())
	})
}

// TestRoleAlias_Subagent covers the subagent_model/subagent_provider ⇔ coder
// role alias, including the transitive type-specific getters.
func TestRoleAlias_Subagent(t *testing.T) {
	// LastUsedProvider is deliberately set to a different provider so a
	// provider-less role entry (which resolves through the last-used
	// provider) is distinguishable from a provider-set one.
	base := func() *Config {
		return &Config{
			LastUsedProvider: "zai",
			ProviderModels: map[string]string{
				"zai":        "GLM-4.6",
				"openrouter": "openai/gpt-5",
			},
		}
	}

	t.Run("role_model_only_fills_model", func(t *testing.T) {
		cfg := base()
		cfg.Roles = map[string]RoleConfig{RoleCoder: {Model: "coder-m"}}
		// Model from the role; provider is left unset (no subagent provider,
		// no role provider) so it resolves through the last-used provider.
		assert.Equal(t, "coder-m", cfg.GetSubagentModel())
		assert.Equal(t, "", cfg.GetSubagentProvider())
		provider, model := cfg.ResolveRole(RoleCoder)
		assert.Equal(t, "zai", provider)
		assert.Equal(t, "coder-m", model)
	})

	t.Run("role_provider_only_fills_provider", func(t *testing.T) {
		cfg := base()
		cfg.Roles = map[string]RoleConfig{RoleCoder: {Provider: "openrouter"}}
		assert.Equal(t, "openrouter", cfg.GetSubagentProvider())
		// Model resolves through the role's provider (openrouter), not the
		// last-used one (zai) — this is what LastUsedProvider distinguishes.
		assert.Equal(t, "openai/gpt-5", cfg.GetSubagentModel())
	})

	t.Run("role_both_set", func(t *testing.T) {
		cfg := base()
		cfg.Roles = map[string]RoleConfig{RoleCoder: {Provider: "openrouter", Model: "gpt-4o"}}
		assert.Equal(t, "openrouter", cfg.GetSubagentProvider())
		assert.Equal(t, "gpt-4o", cfg.GetSubagentModel())
	})

	t.Run("legacy_subagent_fields_win_over_role", func(t *testing.T) {
		cfg := base()
		cfg.SubagentProvider = "legacy-p"
		cfg.SubagentModel = "legacy-m"
		cfg.Roles = map[string]RoleConfig{RoleCoder: {Provider: "role-p", Model: "role-m"}}
		assert.Equal(t, "legacy-p", cfg.GetSubagentProvider())
		assert.Equal(t, "legacy-m", cfg.GetSubagentModel())
	})

	t.Run("type_specific_model_inherits_role_transitively", func(t *testing.T) {
		// A type with no explicit model falls through to the general
		// subagent getter, which aliases the coder role.
		cfg := base()
		cfg.SubagentTypes = map[string]SubagentType{"coder": {ID: "coder"}}
		cfg.Roles = map[string]RoleConfig{RoleCoder: {Provider: "openrouter", Model: "gpt-4o"}}
		assert.Equal(t, "gpt-4o", cfg.GetSubagentTypeModel("coder"))
		assert.Equal(t, "openrouter", cfg.GetSubagentTypeProvider("coder"))
	})

	t.Run("all_empty_today_shape", func(t *testing.T) {
		// No roles, no subagent fields: provider empty (inherit) and model
		// empty — exactly today's shape.
		cfg := base()
		assert.Equal(t, "", cfg.GetSubagentProvider())
		assert.Equal(t, "", cfg.GetSubagentModel())
	})
}

// TestRoleAlias_Completion covers the completion_model/completion_provider ⇔
// coder role alias (inline completion is code generation; the built-in set
// has no completion-specific role).
func TestRoleAlias_Completion(t *testing.T) {
	t.Run("coder_role_fills_completion", func(t *testing.T) {
		cfg := &Config{
			ProviderModels: map[string]string{"openai": "gpt-4"},
			Roles:          map[string]RoleConfig{RoleCoder: {Provider: "openai", Model: "gpt-4-turbo"}},
		}
		assert.Equal(t, "openai", cfg.GetCompletionProvider())
		assert.Equal(t, "gpt-4-turbo", cfg.GetCompletionModel())
	})

	t.Run("coder_role_model_only", func(t *testing.T) {
		cfg := &Config{
			Roles: map[string]RoleConfig{RoleCoder: {Model: "completion-m"}},
		}
		// Completion keeps its no-last-used shape: provider stays empty when
		// the coder role left it unset.
		assert.Equal(t, "completion-m", cfg.GetCompletionModel())
		assert.Equal(t, "", cfg.GetCompletionProvider())
	})

	t.Run("legacy_completion_fields_win_over_role", func(t *testing.T) {
		cfg := &Config{
			CompletionProvider: "legacy-p",
			CompletionModel:    "legacy-m",
			Roles:              map[string]RoleConfig{RoleCoder: {Provider: "role-p", Model: "role-m"}},
		}
		assert.Equal(t, "legacy-p", cfg.GetCompletionProvider())
		assert.Equal(t, "legacy-m", cfg.GetCompletionModel())
	})

	t.Run("all_empty_main_provider_path_preserved", func(t *testing.T) {
		// No roles, no completion fields: both empty — the caller uses the
		// main provider. No last-used fallback is introduced.
		cfg := &Config{
			LastUsedProvider: "openrouter",
			ProviderModels:   map[string]string{"openrouter": "openai/gpt-5"},
		}
		assert.Equal(t, "", cfg.GetCompletionProvider())
		assert.Equal(t, "", cfg.GetCompletionModel())
	})
}

// TestRoleSelection_PreferenceOrder pins the role-read direction: an explicit
// roles-section entry beats the legacy aliases, and among the coder's aliases
// subagent precedes completion. Planner and summarizer have no legacy alias.
func TestRoleSelection_PreferenceOrder(t *testing.T) {
	shared := map[string]string{"zai": "GLM-4.6", "openrouter": "openai/gpt-5"}

	t.Run("coder_reads_subagent_settings", func(t *testing.T) {
		cfg := &Config{
			LastUsedProvider: "zai",
			ProviderModels:   shared,
			SubagentProvider: "openrouter",
			SubagentModel:    "sub-m",
		}
		provider, model := cfg.ResolveRole(RoleCoder)
		assert.Equal(t, "openrouter", provider)
		assert.Equal(t, "sub-m", model)
	})

	t.Run("coder_reads_completion_when_no_subagent", func(t *testing.T) {
		cfg := &Config{
			LastUsedProvider: "zai",
			ProviderModels:   shared,
			CompletionModel:  "completion-m",
		}
		// No subagent settings, so the coder alias falls through to the
		// completion settings.
		provider, model := cfg.ResolveRole(RoleCoder)
		assert.Equal(t, "zai", provider)
		assert.Equal(t, "completion-m", model)
	})

	t.Run("subagent_precedes_completion", func(t *testing.T) {
		cfg := &Config{
			LastUsedProvider: "zai",
			ProviderModels:   shared,
			SubagentModel:    "sub-m",
			CompletionModel:  "completion-m",
		}
		_, model := cfg.ResolveRole(RoleCoder)
		assert.Equal(t, "sub-m", model, "subagent is the more general setting")
	})

	t.Run("explicit_coder_role_beats_both", func(t *testing.T) {
		cfg := &Config{
			LastUsedProvider: "zai",
			ProviderModels:   shared,
			SubagentModel:    "sub-m",
			CompletionModel:  "completion-m",
			Roles:            map[string]RoleConfig{RoleCoder: {Model: "role-m"}},
		}
		_, model := cfg.ResolveRole(RoleCoder)
		assert.Equal(t, "role-m", model, "an explicit roles.coder entry wins")
	})

	t.Run("commit_reads_commit_settings", func(t *testing.T) {
		cfg := &Config{
			LastUsedProvider: "openrouter",
			ProviderModels:   shared,
			CommitModel:      "commit-m",
		}
		provider, model := cfg.ResolveRole(RoleCommit)
		assert.Equal(t, "openrouter", provider)
		assert.Equal(t, "commit-m", model)
	})

	t.Run("reviewer_reads_review_settings", func(t *testing.T) {
		cfg := &Config{
			LastUsedProvider: "openrouter",
			ProviderModels:   shared,
			ReviewModel:      "review-m",
		}
		_, model := cfg.ResolveRole(RoleReviewer)
		assert.Equal(t, "review-m", model)
	})

	t.Run("planner_and_summarizer_have_no_alias", func(t *testing.T) {
		// With subagent settings present but no roles entry, planner and
		// summarizer have no legacy alias and resolve to the conversation
		// default (last-used provider + its model).
		cfg := &Config{
			LastUsedProvider: "zai",
			ProviderModels:   shared,
			SubagentProvider: "openrouter",
			SubagentModel:    "sub-m",
		}
		for _, role := range []string{RolePlanner, RoleSummarizer} {
			provider, model := cfg.ResolveRole(role)
			assert.Equal(t, "zai", provider, role)
			assert.Equal(t, "GLM-4.6", model, role)
		}
	})
}

// TestRoleAlias_LegacyOnlyNoBreakage is the no-breakage guarantee: a
// legacy-only config (subagent + commit + review + completion fields set, no
// roles section) makes every legacy getter return exactly the pre-150.2
// value — the field it was always configured to return. With the roles
// section absent the alias layer contributes nothing, so a pre-role config
// behaves byte-for-byte as it did today.
func TestRoleAlias_LegacyOnlyNoBreakage(t *testing.T) {
	cfg := &Config{
		LastUsedProvider:   "openrouter",
		ProviderModels:     map[string]string{"openrouter": "openai/gpt-5"},
		SubagentProvider:   "sub-p",
		SubagentModel:      "sub-m",
		CommitProvider:     "commit-p",
		CommitModel:        "commit-m",
		ReviewProvider:     "review-p",
		ReviewModel:        "review-m",
		CompletionProvider: "completion-p",
		CompletionModel:    "completion-m",
	}
	require.Nil(t, cfg.Roles)

	assert.Equal(t, "sub-p", cfg.GetSubagentProvider())
	assert.Equal(t, "sub-m", cfg.GetSubagentModel())
	assert.Equal(t, "commit-p", cfg.GetCommitProvider())
	assert.Equal(t, "commit-m", cfg.GetCommitModel())
	assert.Equal(t, "review-p", cfg.GetReviewProvider())
	assert.Equal(t, "review-m", cfg.GetReviewModel())
	assert.Equal(t, "completion-p", cfg.GetCompletionProvider())
	assert.Equal(t, "completion-m", cfg.GetCompletionModel())
}
