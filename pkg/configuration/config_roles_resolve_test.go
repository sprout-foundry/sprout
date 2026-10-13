package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file holds the ResolveRole precedence/fallback contract and the
// explicit-role gate tests. The parse/merge/SetRole/clone/BuiltInRoles tests
// live in config_roles_test.go (same package).

// TestResolveRole_Fallback verifies the field-wise fallback contract: unset or
// unknown roles fall back to the conversation's (provider, model).
func TestResolveRole_Fallback(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *Config
		role         string
		wantProvider string
		wantModel    string
	}{
		{
			name:         "unset role falls back to last-used provider and its model",
			cfg:          &Config{LastUsedProvider: "openrouter", ProviderModels: map[string]string{"openrouter": "openai/gpt-5"}},
			role:         RoleCommit,
			wantProvider: "openrouter",
			wantModel:    "openai/gpt-5",
		},
		{
			name:         "unknown role also falls back to the conversation",
			cfg:          &Config{LastUsedProvider: "openrouter", ProviderModels: map[string]string{"openrouter": "openai/gpt-5"}},
			role:         "my-custom-role",
			wantProvider: "openrouter",
			wantModel:    "openai/gpt-5",
		},
		{
			name: "provider-only role uses that provider's configured model",
			cfg: &Config{
				LastUsedProvider: "openrouter",
				ProviderModels:   map[string]string{"zai": "GLM-4.6", "openrouter": "openai/gpt-5"},
				Roles:            map[string]RoleConfig{RolePlanner: {Provider: "zai"}},
			},
			role:         RolePlanner,
			wantProvider: "zai",
			wantModel:    "GLM-4.6",
		},
		{
			name: "provider-only role with no configured model yields empty model",
			cfg: &Config{
				LastUsedProvider: "openrouter",
				ProviderModels:   map[string]string{"openrouter": "openai/gpt-5"},
				Roles:            map[string]RoleConfig{RolePlanner: {Provider: "zai"}},
			},
			role:         RolePlanner,
			wantProvider: "zai",
			wantModel:    "",
		},
		{
			name: "model-only role keeps the last-used provider",
			cfg: &Config{
				LastUsedProvider: "openrouter",
				ProviderModels:   map[string]string{"openrouter": "openai/gpt-5"},
				Roles:            map[string]RoleConfig{RoleCoder: {Model: "gpt-4-mini"}},
			},
			role:         RoleCoder,
			wantProvider: "openrouter",
			wantModel:    "gpt-4-mini",
		},
		{
			name: "both-set role uses both fields",
			cfg: &Config{
				LastUsedProvider: "openrouter",
				ProviderModels:   map[string]string{"openai": "gpt-4"},
				Roles:            map[string]RoleConfig{RoleReviewer: {Provider: "openai", Model: "gpt-4-turbo"}},
			},
			role:         RoleReviewer,
			wantProvider: "openai",
			wantModel:    "gpt-4-turbo",
		},
		{
			// Documented contract: when nothing is configured anywhere, the
			// resolver returns ("","") and the caller offers interactive
			// model selection, exactly as the pre-role getters do.
			name:         "nothing configured anywhere yields empty pair",
			cfg:          &Config{},
			role:         RoleCommit,
			wantProvider: "",
			wantModel:    "",
		},
		{
			name:         "nil Config yields empty pair",
			cfg:          nil,
			role:         RoleCoder,
			wantProvider: "",
			wantModel:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, model := tt.cfg.ResolveRole(tt.role)
			assert.Equal(t, tt.wantProvider, provider)
			assert.Equal(t, tt.wantModel, model)
		})
	}
}

// TestResolveRole_PreferenceOrder documents that a role's explicit model
// wins over the resolved provider's configured model even when the provider's
// map has a different entry for it.
func TestResolveRole_PreferenceOrder(t *testing.T) {
	cfg := &Config{
		LastUsedProvider: "openrouter",
		ProviderModels:   map[string]string{"zai": "GLM-4.6"},
		Roles:            map[string]RoleConfig{RoleSummarizer: {Provider: "zai", Model: "deepseek-v4"}},
	}
	provider, model := cfg.ResolveRole(RoleSummarizer)
	assert.Equal(t, "zai", provider)
	assert.Equal(t, "deepseek-v4", model, "explicit role model wins over the provider's configured model")
}

// TestCloneConfig_Roles_DeepCopy verifies cloneConfig (and the MergeConfig
// round-trip) deep-copy the roles section: mutating a clone's role leaves the
// original unchanged.
func TestCloneConfig_Roles_DeepCopy(t *testing.T) {
	original := &Config{Roles: map[string]RoleConfig{RolePlanner: {Provider: "openai", Model: "gpt-4"}}}

	clone := cloneConfig(original)
	require.NotNil(t, clone)
	assert.Equal(t, RoleConfig{Provider: "openai", Model: "gpt-4"}, clone.GetRole(RolePlanner))

	clone.Roles[RolePlanner] = RoleConfig{Provider: "zai", Model: "gpt-4"} // replace the clone's entry
	assert.Equal(t, RoleConfig{Provider: "zai", Model: "gpt-4"}, clone.GetRole(RolePlanner))
	assert.Equal(t, RoleConfig{Provider: "openai", Model: "gpt-4"}, original.GetRole(RolePlanner), "original role unchanged")

	// A nil config clones to nil.
	assert.Nil(t, cloneConfig(nil))
}

// TestSetRole_CreatesMap verifies SetRole creates the roles map on demand and
// stores the selection.
func TestSetRole_CreatesMap(t *testing.T) {
	cfg := &Config{}
	assert.Nil(t, cfg.Roles)

	cfg.SetRole(RolePlanner, RoleConfig{Provider: "openai", Model: "gpt-4"})
	require.NotNil(t, cfg.Roles)
	assert.Equal(t, RoleConfig{Provider: "openai", Model: "gpt-4"}, cfg.GetRole(RolePlanner))

	// Overwriting an existing role replaces the entry.
	cfg.SetRole(RolePlanner, RoleConfig{Model: "gpt-5"})
	assert.Equal(t, RoleConfig{Model: "gpt-5"}, cfg.GetRole(RolePlanner))
}

// TestSetRole_TestProviderRejected verifies the defense-in-depth guard: a
// literal "test" provider is not persisted through the roles section.
func TestSetRole_TestProviderRejected(t *testing.T) {
	cfg := &Config{}
	cfg.SetRole(RoleCoder, RoleConfig{Provider: "test", Model: "gpt-4"})
	// The guard returns before the map is created.
	assert.Nil(t, cfg.Roles)
	assert.Equal(t, RoleConfig{}, cfg.GetRole(RoleCoder))

	// On a config that already has roles, the guard rejects only the test
	// provider and leaves the others untouched.
	cfg2 := &Config{Roles: map[string]RoleConfig{RolePlanner: {Provider: "openai"}}}
	cfg2.SetRole(RoleCoder, RoleConfig{Provider: "test", Model: "gpt-4"})
	assert.Equal(t, RoleConfig{Provider: "openai"}, cfg2.GetRole(RolePlanner))
	assert.Equal(t, RoleConfig{}, cfg2.GetRole(RoleCoder))
	assert.Len(t, cfg2.Roles, 1)

	// A non-test provider is accepted.
	cfg2.SetRole(RoleCommit, RoleConfig{Provider: "openrouter"})
	assert.Equal(t, RoleConfig{Provider: "openrouter"}, cfg2.GetRole(RoleCommit))
	assert.Len(t, cfg2.Roles, 2)
}

// TestSetRole_NilReceiver verifies SetRole is nil-receiver safe.
func TestSetRole_NilReceiver(t *testing.T) {
	var cfg *Config
	assert.NotPanics(t, func() { cfg.SetRole(RolePlanner, RoleConfig{Provider: "openai"}) })
}

// TestResolveRole_InternalRoleChains pins the internal-role precedence:
// ResolveRole is the single internal resolver. For each internal role the
// full precedence chain must hold: an explicit roles-section entry beats
// the legacy alias settings, which beat the conversation's (last-used
// provider + its configured model).
func TestResolveRole_InternalRoleChains(t *testing.T) {
	newCfg := func() *Config {
		return &Config{
			LastUsedProvider: "openrouter",
			ProviderModels:   map[string]string{"openrouter": "openai/gpt-5", "zai": "GLM-4.6"},
		}
	}

	tests := []struct {
		name         string
		role         string
		mutate       func(c *Config)
		wantProvider string
		wantModel    string
	}{
		{
			name: "coder: roles entry wins over the subagent alias",
			role: RoleCoder,
			mutate: func(c *Config) {
				c.SubagentProvider = "zai"
				c.SubagentModel = "sub-m"
				c.Roles = map[string]RoleConfig{RoleCoder: {Provider: "openrouter", Model: "role-m"}}
			},
			wantProvider: "openrouter",
			wantModel:    "role-m",
		},
		{
			name: "coder: subagent settings alias when the role is unset",
			role: RoleCoder,
			mutate: func(c *Config) {
				c.SubagentProvider = "zai"
				c.SubagentModel = "sub-m"
			},
			wantProvider: "zai",
			wantModel:    "sub-m",
		},
		{
			// The completion settings are the completion path's own alias
			// (read by GetCompletionProvider/Model), never a source for the
			// general coder resolver — with only them set, the coder role
			// falls back to the conversation.
			name: "coder: completion settings do not alias the coder role",
			role: RoleCoder,
			mutate: func(c *Config) {
				c.CompletionProvider = "zai"
				c.CompletionModel = "completion-m"
			},
			wantProvider: "openrouter",
			wantModel:    "openai/gpt-5",
		},
		{
			name:         "coder: conversation fallback when nothing is set",
			role:         RoleCoder,
			wantProvider: "openrouter",
			wantModel:    "openai/gpt-5",
		},
		{
			name: "commit: roles entry wins over the commit alias",
			role: RoleCommit,
			mutate: func(c *Config) {
				c.CommitProvider = "zai"
				c.CommitModel = "commit-m"
				c.Roles = map[string]RoleConfig{RoleCommit: {Provider: "openrouter", Model: "role-m"}}
			},
			wantProvider: "openrouter",
			wantModel:    "role-m",
		},
		{
			name: "commit: commit settings alias when the role is unset",
			role: RoleCommit,
			mutate: func(c *Config) {
				c.CommitProvider = "zai"
				c.CommitModel = "commit-m"
			},
			wantProvider: "zai",
			wantModel:    "commit-m",
		},
		{
			name:         "commit: conversation fallback when nothing is set",
			role:         RoleCommit,
			wantProvider: "openrouter",
			wantModel:    "openai/gpt-5",
		},
		{
			name: "reviewer: roles entry wins over the review alias",
			role: RoleReviewer,
			mutate: func(c *Config) {
				c.ReviewProvider = "zai"
				c.ReviewModel = "review-m"
				c.Roles = map[string]RoleConfig{RoleReviewer: {Provider: "openrouter", Model: "role-m"}}
			},
			wantProvider: "openrouter",
			wantModel:    "role-m",
		},
		{
			name: "reviewer: review settings alias when the role is unset",
			role: RoleReviewer,
			mutate: func(c *Config) {
				c.ReviewProvider = "zai"
				c.ReviewModel = "review-m"
			},
			wantProvider: "zai",
			wantModel:    "review-m",
		},
		{
			name:         "reviewer: conversation fallback when nothing is set",
			role:         RoleReviewer,
			wantProvider: "openrouter",
			wantModel:    "openai/gpt-5",
		},
		{
			name: "planner: roles entry only (no legacy alias)",
			role: RolePlanner,
			mutate: func(c *Config) {
				c.SubagentProvider = "zai" // present but must not alias planner
				c.Roles = map[string]RoleConfig{RolePlanner: {Provider: "openrouter", Model: "planner-m"}}
			},
			wantProvider: "openrouter",
			wantModel:    "planner-m",
		},
		{
			name: "planner: conversation fallback (no legacy alias)",
			role: RolePlanner,
			mutate: func(c *Config) {
				c.SubagentProvider = "zai" // present but must not alias planner
			},
			wantProvider: "openrouter",
			wantModel:    "openai/gpt-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newCfg()
			if tt.mutate != nil {
				tt.mutate(cfg)
			}
			provider, model := cfg.ResolveRole(tt.role)
			assert.Equal(t, tt.wantProvider, provider, tt.name)
			assert.Equal(t, tt.wantModel, model, tt.name)
		})
	}
}

// TestHasExplicitRole_Reviewer pins the explicit-reviewer gate: it fires only on
// an explicit user selection — a roles.reviewer entry or the legacy review
// settings — never on the resolver's last-used-provider fallback.
func TestHasExplicitRole_Reviewer(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *Config)
		want   bool
	}{
		{
			name: "no reviewer selection: false even with a last-used provider",
		},
		{
			name: "roles.reviewer provider set: true",
			mutate: func(c *Config) {
				c.Roles = map[string]RoleConfig{RoleReviewer: {Provider: "zai"}}
			},
			want: true,
		},
		{
			name: "roles.reviewer model only: true",
			mutate: func(c *Config) {
				c.Roles = map[string]RoleConfig{RoleReviewer: {Model: "review-m"}}
			},
			want: true,
		},
		{
			name: "legacy review provider set: true",
			mutate: func(c *Config) {
				c.ReviewProvider = "zai"
			},
			want: true,
		},
		{
			name: "legacy review model set: true",
			mutate: func(c *Config) {
				c.ReviewModel = "review-m"
			},
			want: true,
		},
		{
			name: "other roles set: false for reviewer",
			mutate: func(c *Config) {
				c.Roles = map[string]RoleConfig{RoleCoder: {Provider: "zai"}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				LastUsedProvider: "openrouter",
				ProviderModels:   map[string]string{"openrouter": "openai/gpt-5", "zai": "GLM-4.6"},
			}
			if tt.mutate != nil {
				tt.mutate(cfg)
			}
			assert.Equal(t, tt.want, cfg.HasExplicitRole(RoleReviewer))
		})
	}

	// A nil receiver reports false.
	var nilCfg *Config
	assert.False(t, nilCfg.HasExplicitRole(RoleReviewer))
}

// TestBuiltInRoles verifies the stable ordering of the built-in role names.
func TestBuiltInRoles(t *testing.T) {
	assert.Equal(t,
		[]string{RolePlanner, RoleCoder, RoleSummarizer, RoleReviewer, RoleCommit, RoleWorker},
		BuiltInRoles(),
	)
}

// TestResolveRole_SummarizerResolvable pins the summarizer role: the
// summarizer role (progress and change summaries) must be
// resolvable through the single resolver — its explicit roles-section entry
// when set, and the conversation's (provider, model) when unset. It has no
// legacy alias, like the planner.
func TestResolveRole_SummarizerResolvable(t *testing.T) {
	tests := []struct {
		name         string
		mutate       func(c *Config)
		wantProvider string
		wantModel    string
	}{
		{
			name: "explicit roles entry is returned when set",
			mutate: func(c *Config) {
				c.Roles = map[string]RoleConfig{RoleSummarizer: {Provider: "zai", Model: "deepseek-v4"}}
			},
			wantProvider: "zai",
			wantModel:    "deepseek-v4",
		},
		{
			name: "model-only entry keeps the last-used provider",
			mutate: func(c *Config) {
				c.Roles = map[string]RoleConfig{RoleSummarizer: {Model: "deepseek-v4"}}
			},
			wantProvider: "openrouter",
			wantModel:    "deepseek-v4",
		},
		{
			name: "provider-only entry uses that provider's configured model",
			mutate: func(c *Config) {
				c.Roles = map[string]RoleConfig{RoleSummarizer: {Provider: "zai"}}
			},
			wantProvider: "zai",
			wantModel:    "GLM-4.6",
		},
		{
			name:         "unset falls back to the conversation",
			wantProvider: "openrouter",
			wantModel:    "openai/gpt-5",
		},
		{
			// Summarizer has no legacy alias: the coder/commit/reviewer
			// alias settings must not leak into it.
			name: "legacy alias settings do not alias the summarizer",
			mutate: func(c *Config) {
				c.SubagentProvider = "zai"
				c.SubagentModel = "sub-m"
				c.CommitProvider = "zai"
				c.CommitModel = "commit-m"
				c.ReviewProvider = "zai"
				c.ReviewModel = "review-m"
			},
			wantProvider: "openrouter",
			wantModel:    "openai/gpt-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				LastUsedProvider: "openrouter",
				ProviderModels:   map[string]string{"openrouter": "openai/gpt-5", "zai": "GLM-4.6"},
			}
			if tt.mutate != nil {
				tt.mutate(cfg)
			}
			provider, model := cfg.ResolveRole(RoleSummarizer)
			assert.Equal(t, tt.wantProvider, provider)
			assert.Equal(t, tt.wantModel, model)
		})
	}
}

// TestResolveWorkerRole pins the worker role's resolution contract: an
// explicit roles.worker entry wins field-wise, and an unset worker falls
// back to the coder role's fully-resolved (provider, model) — so an unset
// worker behaves exactly like the coder model until a host or user points
// it at a cheaper model. The worker role has no legacy alias.
func TestResolveWorkerRole(t *testing.T) {
	newCfg := func() *Config {
		return &Config{
			LastUsedProvider: "openrouter",
			ProviderModels:   map[string]string{"openrouter": "openai/gpt-5", "zai": "GLM-4.6"},
		}
	}

	tests := []struct {
		name         string
		mutate       func(c *Config)
		wantProvider string
		wantModel    string
	}{
		{
			name: "explicit worker entry wins (provider + model)",
			mutate: func(c *Config) {
				c.SubagentProvider = "zai" // coder alias must not leak into worker
				c.Roles = map[string]RoleConfig{RoleWorker: {Provider: "zai", Model: "cheap-model"}}
			},
			wantProvider: "zai",
			wantModel:    "cheap-model",
		},
		{
			name: "model-only worker entry keeps the last-used provider",
			mutate: func(c *Config) {
				c.Roles = map[string]RoleConfig{RoleWorker: {Model: "cheap-model"}}
			},
			wantProvider: "openrouter",
			wantModel:    "cheap-model",
		},
		{
			name: "provider-only worker entry uses that provider's configured model",
			mutate: func(c *Config) {
				c.Roles = map[string]RoleConfig{RoleWorker: {Provider: "zai"}}
			},
			wantProvider: "zai",
			wantModel:    "GLM-4.6",
		},
		{
			// An unset worker falls back to the coder role's full chain:
			// the coder role's legacy subagent alias when set.
			name: "unset worker falls back to the coder role (subagent alias)",
			mutate: func(c *Config) {
				c.SubagentProvider = "zai"
				c.SubagentModel = "sub-m"
			},
			wantProvider: "zai",
			wantModel:    "sub-m",
		},
		{
			// An unset worker falls back to the coder role's roles entry
			// when the subagent alias is unset.
			name: "unset worker falls back to the coder role (roles entry)",
			mutate: func(c *Config) {
				c.Roles = map[string]RoleConfig{RoleCoder: {Provider: "zai", Model: "coder-m"}}
			},
			wantProvider: "zai",
			wantModel:    "coder-m",
		},
		{
			// With nothing set anywhere, the unset worker resolves to the
			// conversation's (last-used provider, its configured model) —
			// the same as ResolveRole(coder).
			name:         "unset worker with nothing set resolves the conversation",
			wantProvider: "openrouter",
			wantModel:    "openai/gpt-5",
		},
		{
			// With an empty Config (no last-used provider, no provider
			// models), the unset worker resolves to ("",""), matching the
			// coder role's empty resolution.
			name: "completely empty config resolves to an empty pair",
			mutate: func(c *Config) {
				*c = Config{}
			},
		},
		{
			// The worker role has no legacy alias: the coder's subagent
			// settings must not be read directly by the worker resolver when
			// the worker entry is explicit (the explicit entry wins
			// field-wise, so an explicit provider must not be overridden by
			// the coder's provider).
			name: "explicit worker provider beats the coder's subagent provider",
			mutate: func(c *Config) {
				c.SubagentProvider = "zai"
				c.Roles = map[string]RoleConfig{RoleWorker: {Provider: "openrouter"}}
			},
			wantProvider: "openrouter",
			wantModel:    "openai/gpt-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newCfg()
			if tt.mutate != nil {
				tt.mutate(cfg)
			}
			provider, model := cfg.ResolveWorkerRole()
			assert.Equal(t, tt.wantProvider, provider)
			assert.Equal(t, tt.wantModel, model)
		})
	}
}

// TestResolveWorkerRole_UnsetEqualsCoderRole pins the core fallback contract
// at the pair level: when the worker role is unset, ResolveWorkerRole must
// return exactly the coder role's fully-resolved (provider, model) pair, so
// an unset worker leaves behaviour unchanged.
func TestResolveWorkerRole_UnsetEqualsCoderRole(t *testing.T) {
	cfg := &Config{
		LastUsedProvider: "openrouter",
		ProviderModels:   map[string]string{"openrouter": "openai/gpt-5", "zai": "GLM-4.6"},
	}

	// Case 1: nothing set — both resolve to the conversation.
	coderProvider, coderModel := cfg.ResolveRole(RoleCoder)
	workerProvider, workerModel := cfg.ResolveWorkerRole()
	assert.Equal(t, coderProvider, workerProvider, "unset worker provider must equal the coder role's")
	assert.Equal(t, coderModel, workerModel, "unset worker model must equal the coder role's")

	// Case 2: the coder role is configured (roles entry) — the unset worker
	// must inherit the coder role's configured pair, not the conversation's.
	cfg.Roles = map[string]RoleConfig{RoleCoder: {Provider: "zai", Model: "coder-m"}}
	coderProvider, coderModel = cfg.ResolveRole(RoleCoder)
	workerProvider, workerModel = cfg.ResolveWorkerRole()
	assert.Equal(t, "zai", coderProvider)
	assert.Equal(t, "coder-m", coderModel)
	assert.Equal(t, coderProvider, workerProvider, "unset worker must inherit the coder role's provider")
	assert.Equal(t, coderModel, workerModel, "unset worker must inherit the coder role's model")

	// Case 3: only the coder's legacy subagent alias is set — the unset
	// worker inherits it through the coder chain.
	cfg2 := &Config{
		LastUsedProvider: "openrouter",
		ProviderModels:   map[string]string{"openrouter": "openai/gpt-5", "zai": "GLM-4.6"},
		SubagentProvider: "zai",
		SubagentModel:    "sub-m",
	}
	coderProvider, coderModel = cfg2.ResolveRole(RoleCoder)
	workerProvider, workerModel = cfg2.ResolveWorkerRole()
	assert.Equal(t, "zai", coderProvider)
	assert.Equal(t, "sub-m", coderModel)
	assert.Equal(t, coderProvider, workerProvider)
	assert.Equal(t, coderModel, workerModel)
}

// TestResolveWorkerRole_NilReceiver pins the nil-receiver contract: a nil
// Config resolves to ("",""), the same as ResolveRole.
func TestResolveWorkerRole_NilReceiver(t *testing.T) {
	var cfg *Config
	provider, model := cfg.ResolveWorkerRole()
	assert.Equal(t, "", provider)
	assert.Equal(t, "", model)
}

// TestHasExplicitRole_Worker pins the explicit-worker gate: it fires only on
// an explicit roles.worker selection (the worker role has no legacy alias),
// never on the resolver's coder fallback.
func TestHasExplicitRole_Worker(t *testing.T) {
	cfg := &Config{
		LastUsedProvider: "openrouter",
		ProviderModels:   map[string]string{"openrouter": "openai/gpt-5", "zai": "GLM-4.6"},
	}
	// Unset worker: false even though the resolver would fall back to the
	// coder role (which has a last-used provider).
	assert.False(t, cfg.HasExplicitRole(RoleWorker))

	// The coder's legacy subagent alias must not make the worker explicit
	// (the worker role has no legacy alias).
	cfg.SubagentProvider = "zai"
	cfg.SubagentModel = "sub-m"
	assert.False(t, cfg.HasExplicitRole(RoleWorker), "coder alias must not mark the worker explicit")

	// An explicit roles.worker entry (provider or model) makes it explicit.
	cfg.Roles = map[string]RoleConfig{RoleWorker: {Model: "cheap-model"}}
	assert.True(t, cfg.HasExplicitRole(RoleWorker))
	cfg.Roles[RoleWorker] = RoleConfig{Provider: "zai"}
	assert.True(t, cfg.HasExplicitRole(RoleWorker))

	// A nil receiver reports false.
	var nilCfg *Config
	assert.False(t, nilCfg.HasExplicitRole(RoleWorker))
}

// TestBuiltInRoles_ContainsWorker pins that the worker role is part of the
// built-in role set (the settings defs and config-set keys derive from it).
func TestBuiltInRoles_ContainsWorker(t *testing.T) {
	roles := BuiltInRoles()
	assert.Contains(t, roles, RoleWorker, "the worker role must be a built-in role")
	assert.Len(t, roles, 6, "the built-in role set has six roles")
}
