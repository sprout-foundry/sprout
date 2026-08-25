package configuration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeConfig_NilBase(t *testing.T) {
	override := &Config{
		Version:          "2.0",
		LastUsedProvider: "openai",
		ReasoningEffort:  "high",
	}

	result := MergeConfig(nil, override)

	require.NotNil(t, result)
	assert.Equal(t, override.Version, result.Version)
	assert.Equal(t, override.LastUsedProvider, result.LastUsedProvider)
	assert.Equal(t, override.ReasoningEffort, result.ReasoningEffort)

	// Verify it's a clone, not the same pointer
	assert.NotSame(t, override, result)
}

func TestMergeConfig_NilOverride(t *testing.T) {
	base := &Config{
		Version:          "2.0",
		LastUsedProvider: "anthropic",
		ReasoningEffort:  "medium",
		ProviderPriority: []string{"anthropic", "openai"},
	}

	result := MergeConfig(base, nil)

	require.NotNil(t, result)
	assert.Equal(t, base.Version, result.Version)
	assert.Equal(t, base.LastUsedProvider, result.LastUsedProvider)
	assert.Equal(t, base.ReasoningEffort, result.ReasoningEffort)
	assert.Equal(t, base.ProviderPriority, result.ProviderPriority)

	// Verify it's a clone, not the same pointer
	assert.NotSame(t, base, result)
}

func TestMergeConfig_BothNil(t *testing.T) {
	result := MergeConfig(nil, nil)
	assert.Nil(t, result)
}

func TestMergeConfig_StringOverrides(t *testing.T) {
	base := &Config{
		Version:           "2.0",
		LastUsedProvider:  "anthropic",
		ReasoningEffort:   "medium",
		ResourceDirectory: "old-resources",
		SystemPromptText:  "old prompt",
	}

	override := &Config{
		LastUsedProvider:  "openai",
		ReasoningEffort:   "high",
		ResourceDirectory: "new-resources",
		SystemPromptText:  "new prompt",
	}

	result := MergeConfig(base, override)

	require.NotNil(t, result)
	assert.Equal(t, base.Version, result.Version) // Should keep base version
	assert.Equal(t, override.LastUsedProvider, result.LastUsedProvider)
	assert.Equal(t, override.ReasoningEffort, result.ReasoningEffort)
	assert.Equal(t, override.ResourceDirectory, result.ResourceDirectory)
	assert.Equal(t, override.SystemPromptText, result.SystemPromptText)
}

func TestMergeConfig_MapMerge(t *testing.T) {
	base := &Config{
		ProviderModels: map[string]string{
			"anthropic": "claude-3-sonnet",
			"openai":    "gpt-3.5-turbo",
		},
		Preferences: map[string]interface{}{
			"theme": "dark",
			"font":  "monospace",
		},
	}

	override := &Config{
		ProviderModels: map[string]string{
			"openai":    "gpt-4",        // Should replace
			"deepinfra": "mixtral-8x7b", // Should add
		},
		Preferences: map[string]interface{}{
			"theme": "light", // Should replace
			"size":  "large", // Should add
		},
	}

	result := MergeConfig(base, override)

	require.NotNil(t, result)
	assert.Equal(t, "claude-3-sonnet", result.ProviderModels["anthropic"])
	assert.Equal(t, "gpt-4", result.ProviderModels["openai"])
	assert.Equal(t, "mixtral-8x7b", result.ProviderModels["deepinfra"])
	assert.Equal(t, "light", result.Preferences["theme"])
	assert.Equal(t, "monospace", result.Preferences["font"])
	assert.Equal(t, "large", result.Preferences["size"])
}

func TestMergeConfig_SliceOverride(t *testing.T) {
	base := &Config{
		ProviderPriority: []string{"anthropic", "openai"},
	}

	override := &Config{
		ProviderPriority: []string{"openai", "deepinfra"},
	}

	result := MergeConfig(base, override)

	require.NotNil(t, result)
	assert.Equal(t, override.ProviderPriority, result.ProviderPriority)
}

func TestMergeConfig_BoolOverrides(t *testing.T) {
	// Note: The current implementation only overrides booleans when they are true
	// This test reflects the actual behavior, not necessarily desired behavior
	base := &Config{
		DisableThinking: false,
		SkipPrompt:      false,
		PDFOCREnabled:   false,
	}

	override := &Config{
		DisableThinking: true,
		SkipPrompt:      true,
		PDFOCREnabled:   true,
	}

	result := MergeConfig(base, override)

	require.NotNil(t, result)
	assert.Equal(t, true, result.DisableThinking)
	assert.Equal(t, true, result.SkipPrompt)
	assert.Equal(t, true, result.PDFOCREnabled)
}

func TestMergeConfig_IntOverrides(t *testing.T) {
	base := &Config{
		SubagentMaxParallel: 2,
	}

	override := &Config{
		SubagentMaxParallel: 5,
	}

	result := MergeConfig(base, override)

	require.NotNil(t, result)
	assert.Equal(t, override.SubagentMaxParallel, result.SubagentMaxParallel)
}

func TestMergeConfig_NestedAPITimeouts(t *testing.T) {
	base := &Config{
		APITimeouts: &APITimeoutConfig{
			ConnectionTimeoutSec: 300,
			FirstChunkTimeoutSec: 600,
			ChunkTimeoutSec:      600,
			OverallTimeoutSec:    1800,
		},
	}

	override := &Config{
		APITimeouts: &APITimeoutConfig{
			FirstChunkTimeoutSec:    900, // Should update
			CommitMessageTimeoutSec: 600, // Should add
		},
	}

	result := MergeConfig(base, override)

	require.NotNil(t, result)
	require.NotNil(t, result.APITimeouts)
	assert.Equal(t, base.APITimeouts.ConnectionTimeoutSec, result.APITimeouts.ConnectionTimeoutSec)
	assert.Equal(t, override.APITimeouts.FirstChunkTimeoutSec, result.APITimeouts.FirstChunkTimeoutSec)
	assert.Equal(t, base.APITimeouts.ChunkTimeoutSec, result.APITimeouts.ChunkTimeoutSec)
	assert.Equal(t, base.APITimeouts.OverallTimeoutSec, result.APITimeouts.OverallTimeoutSec)
	assert.Equal(t, override.APITimeouts.CommitMessageTimeoutSec, result.APITimeouts.CommitMessageTimeoutSec)
}

func TestMergeConfig_SubagentTypes(t *testing.T) {
	base := &Config{
		SubagentTypes: map[string]SubagentType{
			"coder": {
				ID:           "coder",
				Name:         "Coder",
				Enabled:      true,
				SystemPrompt: "coder.md",
			},
			"tester": {
				ID:           "tester",
				Name:         "Tester",
				Enabled:      true,
				SystemPrompt: "tester.md",
			},
		},
	}

	override := &Config{
		SubagentTypes: map[string]SubagentType{
			"tester": { // Should replace
				ID:           "tester",
				Name:         "Enhanced Tester",
				Enabled:      false,
				SystemPrompt: "enhanced_tester.md",
			},
			"debugger": { // Should add
				ID:           "debugger",
				Name:         "Debugger",
				Enabled:      true,
				SystemPrompt: "debugger.md",
			},
		},
	}

	result := MergeConfig(base, override)

	require.NotNil(t, result)
	require.NotNil(t, result.SubagentTypes)

	// Verify existing coder unchanged
	assert.Equal(t, "coder", result.SubagentTypes["coder"].ID)
	assert.Equal(t, "Coder", result.SubagentTypes["coder"].Name)
	assert.True(t, result.SubagentTypes["coder"].Enabled)

	// Verify tester was overridden
	assert.Equal(t, "tester", result.SubagentTypes["tester"].ID)
	assert.Equal(t, "Enhanced Tester", result.SubagentTypes["tester"].Name)
	assert.False(t, result.SubagentTypes["tester"].Enabled)

	// Verify debugger was added
	assert.Equal(t, "debugger", result.SubagentTypes["debugger"].ID)
	assert.Equal(t, "Debugger", result.SubagentTypes["debugger"].Name)
	assert.True(t, result.SubagentTypes["debugger"].Enabled)
}

func TestMergeConfig_EmptyOverrideNoChange(t *testing.T) {
	base := &Config{
		Version:          "2.0",
		LastUsedProvider: "anthropic",
		ReasoningEffort:  "medium",
		ProviderModels:   map[string]string{"anthropic": "claude-3-sonnet"},
	}

	override := &Config{} // Empty override

	result := MergeConfig(base, override)

	require.NotNil(t, result)
	assert.Equal(t, base.Version, result.Version)
	assert.Equal(t, base.LastUsedProvider, result.LastUsedProvider)
	assert.Equal(t, base.ReasoningEffort, result.ReasoningEffort)
	assert.Equal(t, base.ProviderModels, result.ProviderModels)
}

func TestLoadConfigWithLayers_GlobalOnly(t *testing.T) {
	tempDir := t.TempDir()
	globalPath := filepath.Join(tempDir, "global_config.json")

	globalCfg := `{
		"version": "2.0",
		"last_used_provider": "openai",
		"provider_models": {
			"openai": "gpt-4"
		}
	}`

	err := os.WriteFile(globalPath, []byte(globalCfg), 0644)
	require.NoError(t, err)

	result, err := LoadConfigWithLayers(globalPath, "", "", tempDir)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "2.0", result.Version)
	assert.Equal(t, "openai", result.LastUsedProvider)
	assert.Equal(t, "gpt-4", result.ProviderModels["openai"])
}

func TestLoadConfigWithLayers_WorkspaceOverride(t *testing.T) {
	tempDir := t.TempDir()
	globalPath := filepath.Join(tempDir, "global_config.json")
	workspacePath := filepath.Join(tempDir, "workspace_config.json")

	globalCfg := `{
		"version": "2.0",
		"last_used_provider": "openai",
		"provider_models": {
			"openai": "gpt-3.5-turbo",
			"anthropic": "claude-3-sonnet"
		}
	}`

	workspaceCfg := `{
		"version": "2.0",
		"last_used_provider": "anthropic",
		"provider_models": {
			"anthropic": "claude-3-opus",
			"deepinfra": "mixtral-8x7b"
		}
	}`

	err := os.WriteFile(globalPath, []byte(globalCfg), 0644)
	require.NoError(t, err)
	err = os.WriteFile(workspacePath, []byte(workspaceCfg), 0644)
	require.NoError(t, err)

	result, err := LoadConfigWithLayers(globalPath, workspacePath, "", tempDir)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "2.0", result.Version)
	assert.Equal(t, "anthropic", result.LastUsedProvider)                // Override takes precedence
	assert.Equal(t, "gpt-3.5-turbo", result.ProviderModels["openai"])    // From global
	assert.Equal(t, "claude-3-opus", result.ProviderModels["anthropic"]) // Overridden
	assert.Equal(t, "mixtral-8x7b", result.ProviderModels["deepinfra"])  // Added by override
}

func TestLoadConfigWithLayers_SessionOverride(t *testing.T) {
	tempDir := t.TempDir()
	globalPath := filepath.Join(tempDir, "global_config.json")
	workspacePath := filepath.Join(tempDir, "workspace_config.json")
	sessionPath := filepath.Join(tempDir, "session_config.json")

	globalCfg := `{
		"version": "2.0",
		"last_used_provider": "openai",
		"reasoning_effort": "medium"
	}`

	workspaceCfg := `{
		"version": "2.0",
		"last_used_provider": "anthropic",
		"reasoning_effort": "high"
	}`

	sessionCfg := `{
		"version": "2.0",
		"last_used_provider": "deepinfra",
		"reasoning_effort": "low"
	}`

	err := os.WriteFile(globalPath, []byte(globalCfg), 0644)
	require.NoError(t, err)
	err = os.WriteFile(workspacePath, []byte(workspaceCfg), 0644)
	require.NoError(t, err)
	err = os.WriteFile(sessionPath, []byte(sessionCfg), 0644)
	require.NoError(t, err)

	result, err := LoadConfigWithLayers(globalPath, workspacePath, sessionPath, tempDir)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "2.0", result.Version)
	assert.Equal(t, "deepinfra", result.LastUsedProvider) // Session takes highest precedence
	assert.Equal(t, "low", result.ReasoningEffort)        // Session takes highest precedence
}

func TestLoadConfigWithLayers_MissingWorkspace(t *testing.T) {
	tempDir := t.TempDir()
	globalPath := filepath.Join(tempDir, "global_config.json")
	workspacePath := filepath.Join(tempDir, "nonexistent_workspace_config.json")

	globalCfg := `{
		"version": "2.0",
		"last_used_provider": "openai"
	}`

	err := os.WriteFile(globalPath, []byte(globalCfg), 0644)
	require.NoError(t, err)

	result, err := LoadConfigWithLayers(globalPath, workspacePath, "", tempDir)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "2.0", result.Version)
	assert.Equal(t, "openai", result.LastUsedProvider)
}

func TestLoadConfigWithLayers_CorruptFile(t *testing.T) {
	tempDir := t.TempDir()
	globalPath := filepath.Join(tempDir, "global_config.json")
	workspacePath := filepath.Join(tempDir, "corrupt_config.json")

	globalCfg := `{
		"version": "2.0",
		"last_used_provider": "openai"
	}`

	corruptCfg := `{
		"version": "2.0",
		"last_used_provider": "anthropic",
		// Missing closing brace - invalid JSON
	`

	err := os.WriteFile(globalPath, []byte(globalCfg), 0644)
	require.NoError(t, err)
	err = os.WriteFile(workspacePath, []byte(corruptCfg), 0644)
	require.NoError(t, err)

	result, err := LoadConfigWithLayers(globalPath, workspacePath, "", tempDir)
	require.NoError(t, err) // Should not error, should log warning and continue
	require.NotNil(t, result)

	// Should have global config values since workspace was corrupt and skipped
	assert.Equal(t, "2.0", result.Version)
	assert.Equal(t, "openai", result.LastUsedProvider)
}

func TestNewManagerWithLayers_CreatesManager(t *testing.T) {
	tempDir := t.TempDir()
	globalDir := tempDir
	workspaceDir := filepath.Join(tempDir, "workspace")

	// Create workspace directory
	err := os.Mkdir(workspaceDir, 0755)
	require.NoError(t, err)

	// Create global config
	globalCfg := `{
		"version": "2.0",
		"last_used_provider": "openai"
	}`
	err = os.WriteFile(filepath.Join(globalDir, ConfigFileName), []byte(globalCfg), 0644)
	require.NoError(t, err)

	manager, err := NewManagerWithLayers(globalDir, workspaceDir)
	require.NoError(t, err)
	require.NotNil(t, manager)

	config := manager.GetConfig()
	require.NotNil(t, config)
	assert.Equal(t, "2.0", config.Version)
	assert.Equal(t, "openai", config.LastUsedProvider)
}

func TestNewManagerWithLayers_SavesToWorkspaceDir(t *testing.T) {
	tempDir := t.TempDir()
	globalDir := tempDir
	workspaceDir := filepath.Join(tempDir, "workspace")

	// Create workspace directory with a config file
	err := os.Mkdir(workspaceDir, 0755)
	require.NoError(t, err)

	// Create global config
	globalCfg := `{
		"version": "2.0",
		"last_used_provider": "openai"
	}`
	err = os.WriteFile(filepath.Join(globalDir, ConfigFileName), []byte(globalCfg), 0644)
	require.NoError(t, err)

	// Create workspace config that overrides global
	workspaceCfg := `{
		"version": "2.0",
		"last_used_provider": "anthropic",
		"reasoning_effort": "high"
	}`
	err = os.WriteFile(filepath.Join(workspaceDir, ConfigFileName), []byte(workspaceCfg), 0644)
	require.NoError(t, err)

	manager, err := NewManagerWithLayers(globalDir, workspaceDir)
	require.NoError(t, err)
	require.NotNil(t, manager)

	// The merged config should reflect workspace overriding global
	config := manager.GetConfig()
	assert.Equal(t, "anthropic", config.LastUsedProvider) // workspace overrides global
	assert.Equal(t, "high", config.ReasoningEffort)       // workspace-only value
}

// Additional helper test to verify deep cloning works correctly
func TestMergeConfig_DeepClone(t *testing.T) {
	base := &Config{
		Version: "2.0",
		ProviderModels: map[string]string{
			"openai": "gpt-4",
		},
		Preferences: map[string]interface{}{
			"theme": "dark",
		},
		SubagentTypes: map[string]SubagentType{
			"coder": {
				ID:      "coder",
				Name:    "Coder",
				Enabled: true,
			},
		},
	}

	result := MergeConfig(base, nil)

	// Modify the result to ensure it's a deep clone
	result.ProviderModels["openai"] = "gpt-5"
	result.Preferences["theme"] = "light"

	// For SubagentTypes, we need to replace the entire struct since map values are not addressable
	coderType := result.SubagentTypes["coder"]
	coderType.Name = "Super Coder"
	result.SubagentTypes["coder"] = coderType

	// Verify original base is unchanged
	assert.Equal(t, "gpt-4", base.ProviderModels["openai"])
	assert.Equal(t, "dark", base.Preferences["theme"].(string))
	assert.Equal(t, "Coder", base.SubagentTypes["coder"].Name)
}
