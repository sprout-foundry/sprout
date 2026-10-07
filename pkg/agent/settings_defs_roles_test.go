package agent

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetSettingValue_RoleModel verifies the `sprout config set` role
// model path: the def writes the stored role entry
// and preserves the role's stored provider (read-modify-write).
func TestSetSettingValue_RoleModel(t *testing.T) {
	cfg := &configuration.Config{}
	require.NoError(t, SetSettingValue(cfg, "role.coder.model", "m"))
	assert.Equal(t, "m", cfg.Roles["coder"].Model)
	assert.Empty(t, cfg.Roles["coder"].Provider, "provider untouched")

	// Read-modify-write: a later model update keeps the stored provider.
	require.NoError(t, SetSettingValue(cfg, "role.coder.provider", "openai"))
	require.NoError(t, SetSettingValue(cfg, "role.coder.model", "gpt-4"))
	assert.Equal(t, configuration.RoleConfig{Provider: "openai", Model: "gpt-4"}, cfg.GetRole(configuration.RoleCoder))
}

// TestSetSettingValue_RoleProvider verifies the provider def analogously:
// it writes the role's provider and preserves the stored model.
func TestSetSettingValue_RoleProvider(t *testing.T) {
	cfg := &configuration.Config{}
	require.NoError(t, SetSettingValue(cfg, "role.planner.provider", "zai"))
	assert.Equal(t, "zai", cfg.Roles["planner"].Provider)

	require.NoError(t, SetSettingValue(cfg, "role.planner.model", "GLM-4.6"))
	assert.Equal(t, configuration.RoleConfig{Provider: "zai", Model: "GLM-4.6"}, cfg.GetRole(configuration.RolePlanner))
}

// TestSetSettingValue_RoleTestProviderRejected pins the CLI-facing guard:
// the literal test provider cannot be persisted through a role def.
// Config.SetRole would silently drop the write (a no-op that looks like
// success); the def surfaces it as a validation error instead.
func TestSetSettingValue_RoleTestProviderRejected(t *testing.T) {
	cfg := &configuration.Config{}
	err := SetSettingValue(cfg, "role.coder.provider", "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test provider cannot be persisted")
	assert.Equal(t, configuration.RoleConfig{}, cfg.GetRole(configuration.RoleCoder))
}

// TestSupportedSettingKeys_IncludesRoleKeys verifies all ten role keys
// (5 built-in roles x {model, provider}) are registered for
// `sprout config set` validation and --help-keys.
func TestSupportedSettingKeys_IncludesRoleKeys(t *testing.T) {
	keys := SupportedSettingKeys()
	for _, role := range configuration.BuiltInRoles() {
		assert.Contains(t, keys, "role."+role+".model")
		assert.Contains(t, keys, "role."+role+".provider")
	}

	var roleKeys []string
	for _, k := range keys {
		if strings.HasPrefix(k, "role.") {
			roleKeys = append(roleKeys, k)
		}
	}
	assert.Len(t, roleKeys, len(configuration.BuiltInRoles())*2, "exactly one model and one provider def per built-in role")
}

// TestSetSettingValue_UnknownRoleRejected verifies an unknown role key
// fails key validation and leaves the config untouched.
func TestSetSettingValue_UnknownRoleRejected(t *testing.T) {
	cfg := &configuration.Config{}
	err := SetSettingValue(cfg, "role.nope.model", "m")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown setting key "role.nope.model"`)
	assert.Empty(t, cfg.Roles)
}

// TestGetSettingValue_RoleFields verifies the registry read path returns
// the stored role fields and empty (not an error) for unset roles.
func TestGetSettingValue_RoleFields(t *testing.T) {
	cfg := &configuration.Config{}
	require.NoError(t, SetSettingValue(cfg, "role.commit.model", "gpt-4o"))
	require.NoError(t, SetSettingValue(cfg, "role.commit.provider", "openrouter"))

	v, err := GetSettingValue(cfg, "role.commit.model")
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o", v)
	v, err = GetSettingValue(cfg, "role.commit.provider")
	require.NoError(t, err)
	assert.Equal(t, "openrouter", v)

	v, err = GetSettingValue(cfg, "role.summarizer.model")
	require.NoError(t, err)
	assert.Empty(t, v)
}
