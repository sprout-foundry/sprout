package configuration

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
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

	computerUser, ok := cfg.SubagentTypes["computer_user"]
	assert.True(t, ok, "expected computer_user persona in defaults")
	assert.True(t, computerUser.Enabled)
	assert.Contains(t, computerUser.AllowedTools, "write_structured_file")
	assert.Contains(t, computerUser.AllowedTools, "patch_structured_file")

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

func TestGetSubagentTypeFillsDefaultAllowedTools(t *testing.T) {
	cfg := &Config{
		SubagentTypes: map[string]SubagentType{
			"general": {
				ID:      "general",
				Name:    "General",
				Enabled: true,
			},
		},
	}

	persona := cfg.GetSubagentType("general")
	assert.NotNil(t, persona)
	assert.NotEmpty(t, persona.AllowedTools)
	assert.Contains(t, persona.AllowedTools, "read_file")
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
	assert.Equal(t, filepath.Join("/tmp/xdg-home", "ledit"), dir)
}

func TestGetDefaultConfigDirUsesHomeEnvWhenXDGUnset(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home-preferred")

	dir, err := getDefaultConfigDir()
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/home-preferred", ConfigDirName), dir)
}

func TestMergeLegacyStructuredToolsIntoPersonaAllowlists(t *testing.T) {
	cfg := &Config{
		SubagentTypes: map[string]SubagentType{
			"orchestrator": {
				ID:           "orchestrator",
				Name:         "Orchestrator",
				Enabled:      true,
				AllowedTools: []string{"read_file", "write_file", "edit_file"},
			},
			"researcher": {
				ID:           "researcher",
				Name:         "Researcher",
				Enabled:      true,
				AllowedTools: []string{"read_file", "search_files"},
			},
			"web_scraper": {
				ID:           "web_scraper",
				Name:         "Web Scraper",
				Enabled:      true,
				AllowedTools: []string{"read_file", "write_file", "edit_file", "search_files"},
			},
		},
	}

	mergeLegacyStructuredToolsIntoPersonaAllowlists(cfg)

	orchestrator := cfg.SubagentTypes["orchestrator"]
	assert.Contains(t, orchestrator.AllowedTools, "write_structured_file")
	assert.Contains(t, orchestrator.AllowedTools, "patch_structured_file")

	researcher := cfg.SubagentTypes["researcher"]
	assert.NotContains(t, researcher.AllowedTools, "write_structured_file")
	assert.NotContains(t, researcher.AllowedTools, "patch_structured_file")

	webScraper := cfg.SubagentTypes["web_scraper"]
	assert.Contains(t, webScraper.AllowedTools, "shell_command")
}
