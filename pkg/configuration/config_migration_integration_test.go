package configuration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigMigration_Integration_WithLoad tests that migration works through the Load() function
func TestConfigMigration_Integration_WithLoad(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	// Create a minimal config file without version or any new fields
	// This simulates a pre-versioned config (0.0)
	configContent := `{
		"last_used_provider": "openai",
		"provider_models": {
			"openai": "gpt-4"
		}
	}`

	configPath := filepath.Join(tmpDir, ConfigFileName)
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	// Load the config - should apply migration
	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify migration was applied
	assert.Equal(t, "2.0", cfg.Version)

	// Verify API timeouts defaults were applied
	assert.NotNil(t, cfg.APITimeouts)
	assert.Equal(t, 300, cfg.APITimeouts.ConnectionTimeoutSec)
	assert.Equal(t, 600, cfg.APITimeouts.FirstChunkTimeoutSec)
	assert.Equal(t, 600, cfg.APITimeouts.ChunkTimeoutSec)
	assert.Equal(t, 1800, cfg.APITimeouts.OverallTimeoutSec)
	assert.Equal(t, 300, cfg.APITimeouts.CommitMessageTimeoutSec)

	// Verify PDF OCR defaults were applied
	assert.True(t, cfg.PDFOCREnabled)
	assert.Equal(t, "ollama", cfg.PDFOCRProvider)
	assert.Equal(t, "glm-ocr", cfg.PDFOCRModel)

	// Verify zsh command detection defaults were applied
	assert.True(t, cfg.EnableZshCommandDetection)
	assert.True(t, cfg.AutoExecuteDetectedCommands)

	// Verify original fields are preserved
	assert.Equal(t, "openai", cfg.LastUsedProvider)
	assert.Equal(t, "gpt-4", cfg.ProviderModels["openai"])
}
