//go:build !js

package cmd

import (
	"encoding/json"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigGet_RoleSectionResolves verifies the `sprout config get
// roles.<role>` read path: getConfigField navigates the
// serialized roles section to the stored RoleConfig.
func TestConfigGet_RoleSectionResolves(t *testing.T) {
	cfg := &configuration.Config{
		LastUsedProvider: "openrouter",
		Roles: map[string]configuration.RoleConfig{
			"coder": {Provider: "openai", Model: "gpt-4"},
		},
	}

	value, found := getConfigField(cfg, "roles.coder")
	require.True(t, found, "roles.coder must resolve through the roles section")
	role, ok := value.(configuration.RoleConfig)
	require.True(t, ok)
	assert.Equal(t, configuration.RoleConfig{Provider: "openai", Model: "gpt-4"}, role)

	// A role name with no stored entry is not found.
	_, found = getConfigField(cfg, "roles.planner")
	assert.False(t, found)
}

// TestConfigGet_RoleSectionAbsent verifies the read path reports not-found
// rather than erroring when the roles section is empty or the key is
// unknown.
func TestConfigGet_RoleSectionAbsent(t *testing.T) {
	cfg := &configuration.Config{}
	_, found := getConfigField(cfg, "roles.coder")
	assert.False(t, found)
	_, found = getConfigField(cfg, "nope")
	assert.False(t, found)
}

// TestConfigShow_IncludesRoles verifies `sprout config show` serializes the
// roles section (the redacted JSON dump includes it as-is).
func TestConfigShow_IncludesRoles(t *testing.T) {
	cfg := &configuration.Config{
		Roles: map[string]configuration.RoleConfig{
			configuration.RoleCoder: {Provider: "openai", Model: "gpt-4"},
		},
	}
	data, err := json.MarshalIndent(configuration.RedactConfig(cfg), "", "  ")
	require.NoError(t, err)
	assert.Contains(t, string(data), `"roles"`)
	assert.Contains(t, string(data), `"coder"`)
	assert.Contains(t, string(data), `"gpt-4"`)
}
