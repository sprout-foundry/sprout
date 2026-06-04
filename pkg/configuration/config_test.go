package configuration

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "PDF OCR disabled - should pass",
			config: &Config{
				PDFOCREnabled: false,
			},
			expectError: false,
		},
		{
			name: "PDF OCR enabled with provider and model - should pass",
			config: &Config{
				PDFOCREnabled:  true,
				PDFOCRProvider: "ollama",
				PDFOCRModel:    "glm-ocr",
			},
			expectError: false,
		},
		{
			name: "PDF OCR enabled but empty provider - should fail",
			config: &Config{
				PDFOCREnabled:  true,
				PDFOCRProvider: "",
				PDFOCRModel:    "glm-ocr",
			},
			expectError: true,
			errorMsg:    "PDF OCR provider cannot be empty when PDF OCR is enabled",
		},
		{
			name: "PDF OCR enabled but empty model - should fail",
			config: &Config{
				PDFOCREnabled:  true,
				PDFOCRProvider: "ollama",
				PDFOCRModel:    "",
			},
			expectError: true,
			errorMsg:    "PDF OCR model cannot be empty when PDF OCR is enabled",
		},
		{
			name: "PDF OCR enabled with empty provider and model - should fail",
			config: &Config{
				PDFOCREnabled:  true,
				PDFOCRProvider: "",
				PDFOCRModel:    "",
			},
			expectError: true,
			errorMsg:    "PDF OCR provider cannot be empty when PDF OCR is enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewConfigIncludesWebScraperPersona(t *testing.T) {
	cfg := NewConfig()
	assert.NotNil(t, cfg.SubagentTypes)

	persona, ok := cfg.SubagentTypes["web_scraper"]
	assert.True(t, ok, "expected web_scraper persona in defaults")
	assert.True(t, persona.Enabled)
	assert.NotEmpty(t, persona.SystemPrompt)
	assert.NotEmpty(t, persona.AllowedTools)
	assert.Contains(t, persona.AllowedTools, "web_search")
	assert.Contains(t, persona.AllowedTools, "fetch_url")
	assert.Contains(t, persona.AllowedTools, "edit_file")
	assert.Contains(t, persona.AllowedTools, "shell_command")
	assert.Contains(t, persona.AllowedTools, "write_structured_file")
	assert.Contains(t, persona.AllowedTools, "patch_structured_file")

	orchestrator, ok := cfg.SubagentTypes["orchestrator"]
	assert.True(t, ok, "expected orchestrator persona in defaults")
	assert.True(t, orchestrator.Enabled)

	coderPersona, ok := cfg.SubagentTypes["coder"]
	assert.True(t, ok, "expected coder persona in defaults")
	assert.True(t, coderPersona.Enabled)
	assert.Contains(t, coderPersona.AllowedTools, "write_structured_file")
	assert.Contains(t, coderPersona.AllowedTools, "patch_structured_file")
	assert.Contains(t, coderPersona.AllowedTools, "browse_url")

	debuggerPersona, ok := cfg.SubagentTypes["debugger"]
	assert.True(t, ok, "expected debugger persona in defaults")
	assert.True(t, debuggerPersona.Enabled)
	assert.Contains(t, debuggerPersona.AllowedTools, "browse_url")

	assert.Contains(t, persona.AllowedTools, "browse_url")

	refactorPersona, ok := cfg.SubagentTypes["refactor"]
	assert.True(t, ok, "expected refactor persona in defaults")
	assert.True(t, refactorPersona.Enabled)
	assert.NotEmpty(t, refactorPersona.SystemPrompt)
	assert.NotEmpty(t, refactorPersona.AllowedTools)
	assert.Contains(t, refactorPersona.AllowedTools, "edit_file")
	assert.Contains(t, refactorPersona.AllowedTools, "write_structured_file")
	assert.Contains(t, refactorPersona.AllowedTools, "patch_structured_file")
	assert.Contains(t, refactorPersona.AllowedTools, "search_files")
}

func TestGetSubagentType_AllowedToolsFromCatalog(t *testing.T) {
	cfg := NewConfig()
	persona := cfg.GetSubagentType("general")
	assert.NotNil(t, persona)
	assert.NotEmpty(t, persona.AllowedTools)
	assert.Contains(t, persona.AllowedTools, "read_file")
}

func TestGetSubagentType_DisabledReturnsNil(t *testing.T) {
	cfg := NewConfig()
	cfg.SetPersonaDisabled("general", true)
	assert.Nil(t, cfg.GetSubagentType("general"))
	cfg.SetPersonaDisabled("general", false)
	assert.NotNil(t, cfg.GetSubagentType("general"))
}

func TestSelfReviewGateModeDefaultsAndNormalization(t *testing.T) {
	cfg := NewConfig()
	assert.Equal(t, SelfReviewGateModeOff, cfg.GetSelfReviewGateMode())

	cfg.SelfReviewGateMode = ""
	assert.Equal(t, SelfReviewGateModeOff, cfg.GetSelfReviewGateMode())

	err := cfg.SetSelfReviewGateMode("ALWAYS")
	assert.NoError(t, err)
	assert.Equal(t, SelfReviewGateModeAlways, cfg.GetSelfReviewGateMode())

	err = cfg.SetSelfReviewGateMode("off")
	assert.NoError(t, err)
	assert.Equal(t, SelfReviewGateModeOff, cfg.GetSelfReviewGateMode())
}

func TestConfigValidateSelfReviewGateMode(t *testing.T) {
	cfg := NewConfig()
	cfg.SelfReviewGateMode = "invalid-mode"

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "self_review_gate_mode")
}

func TestGetDefaultConfigDirPrefersXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-home")
	t.Setenv("HOME", "/tmp/home-ignored")

	dir, err := getDefaultConfigDir()
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/xdg-home", "sprout"), dir)
}

func TestGetDefaultConfigDirUsesHomeEnvWhenXDGUnset(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home-preferred")

	dir, err := getDefaultConfigDir()
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/home-preferred", ".config", "sprout"), dir)
}

func TestGetSubagentMaxParallel(t *testing.T) {
	tests := []struct {
		name       string
		config    *Config
		expected  int
	}{
		{
			name: "returns configured value when greater than 0",
			config: &Config{
				SubagentMaxParallel: 5,
			},
			expected: 5,
		},
		{
			name: "returns default 2 when set to 0",
			config: &Config{
				SubagentMaxParallel: 0,
			},
			expected: 2,
		},
		{
			name: "returns default 2 when set to negative value",
			config: &Config{
				SubagentMaxParallel: -1,
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetSubagentMaxParallel()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetSubagentParallelEnabled(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name      string
		config   *Config
		expected bool
	}{
		{
			name: "returns true when field is explicitly set to true",
			config: &Config{
				SubagentParallelEnabled: &trueVal,
			},
			expected: true,
		},
		{
			name:      "returns false when field is explicitly set to false",
			config:   &Config{SubagentParallelEnabled: &falseVal},
			expected: false,
		},
		{
			name:      "returns true when field not set (default config)",
			config:   &Config{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetSubagentParallelEnabled()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPersistentContextConfigResolve_NilReturnsDefaults(t *testing.T) {
	var cfg *PersistentContextConfig
	result := cfg.Resolve()

	assert.True(t, result.ProactiveContextEnabled)
	assert.Equal(t, 5, result.MaxContextualResults)
	assert.Equal(t, 0.50, result.MinRelevanceScore)
	assert.Equal(t, 4000, result.MaxContextChars)
	assert.True(t, result.WorkspaceScopedRetrieval, "default is now true (workspace scoping on by default)")
	assert.True(t, result.DriftDetectionEnabled)
	assert.Equal(t, 0.60, result.DriftThreshold)
	assert.Equal(t, 5, result.DriftCheckInterval)
}

func TestPersistentContextConfigResolve_ExplicitValuesPreserved(t *testing.T) {
	cfg := &PersistentContextConfig{
		ProactiveContextEnabled:   false,
		MaxContextualResults:      10,
		MinRelevanceScore:         0.75,
		MaxContextChars:           8000,
		WorkspaceScopedRetrieval:  true,
		DriftDetectionEnabled:     false,
		DriftThreshold:            0.80,
		DriftCheckInterval:        10,
	}
	result := cfg.Resolve()

	assert.False(t, result.ProactiveContextEnabled)
	assert.Equal(t, 10, result.MaxContextualResults)
	assert.Equal(t, 0.75, result.MinRelevanceScore)
	assert.Equal(t, 8000, result.MaxContextChars)
	assert.True(t, result.WorkspaceScopedRetrieval)
	assert.False(t, result.DriftDetectionEnabled)
	assert.Equal(t, 0.80, result.DriftThreshold)
	assert.Equal(t, 10, result.DriftCheckInterval)
}

func TestPersistentContextConfigResolve_PartialOverrides(t *testing.T) {
	cfg := &PersistentContextConfig{
		ProactiveContextEnabled:  false,
		MaxContextualResults:     0,    // zero — should get default
		MinRelevanceScore:        0.8,  // explicit
		MaxContextChars:          0,    // zero — should get default
		WorkspaceScopedRetrieval: true,
		DriftThreshold:           0.70, // explicit
		DriftCheckInterval:       0,    // zero — should get default
	}
	result := cfg.Resolve()

	assert.False(t, result.ProactiveContextEnabled)
	assert.Equal(t, 5, result.MaxContextualResults)      // default
	assert.Equal(t, 0.8, result.MinRelevanceScore)       // explicit
	assert.Equal(t, 4000, result.MaxContextChars)        // default
	assert.True(t, result.WorkspaceScopedRetrieval)
	assert.False(t, result.DriftDetectionEnabled)        // false (zero value) treated as explicit
	assert.Equal(t, 0.70, result.DriftThreshold)         // explicit
	assert.Equal(t, 5, result.DriftCheckInterval)        // default
}

func TestPersistentContextConfigResolve_DoesNotMutateOriginal(t *testing.T) {
	cfg := &PersistentContextConfig{
		ProactiveContextEnabled:  false,
		MaxContextualResults:     0,
		MinRelevanceScore:        0.8,
		MaxContextChars:          0,
		WorkspaceScopedRetrieval: true,
	}

	// Capture original state
	orig := *cfg

	_ = cfg.Resolve()
	_ = cfg.Resolve() // call multiple times

	assert.Equal(t, orig, *cfg, "original config should not be mutated by Resolve()")
}

// =============================================================================
// PersistentContextConfig RetentionDays tests (SP-033-3c)
// =============================================================================

func TestPersistentContextConfig_Resolve_RetentionDays_Default(t *testing.T) {
	cfg := &PersistentContextConfig{}
	result := cfg.Resolve()

	assert.Equal(t, 0, result.RetentionDays, "RetentionDays should default to 0 (never expire)")
}

func TestPersistentContextConfig_Resolve_RetentionDays_Explicit(t *testing.T) {
	cfg := &PersistentContextConfig{
		RetentionDays: 30,
	}
	result := cfg.Resolve()

	assert.Equal(t, 30, result.RetentionDays, "RetentionDays should preserve explicit value")
}

func TestPersistentContextConfig_Resolve_RetentionDays_Negative(t *testing.T) {
	cfg := &PersistentContextConfig{
		RetentionDays: -1,
	}
	result := cfg.Resolve()

	assert.Equal(t, 0, result.RetentionDays, "Negative RetentionDays should be treated as 0 (never expire)")
}

func TestPersistentContextConfig_JSON_Marshal_Unmarshal_RetentionDays(t *testing.T) {
	cfg := &PersistentContextConfig{
		ProactiveContextEnabled: true,
		RetentionDays:           30,
	}

	data, err := json.Marshal(cfg)
	assert.NoError(t, err)

	// Verify the JSON contains the retentionDays key
	assert.Contains(t, string(data), "retentionDays")
	assert.Contains(t, string(data), "30")

	var decoded PersistentContextConfig
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, 30, decoded.RetentionDays, "RetentionDays should survive JSON round-trip")
}

func TestPersistentContextConfig_JSON_OmitsRetentionDaysWhenZero(t *testing.T) {
	cfg := &PersistentContextConfig{
		ProactiveContextEnabled: true,
		RetentionDays:           0,
	}

	data, err := json.Marshal(cfg)
	assert.NoError(t, err)

	// With omitempty, zero RetentionDays should not appear in JSON
	assert.NotContains(t, string(data), "retentionDays",
		"zero RetentionDays should be omitted from JSON due to omitempty tag")
}

// TestPersonaCatalog_Immutable verifies that personas come from the embedded
// catalog and are not mutated by writes to SubagentTypes after construction.
// This is the post-override-removal contract: user code can read SubagentTypes
// but the persistent layer ignores it.
func TestPersonaCatalog_Immutable(t *testing.T) {
	cfg := NewConfig()

	// Mutate a built-in entry in memory.
	general, ok := cfg.SubagentTypes["general"]
	require.True(t, ok, "general persona should exist in catalog")
	general.AllowedTools = []string{"read_file"}
	cfg.SubagentTypes["general"] = general

	// A fresh config should still have the catalog defaults.
	fresh := NewConfig()
	freshGeneral, ok := fresh.SubagentTypes["general"]
	require.True(t, ok)
	assert.Greater(t, len(freshGeneral.AllowedTools), 1,
		"catalog should hydrate full tool list, not be affected by prior mutation")
}

