package configuration

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A narrower layer must be able to turn a flag OFF, not just ON. The merge used
// truthiness as a stand-in for presence (`if override.X.Enabled`), so `false`
// was indistinguishable from "unset" and a workspace could never override a
// global "on". Embedding indexing is the flag that made this visible: once
// enabled globally, no workspace could opt out.
func TestWorkspaceCanDisableGloballyEnabledEmbedding(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalDir := filepath.Join(home, ".config", "sprout")
	writeFile(t, filepath.Join(globalDir, ConfigFileName),
		`{"version":"2.0","embedding_index":{"enabled":true,"auto_index":true,"max_results":3}}`)

	workspaceDir := filepath.Join(home, "proj", ".sprout")
	writeFile(t, filepath.Join(workspaceDir, WorkspaceConfigFileName),
		`{"embedding_index":{"enabled":false,"auto_index":false}}`)

	cfg, err := LoadConfigWithLayers(
		filepath.Join(globalDir, ConfigFileName),
		filepath.Join(workspaceDir, WorkspaceConfigFileName),
		"", globalDir)
	require.NoError(t, err)

	assert.False(t, cfg.EmbeddingIndex.IsEnabled(),
		"workspace enabled:false must override global enabled:true")
	assert.False(t, cfg.EmbeddingIndex.IsAutoIndex(),
		"workspace auto_index:false must override global auto_index:true")
	assert.Equal(t, 3, cfg.EmbeddingIndex.MaxResults,
		"a field the workspace never mentioned must still inherit from global")
}

// The inverse still has to work, and a workspace that says nothing about a flag
// must not silently clear it.
func TestWorkspaceSilenceInheritsEmbeddingState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalDir := filepath.Join(home, ".config", "sprout")
	writeFile(t, filepath.Join(globalDir, ConfigFileName),
		`{"version":"2.0","embedding_index":{"enabled":true,"auto_index":true}}`)

	workspaceDir := filepath.Join(home, "proj", ".sprout")
	writeFile(t, filepath.Join(workspaceDir, WorkspaceConfigFileName),
		`{"embedding_index":{"max_results":9}}`)

	cfg, err := LoadConfigWithLayers(
		filepath.Join(globalDir, ConfigFileName),
		filepath.Join(workspaceDir, WorkspaceConfigFileName),
		"", globalDir)
	require.NoError(t, err)

	assert.True(t, cfg.EmbeddingIndex.IsEnabled(),
		"a workspace that only set max_results must not turn embedding off")
	assert.Equal(t, 9, cfg.EmbeddingIndex.MaxResults)
}

// Unset stays off. Indexing is opt-in — an absent config must never be read as
// "on", which is the failure mode that had the daemon indexing whatever
// directory it started in.
func TestUnsetEmbeddingIsOff(t *testing.T) {
	assert.False(t, (*EmbeddingIndexConfig)(nil).IsEnabled())
	assert.False(t, (&EmbeddingIndexConfig{}).IsEnabled())
	assert.False(t, (&EmbeddingIndexConfig{}).IsAutoIndex())
}

// The other one-way booleans are fixed by presence tracking rather than
// pointers, so they need the same coverage at the layer-file level.
func TestLayerCanDisableFlatBooleans(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalDir := filepath.Join(home, ".config", "sprout")
	writeFile(t, filepath.Join(globalDir, ConfigFileName),
		`{"version":"2.0","skip_prompt":true,"disable_thinking":true,"pdf_ocr_enabled":true,"mcp":{"enabled":true}}`)

	workspaceDir := filepath.Join(home, "proj", ".sprout")
	writeFile(t, filepath.Join(workspaceDir, WorkspaceConfigFileName),
		`{"skip_prompt":false,"disable_thinking":false,"pdf_ocr_enabled":false,"mcp":{"enabled":false}}`)

	cfg, err := LoadConfigWithLayers(
		filepath.Join(globalDir, ConfigFileName),
		filepath.Join(workspaceDir, WorkspaceConfigFileName),
		"", globalDir)
	require.NoError(t, err)

	assert.False(t, cfg.SkipPrompt, "workspace skip_prompt:false must win")
	assert.False(t, cfg.DisableThinking, "workspace disable_thinking:false must win")
	assert.False(t, cfg.PDFOCREnabled, "workspace pdf_ocr_enabled:false must win")
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
		`{"skip_prompt":false,"embedding_index":{"enabled":false},"mcp":{"servers":{"a":{"command":"x"}}}}`), cfg))

	assert.True(t, cfg.IsExplicitlySet("skip_prompt"))
	assert.True(t, cfg.IsExplicitlySet("embedding_index.enabled"))
	assert.True(t, cfg.IsExplicitlySet("mcp.servers"))
	assert.False(t, cfg.IsExplicitlySet("disable_thinking"))
	assert.False(t, cfg.IsExplicitlySet("mcp.servers.a"),
		"recursion is bounded at section.field so open-ended maps aren't enumerated")
	assert.True(t, cfg.SectionExplicitlySet("embedding_index"))
	assert.False(t, cfg.SectionExplicitlySet("computer_use"))
}
