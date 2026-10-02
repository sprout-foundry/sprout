package configuration

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A narrower layer must be able to turn a flag OFF, not just ON. The merge used
// truthiness as a stand-in for presence (`if override.X.Enabled`), so `false`
// was indistinguishable from "unset". Presence tracking fixes that, so it needs
// coverage at the layer-file level.
func TestLayerCanDisableFlatBooleans(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalDir := filepath.Join(home, ".config", "sprout")
	writeFile(t, filepath.Join(globalDir, ConfigFileName),
		`{"version":"2.0","skip_prompt":true,"disable_thinking":true,"vision_fallback_to_ocr":true,"mcp":{"enabled":true}}`)

	workspaceDir := filepath.Join(home, "proj", ".sprout")
	writeFile(t, filepath.Join(workspaceDir, WorkspaceConfigFileName),
		`{"skip_prompt":false,"disable_thinking":false,"mcp":{"enabled":false}}`)

	cfg, err := LoadConfigWithLayers(
		filepath.Join(globalDir, ConfigFileName),
		filepath.Join(workspaceDir, WorkspaceConfigFileName),
		"", globalDir)
	require.NoError(t, err)

	assert.False(t, cfg.SkipPrompt, "workspace skip_prompt:false must win")
	assert.False(t, cfg.DisableThinking, "workspace disable_thinking:false must win")
	assert.False(t, cfg.MCP.Enabled, "workspace mcp.enabled:false must win")
}

// Presence tracking must not make silence destructive: a layer that never names
// a flag leaves the broader layer's value alone.
func TestSilentLayerDoesNotClearFlatBooleans(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalDir := filepath.Join(home, ".config", "sprout")
	writeFile(t, filepath.Join(globalDir, ConfigFileName),
		`{"version":"2.0","skip_prompt":true,"disable_thinking":true,"mcp":{"enabled":true}}`)

	workspaceDir := filepath.Join(home, "proj", ".sprout")
	writeFile(t, filepath.Join(workspaceDir, WorkspaceConfigFileName),
		`{"reasoning_effort":"high"}`)

	cfg, err := LoadConfigWithLayers(
		filepath.Join(globalDir, ConfigFileName),
		filepath.Join(workspaceDir, WorkspaceConfigFileName),
		"", globalDir)
	require.NoError(t, err)

	assert.True(t, cfg.SkipPrompt)
	assert.True(t, cfg.DisableThinking)
	assert.True(t, cfg.MCP.Enabled)
	assert.Equal(t, "high", cfg.ReasoningEffort)
}

func TestExplicitKeyTrackingRecordsNestedPaths(t *testing.T) {
	cfg := &Config{}
	require.NoError(t, unmarshalLayer([]byte(
		`{"skip_prompt":false,"wakeup":{"enabled":false},"mcp":{"servers":{"a":{"command":"x"}}}}`), cfg))

	assert.True(t, cfg.IsExplicitlySet("skip_prompt"))
	assert.True(t, cfg.IsExplicitlySet("wakeup.enabled"))
	assert.True(t, cfg.IsExplicitlySet("mcp.servers"))
	assert.False(t, cfg.IsExplicitlySet("disable_thinking"))
	assert.False(t, cfg.IsExplicitlySet("mcp.servers.a"),
		"recursion is bounded at section.field so open-ended maps aren't enumerated")
	assert.True(t, cfg.SectionExplicitlySet("wakeup"))
	assert.False(t, cfg.SectionExplicitlySet("computer_use"))
}
