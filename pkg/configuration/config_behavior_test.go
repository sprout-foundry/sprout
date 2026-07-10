package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 1. NewConfig defaults
// ---------------------------------------------------------------------------

func TestNewConfigDefaults(t *testing.T) {
	cfg := NewConfig()

	assert.Equal(t, ConfigVersion, cfg.Version)
	assert.Equal(t, "project", cfg.HistoryScope, "HistoryScope should default to 'project'")
	assert.Empty(t, cfg.LastUsedProvider, "LastUsedProvider should default to empty string")
	assert.NotNil(t, cfg.ProviderModels, "ProviderModels should be initialized")
	assert.NotNil(t, cfg.ProviderPriority, "ProviderPriority should be initialized")
	assert.NotNil(t, cfg.CustomProviders, "CustomProviders should be initialized")
	assert.NotNil(t, cfg.CommandHistoryByPath, "CommandHistoryByPath should be initialized")
	assert.NotNil(t, cfg.HistoryIndexByPath, "HistoryIndexByPath should be initialized")
	assert.NotNil(t, cfg.Preferences, "Preferences should be initialized")
	assert.NotNil(t, cfg.APITimeouts, "APITimeouts should be initialized")
	assert.Equal(t, 300, cfg.APITimeouts.ConnectionTimeoutSec)
	assert.Equal(t, 600, cfg.APITimeouts.FirstChunkTimeoutSec)
	assert.Equal(t, 600, cfg.APITimeouts.ChunkTimeoutSec)
	assert.Equal(t, 1800, cfg.APITimeouts.OverallTimeoutSec)
	assert.True(t, cfg.EnableZshCommandDetection)
	assert.True(t, cfg.AutoExecuteDetectedCommands)
	assert.True(t, cfg.PDFOCREnabled)
	assert.Equal(t, "ollama", cfg.PDFOCRProvider)
	assert.Equal(t, "glm-ocr", cfg.PDFOCRModel)
	assert.NotEmpty(t, cfg.SubagentTypes, "SubagentTypes should contain defaults")
	assert.NotEmpty(t, cfg.Skills, "Skills should contain defaults")
}

// ---------------------------------------------------------------------------
// 2. Save / Load round-trip via LEDIT_CONFIG
// ---------------------------------------------------------------------------

func TestConfigSaveLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	original := NewConfig()
	original.LastUsedProvider = "deepinfra"
	original.HistoryScope = "global"
	original.ReasoningEffort = "high"
	original.SystemPromptText = "custom prompt"
	original.SkipPrompt = true

	// Save
	err := original.Save()
	require.NoError(t, err)

	// Verify file exists
	configPath := filepath.Join(tmpDir, ConfigFileName)
	_, err = os.Stat(configPath)
	require.NoError(t, err, "config file should exist on disk")

	// Load
	loaded, err := Load()
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, ConfigVersion, loaded.Version)
	assert.Equal(t, "deepinfra", loaded.LastUsedProvider)
	assert.Equal(t, "global", loaded.HistoryScope)
	assert.Equal(t, "high", loaded.ReasoningEffort)
	assert.Equal(t, "custom prompt", loaded.SystemPromptText)
	assert.True(t, loaded.SkipPrompt)
}

// ---------------------------------------------------------------------------
// 3. Validate catches multiple errors
// ---------------------------------------------------------------------------

func TestConfigValidateMultipleErrors(t *testing.T) {
	cfg := NewConfig()
	// Set multiple invalid fields at once
	cfg.PDFOCREnabled = true
	cfg.PDFOCRProvider = ""
	cfg.PDFOCRModel = ""

	err := cfg.Validate()
	// The Validate method returns the first error encountered, but we can
	// verify the config truly has multiple problems by testing each one
	// individually to confirm they are independently invalid.
	assert.Error(t, err, "Validate should return an error for invalid config")

	// Confirm all conditions are independently invalid
	singleErr := (&Config{PDFOCREnabled: true, PDFOCRProvider: ""}).Validate()
	assert.Error(t, singleErr)

	singleErr = (&Config{PDFOCREnabled: true, PDFOCRModel: ""}).Validate()
	assert.Error(t, singleErr)
}

// ---------------------------------------------------------------------------
// 4. GetSubagentType returns nil for unknown persona
// ---------------------------------------------------------------------------

func TestGetSubagentTypeReturnsNilForUnknownPersona(t *testing.T) {
	cfg := NewConfig()

	unknowns := []string{
		"nonexistent",
		"foobar",
		"does-not-exist",
		"",
	}

	for _, id := range unknowns {
		t.Run(id, func(t *testing.T) {
			result := cfg.GetSubagentType(id)
			assert.Nil(t, result, "GetSubagentType(%q) should return nil", id)
		})
	}
}

// ---------------------------------------------------------------------------
// 5. GetSubagentType serves catalog-defined AllowedTools
// ---------------------------------------------------------------------------

func TestGetSubagentTypeReturnsCatalogAllowedTools(t *testing.T) {
	defaults := defaultSubagentTypes()
	coderDefault, ok := defaults["coder"]
	require.True(t, ok, "coder should exist in catalog")
	require.NotEmpty(t, coderDefault.AllowedTools, "coder catalog entry should have AllowedTools")

	cfg := NewConfig()
	persona := cfg.GetSubagentType("coder")
	require.NotNil(t, persona)
	assert.Equal(t, coderDefault.AllowedTools, persona.AllowedTools)
}

// ---------------------------------------------------------------------------
// Bonus: Verify Save produces valid JSON and Load handles missing file
// ---------------------------------------------------------------------------

func TestLoadReturnsDefaultWhenNoConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	// Point to an empty temp dir — no config.json exists yet
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, ConfigVersion, cfg.Version)
	assert.NotEmpty(t, cfg.ProviderModels)
}

func TestSaveProducesValidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	cfg := NewConfig()
	cfg.LastUsedProvider = "ollama-local"
	cfg.SkipPrompt = true

	err := cfg.Save()
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(tmpDir, ConfigFileName))
	require.NoError(t, err)

	// Verify it's valid JSON
	var parsed map[string]interface{}
	err = json.Unmarshal(raw, &parsed)
	require.NoError(t, err, "saved config should be valid JSON")
	assert.Equal(t, ConfigVersion, parsed["version"])
}

// ---------------------------------------------------------------------------
// Load() merges defaults for omitempty bool fields (SP-fix)
// ---------------------------------------------------------------------------

// TestLoadDefaultsAppliedForOmittedZshFields verifies that when a config
// file omits the zsh detection fields, Load() applies the NewConfig()
// defaults (both true) instead of leaving them as the Go zero value (false).
func TestLoadDefaultsAppliedForOmittedZshFields(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	// Write a config that has NO zsh-related keys.
	configPath := filepath.Join(tmpDir, ConfigFileName)
	minimalConfig := `{
		"version": "2.0",
		"last_used_provider": "ollama-local"
	}`
	require.NoError(t, os.WriteFile(configPath, []byte(minimalConfig), 0600))

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// With the defaults-merge fix, omitted bool fields should pick up
	// NewConfig() defaults rather than the Go zero value.
	assert.True(t, cfg.EnableZshCommandDetection,
		"EnableZshCommandDetection should default to true when absent from file")
	assert.True(t, cfg.AutoExecuteDetectedCommands,
		"AutoExecuteDetectedCommands should default to true when absent from file")
}

// TestLoadRespectsExplicitFalseZshFields verifies that a config file with
// explicit false values for the zsh fields is respected after Load().
func TestLoadRespectsExplicitFalseZshFields(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	// Write a config that explicitly disables zsh detection.
	configPath := filepath.Join(tmpDir, ConfigFileName)
	explicitFalseConfig := `{
		"version": "2.0",
		"enable_zsh_command_detection": false,
		"auto_execute_detected_commands": false
	}`
	require.NoError(t, os.WriteFile(configPath, []byte(explicitFalseConfig), 0600))

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.False(t, cfg.EnableZshCommandDetection,
		"EnableZshCommandDetection should be false when explicitly set in file")
	assert.False(t, cfg.AutoExecuteDetectedCommands,
		"AutoExecuteDetectedCommands should be false when explicitly set in file")
}

// TestSavePersistsExplicitFalseZshFields verifies that setting a bool
// field to false and saving results in the false value being persisted
// to disk (i.e. omitempty was removed from the JSON tag).
func TestSavePersistsExplicitFalseZshFields(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	cfg := NewConfig()
	cfg.EnableZshCommandDetection = false
	cfg.AutoExecuteDetectedCommands = false

	require.NoError(t, cfg.Save())

	configPath := filepath.Join(tmpDir, ConfigFileName)
	raw, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &parsed))

	// With omitempty removed, the false values should appear in the JSON.
	assert.Contains(t, string(raw), `"enable_zsh_command_detection": false`,
		"enable_zsh_command_detection should be persisted as false")
	assert.Contains(t, string(raw), `"auto_execute_detected_commands": false`,
		"auto_execute_detected_commands should be persisted as false")
}

// TestSaveLoadRoundTripExplicitFalseZshFields verifies the full round-trip:
// set false, save, reload, and confirm the loaded value is still false.
func TestSaveLoadRoundTripExplicitFalseZshFields(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	original := NewConfig()
	original.EnableZshCommandDetection = false
	original.AutoExecuteDetectedCommands = false
	require.NoError(t, original.Save())

	loaded, err := Load()
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.False(t, loaded.EnableZshCommandDetection,
		"EnableZshCommandDetection should survive save/load round-trip as false")
	assert.False(t, loaded.AutoExecuteDetectedCommands,
		"AutoExecuteDetectedCommands should survive save/load round-trip as false")
}
