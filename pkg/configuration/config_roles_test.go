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
