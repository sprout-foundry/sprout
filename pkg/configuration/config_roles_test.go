package configuration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoleConfig_Parse_RolesSection verifies that a Config decoded from JSON
// with a roles section exposes the stored selection through GetRole, that an
// absent or unknown role returns the zero RoleConfig, and that the section's
// JSON shape (including an empty {} entry) round-trips.
func TestRoleConfig_Parse_RolesSection(t *testing.T) {
	const jsonConfig = `{
		"last_used_provider": "openrouter",
		"provider_models": {
			"openrouter": "openai/gpt-5",
			"zai": "GLM-4.6"
		},
		"roles": {
			"planner":    { "provider": "zai", "model": "GLM-4.6" },
			"coder":      { "provider": "openai" },
			"summarizer": { "model": "deepseek-v4" },
			"reviewer": {}
		}
	}`

	var cfg Config
	require.NoError(t, json.Unmarshal([]byte(jsonConfig), &cfg))

	// All five built-ins are accepted by the map shape; the two omitted /
	// empty ones resolve to the zero RoleConfig through GetRole.
	assert.Equal(t, RoleConfig{Provider: "zai", Model: "GLM-4.6"}, cfg.GetRole(RolePlanner))
	assert.Equal(t, RoleConfig{Provider: "openai"}, cfg.GetRole(RoleCoder))
	assert.Equal(t, RoleConfig{Model: "deepseek-v4"}, cfg.GetRole(RoleSummarizer))
	assert.Equal(t, RoleConfig{}, cfg.GetRole(RoleReviewer)) // present but empty entry
	assert.Equal(t, RoleConfig{}, cfg.GetRole(RoleCommit))   // absent key
	assert.Equal(t, RoleConfig{}, cfg.GetRole("not-a-role")) // unknown name

	// commit is absent, so the map holds exactly the four named roles.
	assert.Len(t, cfg.Roles, 4)

	// An empty role entry (reviewer: {}) must survive a marshal/unmarshal
	// round-trip — the key is preserved even though its value is the zero
	// RoleConfig.
	data, err := json.Marshal(&cfg)
	require.NoError(t, err)
	var round Config
	require.NoError(t, json.Unmarshal(data, &round))
	assert.Equal(t, cfg.Roles, round.Roles)
	assert.Len(t, round.Roles, 4)
	assert.Equal(t, RoleConfig{}, round.GetRole(RoleReviewer))
}

// TestRoleConfig_RoundTrip_EmptyEntry guards the JSON shape of an empty role
// entry ({}) explicitly: it must be serialized and re-parsed as a present,
// zero RoleConfig.
func TestRoleConfig_RoundTrip_EmptyEntry(t *testing.T) {
	cfg := &Config{Roles: map[string]RoleConfig{
		RoleReviewer: {}, // explicit empty entry
	}}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"reviewer":{}`)

	var round Config
	require.NoError(t, json.Unmarshal(data, &round))
	assert.Equal(t, RoleConfig{}, round.GetRole(RoleReviewer))
}

// TestGetRole_NilReceiver verifies GetRole is nil-receiver safe.
func TestGetRole_NilReceiver(t *testing.T) {
	var cfg *Config
	assert.Equal(t, RoleConfig{}, cfg.GetRole(RolePlanner))
}

// TestMergeConfig_Roles_FieldWise verifies a role named in both layers keeps
// every field the override left empty from the base layer (field-wise
// precedence, unlike RiskProfiles' whole-entry replacement).
func TestMergeConfig_Roles_FieldWise(t *testing.T) {
	base := &Config{
		Roles: map[string]RoleConfig{
			RoleCommit: {Provider: "openrouter"},
		},
	}
	override := &Config{
		Roles: map[string]RoleConfig{
			RoleCommit: {Model: "gpt-4"}, // provider left empty
		},
	}

	result := MergeConfig(base, override)
	require.NotNil(t, result)

	// Project's model + global's provider survive.
	assert.Equal(t, RoleConfig{Provider: "openrouter", Model: "gpt-4"}, result.GetRole(RoleCommit))
}

// TestMergeConfig_Roles_BaseOnlySurvives verifies a role present only in the
// base layer passes through untouched.
func TestMergeConfig_Roles_BaseOnlySurvives(t *testing.T) {
	base := &Config{
		Roles: map[string]RoleConfig{
			RoleReviewer: {Provider: "zai", Model: "GLM-4.6"},
			RoleCommit:   {Provider: "openrouter"},
		},
	}
	override := &Config{
		Roles: map[string]RoleConfig{
			RoleCommit: {Model: "gpt-4"},
		},
	}

	result := MergeConfig(base, override)
	require.NotNil(t, result)

	assert.Equal(t, RoleConfig{Provider: "zai", Model: "GLM-4.6"}, result.GetRole(RoleReviewer), "base-only role survives")
	assert.Equal(t, RoleConfig{Provider: "openrouter", Model: "gpt-4"}, result.GetRole(RoleCommit))
}

// TestMergeConfig_Roles_OverrideOnlyAdded verifies a role present only in the
// override layer is added.
func TestMergeConfig_Roles_OverrideOnlyAdded(t *testing.T) {
	base := &Config{
		Roles: map[string]RoleConfig{
			RoleCoder: {Provider: "openai"},
		},
	}
	override := &Config{
		Roles: map[string]RoleConfig{
			RoleSummarizer: {Model: "deepseek-v4"},
		},
	}

	result := MergeConfig(base, override)
	require.NotNil(t, result)

	assert.Equal(t, RoleConfig{Provider: "openai"}, result.GetRole(RoleCoder))
	assert.Equal(t, RoleConfig{Model: "deepseek-v4"}, result.GetRole(RoleSummarizer), "override-only role added")
	assert.Len(t, result.Roles, 2)
}

// TestMergeConfig_Roles_NilRolesNoPanic verifies merging never nil-panics
// when either layer has no roles section.
func TestMergeConfig_Roles_NilRolesNoPanic(t *testing.T) {
	// base has no roles, override adds one
	base := &Config{}
	override := &Config{Roles: map[string]RoleConfig{RolePlanner: {Provider: "openai"}}}
	result := MergeConfig(base, override)
	assert.Equal(t, RoleConfig{Provider: "openai"}, result.GetRole(RolePlanner))

	// base has roles, override has none — the base roles must survive
	base2 := &Config{Roles: map[string]RoleConfig{RoleCoder: {Model: "gpt-4-mini"}}}
	result2 := MergeConfig(base2, &Config{})
	assert.Equal(t, RoleConfig{Model: "gpt-4-mini"}, result2.GetRole(RoleCoder))
	assert.Len(t, result2.Roles, 1)

	// both layers have no roles — no section is created
	bothNil := MergeConfig(&Config{}, &Config{})
	assert.Empty(t, bothNil.Roles)
}

// TestMergeConfig_Roles_NilLayerClones verifies MergeConfig(nil, cfg) and
// (cfg, nil) deep-clone the roles section.
func TestMergeConfig_Roles_NilLayerClones(t *testing.T) {
	cfg := &Config{Roles: map[string]RoleConfig{RolePlanner: {Provider: "openai", Model: "gpt-4"}}}

	fromNil := MergeConfig(nil, cfg)
	require.NotNil(t, fromNil)
	assert.Equal(t, RoleConfig{Provider: "openai", Model: "gpt-4"}, fromNil.GetRole(RolePlanner))
	fromNil.Roles[RolePlanner] = RoleConfig{Provider: "zai", Model: "gpt-4"} // replace the clone's entry
	assert.Equal(t, RoleConfig{Provider: "openai", Model: "gpt-4"}, cfg.GetRole(RolePlanner), "original unchanged")

	toNil := MergeConfig(cfg, nil)
	require.NotNil(t, toNil)
	assert.Equal(t, RoleConfig{Provider: "openai", Model: "gpt-4"}, toNil.GetRole(RolePlanner))
	toNil.Roles[RolePlanner] = RoleConfig{Provider: "zai", Model: "gpt-4"} // replace the clone's entry
	assert.Equal(t, RoleConfig{Provider: "openai", Model: "gpt-4"}, cfg.GetRole(RolePlanner), "original unchanged")
}

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

// TestResolveRole_InternalRoleChains pins item 150.3 (SP-150 §150b):
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
			name: "coder: completion settings when subagent settings are unset",
			role: RoleCoder,
			mutate: func(c *Config) {
				c.CompletionProvider = "zai"
				c.CompletionModel = "completion-m"
			},
			wantProvider: "zai",
			wantModel:    "completion-m",
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

// TestBuiltInRoles verifies the stable ordering of the built-in role names.
func TestBuiltInRoles(t *testing.T) {
	assert.Equal(t,
		[]string{RolePlanner, RoleCoder, RoleSummarizer, RoleReviewer, RoleCommit},
		BuiltInRoles(),
	)
}
