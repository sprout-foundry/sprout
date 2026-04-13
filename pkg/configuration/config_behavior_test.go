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
	assert.False(t, cfg.EnablePreWriteValidation, "EnablePreWriteValidation is zero-value false when not explicitly set in NewConfig")
	assert.Equal(t, "project", cfg.HistoryScope, "HistoryScope should default to 'project'")
	assert.Empty(t, cfg.LastUsedProvider, "LastUsedProvider should default to empty string")
	assert.Equal(t, SelfReviewGateModeOff, cfg.SelfReviewGateMode, "SelfReviewGateMode should default to 'off'")
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

	original := NewConfig()
	original.LastUsedProvider = "deepinfra"
	original.HistoryScope = "global"
	original.EnablePreWriteValidation = false
	original.ReasoningEffort = "high"
	original.SystemPromptText = "custom prompt"
	original.SkipPrompt = true
	original.SelfReviewGateMode = SelfReviewGateModeCode

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
	assert.False(t, loaded.EnablePreWriteValidation)
	assert.Equal(t, "high", loaded.ReasoningEffort)
	assert.Equal(t, "custom prompt", loaded.SystemPromptText)
	assert.True(t, loaded.SkipPrompt)
	assert.Equal(t, SelfReviewGateModeCode, loaded.GetSelfReviewGateMode())
}

// ---------------------------------------------------------------------------
// 3. Validate catches multiple errors
// ---------------------------------------------------------------------------

func TestConfigValidateMultipleErrors(t *testing.T) {
	cfg := NewConfig()
	// Set multiple invalid fields at once
	cfg.SelfReviewGateMode = "totally-invalid"
	cfg.PDFOCREnabled = true
	cfg.PDFOCRProvider = ""
	cfg.PDFOCRModel = ""

	err := cfg.Validate()
	// The Validate method returns the first error encountered, but we can
	// verify the config truly has multiple problems by testing each one
	// individually to confirm they are independently invalid.
	assert.Error(t, err, "Validate should return an error for invalid config")

	// Confirm all conditions are independently invalid
	singleErr := (&Config{SelfReviewGateMode: "totally-invalid"}).Validate()
	assert.Error(t, singleErr)

	singleErr = (&Config{PDFOCREnabled: true, PDFOCRProvider: ""}).Validate()
	assert.Error(t, singleErr)

	singleErr = (&Config{PDFOCREnabled: true, PDFOCRModel: ""}).Validate()
	assert.Error(t, singleErr)

	// However, the original Validate call only returned the first error.
	// Let's also test that a config with only the self-review issue errors.
	cfg2 := NewConfig()
	cfg2.SelfReviewGateMode = "bad"
	cfg2.PDFOCREnabled = false // valid PDF OCR state

	err = cfg2.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "self_review_gate_mode")
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
// 5. GetSubagentType fills in default AllowedTools when none specified
// ---------------------------------------------------------------------------

func TestGetSubagentTypeFillsDefaultAllowedToolsWhenEmpty(t *testing.T) {
	// Build a config where a known persona has an empty AllowedTools slice.
	// We use "coder" since it has defaults in defaultSubagentTypes.
	defaults := defaultSubagentTypes()
	coderDefault, ok := defaults["coder"]
	require.True(t, ok, "coder should exist in defaults")
	require.NotEmpty(t, coderDefault.AllowedTools, "coder default should have AllowedTools")

	// Create config with coder but no AllowedTools
	cfg := &Config{
		SubagentTypes: map[string]SubagentType{
			"coder": {
				ID:           coderDefault.ID,
				Name:         coderDefault.Name,
				Description:  coderDefault.Description,
				Enabled:      true,
				AllowedTools: nil, // explicitly empty
			},
		},
	}

	persona := cfg.GetSubagentType("coder")
	require.NotNil(t, persona)
	assert.NotEmpty(t, persona.AllowedTools, "AllowedTools should be filled from defaults")
	assert.Equal(t, coderDefault.AllowedTools, persona.AllowedTools)
}

// ---------------------------------------------------------------------------
// 6. SelfReviewGateMode set/get round-trip
// ---------------------------------------------------------------------------

func TestSelfReviewGateModeSetGetRoundTrip(t *testing.T) {
	cfg := NewConfig()

	modes := []string{"off", "code", "always"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			err := cfg.SetSelfReviewGateMode(mode)
			require.NoError(t, err)
			assert.Equal(t, mode, cfg.GetSelfReviewGateMode())
		})
	}

	// Case-insensitive / mixed-case round-trip
	t.Run("mixed case ALWAYS", func(t *testing.T) {
		err := cfg.SetSelfReviewGateMode("ALWAYS")
		require.NoError(t, err)
		assert.Equal(t, "always", cfg.GetSelfReviewGateMode())
	})

	t.Run("empty string normalizes to off", func(t *testing.T) {
		err := cfg.SetSelfReviewGateMode("")
		require.NoError(t, err)
		assert.Equal(t, SelfReviewGateModeOff, cfg.GetSelfReviewGateMode())
	})
}

// ---------------------------------------------------------------------------
// 7. SetSelfReviewGateMode rejects invalid modes
// ---------------------------------------------------------------------------

func TestSetSelfReviewGateModeRejectsInvalid(t *testing.T) {
	cfg := NewConfig()

	invalidModes := []string{
		"invalid",
		"ON",
		"CodeReview",
		"maybe",
		"123",
	}

	for _, mode := range invalidModes {
		t.Run(mode, func(t *testing.T) {
			err := cfg.SetSelfReviewGateMode(mode)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "invalid self-review gate mode")
		})
	}

	// Verify the mode did NOT change after rejected attempts
	assert.Equal(t, SelfReviewGateModeOff, cfg.GetSelfReviewGateMode(),
		"mode should remain unchanged after rejected SetSelfReviewGateMode calls")
}

// ---------------------------------------------------------------------------
// Bonus: Verify Save produces valid JSON and Load handles missing file
// ---------------------------------------------------------------------------

func TestLoadReturnsDefaultWhenNoConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	// Point to an empty temp dir — no config.json exists yet
	t.Setenv("LEDIT_CONFIG", tmpDir)

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, ConfigVersion, cfg.Version)
	assert.NotEmpty(t, cfg.ProviderModels)
}

func TestSaveProducesValidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)

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
