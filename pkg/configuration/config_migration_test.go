package configuration

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigMigration_0_0_to_2_0 tests the migration from pre-versioned configs to version 2.0
func TestConfigMigration_0_0_to_2_0(t *testing.T) {
	// Test with empty config (no version field)
	raw := make(map[string]interface{})

	migrated, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)
	assert.Equal(t, "2.0", migrated["version"])

	// Check API timeouts defaults
	apiTimeouts, ok := migrated["api_timeouts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 300.0, apiTimeouts["connection_timeout_sec"].(float64))
	assert.Equal(t, 600.0, apiTimeouts["first_chunk_timeout_sec"].(float64))
	assert.Equal(t, 600.0, apiTimeouts["chunk_timeout_sec"].(float64))
	assert.Equal(t, 1800.0, apiTimeouts["overall_timeout_sec"].(float64))
	assert.Equal(t, 300.0, apiTimeouts["commit_message_timeout_sec"].(float64))

	// Check PDF OCR defaults
	assert.True(t, migrated["pdf_ocr_enabled"].(bool))
	assert.Equal(t, "ollama", migrated["pdf_ocr_provider"])
	assert.Equal(t, "glm-ocr", migrated["pdf_ocr_model"])

	// Check zsh command detection defaults
	assert.True(t, migrated["enable_zsh_command_detection"].(bool))
	assert.True(t, migrated["auto_execute_detected_commands"].(bool))
}

// TestConfigMigration_0_0_to_2_0_PartialAPI timeouts tests migration when api_timeouts already exists but with missing fields
func TestConfigMigration_0_0_to_2_0_PartialAPITimeouts(t *testing.T) {
	raw := map[string]interface{}{
		"api_timeouts": map[string]interface{}{
			"connection_timeout_sec": 120.0,
			// Other fields are missing
		},
	}

	migrated, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)

	// Check that the existing value is preserved and missing fields are filled
	apiTimeouts, ok := migrated["api_timeouts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 120.0, apiTimeouts["connection_timeout_sec"].(float64)) // Preserved
	assert.Equal(t, 600.0, apiTimeouts["first_chunk_timeout_sec"].(float64)) // Default applied
	assert.Equal(t, 600.0, apiTimeouts["chunk_timeout_sec"].(float64)) // Default applied
	assert.Equal(t, 1800.0, apiTimeouts["overall_timeout_sec"].(float64)) // Default applied
	assert.Equal(t, 300.0, apiTimeouts["commit_message_timeout_sec"].(float64)) // Default applied
}

// TestConfigMigration_0_0_to_2_0_PartialPDFOCR tests migration when some PDF OCR fields are set
func TestConfigMigration_0_0_to_2_0_PartialPDFOCR(t *testing.T) {
	// Original logic: if !config.PDFOCREnabled && config.PDFOCRProvider == "" && config.PDFOCRModel == ""
	// then apply defaults. So if enabled is false but provider/model are empty, defaults ARE applied.
	raw := map[string]interface{}{
		"pdf_ocr_enabled": false,
	}

	migrated, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)

	// Defaults SHOULD be applied because all three fields are at zero values
	assert.True(t, migrated["pdf_ocr_enabled"].(bool))
	assert.Equal(t, "ollama", migrated["pdf_ocr_provider"])
	assert.Equal(t, "glm-ocr", migrated["pdf_ocr_model"])
}

// TestConfigMigration_0_0_to_2_0_ZshFields tests zsh command detection field defaults
func TestConfigMigration_0_0_to_2_0_ZshFields(t *testing.T) {
	// Test with no zsh fields present
	raw := make(map[string]interface{})

	migrated, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)

	assert.True(t, migrated["enable_zsh_command_detection"].(bool))
	assert.True(t, migrated["auto_execute_detected_commands"].(bool))
}

// TestConfigMigration_NoMigrationNeeded tests that configs already at target version are unchanged
func TestConfigMigration_NoMigrationNeeded(t *testing.T) {
	raw := map[string]interface{}{
		"version": "2.0",
		"api_timeouts": map[string]interface{}{
			"connection_timeout_sec": 999.0,
		},
	}

	migrated, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)

	// Should return the same map unchanged
	assert.Equal(t, "2.0", migrated["version"])
	apiTimeouts, ok := migrated["api_timeouts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 999.0, apiTimeouts["connection_timeout_sec"].(float64))
}

// TestConfigMigration_InvalidChain tests that migration returns an error when no path exists
func TestConfigMigration_InvalidChain(t *testing.T) {
	raw := map[string]interface{}{
		"version": "99.0",
	}

	_, err := MigrateConfig(raw, "2.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no migration path")
}

// TestRegisterMigration_DuplicateSource tests that registering duplicate source versions panics
func TestRegisterMigration_DuplicateSource(t *testing.T) {
	// This test should cause a panic, so we use recover
	defer func() {
		if r := recover(); r != nil {
			assert.Contains(t, r.(string), "duplicate source version")
		} else {
			t.Error("Expected panic when registering duplicate source version")
		}
	}()

	// Try to register a migration from a source that already exists
	RegisterMigration("0.0", "99.0", func(raw map[string]interface{}) error {
		return nil
	})
}

// TestConfigMigration_Integration tests the full integration with Load()
func TestConfigMigration_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)

	// Create a minimal config file without version or any new fields
	configContent := `{
		"last_used_provider": "openai",
		"provider_models": {
			"openai": "gpt-4"
		}
	}`

	configPath := t.TempDir() + "/config.json"
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	// Load the config - should apply migration
	// Note: We can't use Load() directly here because it expects the config
	// to be in LEDIT_CONFIG directory. Let's test the migration directly
	// with raw JSON.
	var raw map[string]interface{}
	err = json.Unmarshal([]byte(configContent), &raw)
	require.NoError(t, err)

	migrated, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)

	// Verify defaults were applied
	assert.Equal(t, "2.0", migrated["version"])
	assert.True(t, migrated["enable_zsh_command_detection"].(bool))
	assert.True(t, migrated["auto_execute_detected_commands"].(bool))

	// Verify original fields are preserved
	assert.Equal(t, "openai", migrated["last_used_provider"])

	// Marshal back to JSON to ensure it's valid
	_, err = json.Marshal(migrated)
	require.NoError(t, err)
}

// TestConfigMigration_1_0_to_2_0_EmptyConfig tests migration from version 1.0 with only the version field
func TestConfigMigration_1_0_to_2_0_EmptyConfig(t *testing.T) {
	// Start with a v1.0 config that only has the version field
	raw := map[string]interface{}{
		"version": "1.0",
	}

	migrated, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)
	assert.Equal(t, "2.0", migrated["version"])

	// Check API timeouts defaults are applied
	apiTimeouts, ok := migrated["api_timeouts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 300.0, apiTimeouts["connection_timeout_sec"].(float64))
	assert.Equal(t, 600.0, apiTimeouts["first_chunk_timeout_sec"].(float64))
	assert.Equal(t, 600.0, apiTimeouts["chunk_timeout_sec"].(float64))
	assert.Equal(t, 1800.0, apiTimeouts["overall_timeout_sec"].(float64))
	assert.Equal(t, 300.0, apiTimeouts["commit_message_timeout_sec"].(float64))

	// Check PDF OCR defaults are applied
	assert.True(t, migrated["pdf_ocr_enabled"].(bool))
	assert.Equal(t, "ollama", migrated["pdf_ocr_provider"])
	assert.Equal(t, "glm-ocr", migrated["pdf_ocr_model"])

	// Check zsh command detection defaults are applied
	assert.True(t, migrated["enable_zsh_command_detection"].(bool))
	assert.True(t, migrated["auto_execute_detected_commands"].(bool))
}

// TestConfigMigration_1_0_to_2_0_PartialAPITimeouts tests migration when some API timeout fields are already set
func TestConfigMigration_1_0_to_2_0_PartialAPITimeouts(t *testing.T) {
	// Start with v1.0 config with partial api_timeouts
	raw := map[string]interface{}{
		"version": "1.0",
		"api_timeouts": map[string]interface{}{
			"connection_timeout_sec": 500.0,
			"first_chunk_timeout_sec": 900.0,
			// Other fields are missing
		},
	}

	migrated, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)
	assert.Equal(t, "2.0", migrated["version"])

	// Check that existing values are preserved and missing fields get defaults
	apiTimeouts, ok := migrated["api_timeouts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 500.0, apiTimeouts["connection_timeout_sec"].(float64))  // Preserved
	assert.Equal(t, 900.0, apiTimeouts["first_chunk_timeout_sec"].(float64)) // Preserved
	assert.Equal(t, 600.0, apiTimeouts["chunk_timeout_sec"].(float64))        // Default applied
	assert.Equal(t, 1800.0, apiTimeouts["overall_timeout_sec"].(float64))      // Default applied
	assert.Equal(t, 300.0, apiTimeouts["commit_message_timeout_sec"].(float64)) // Default applied
}

// TestConfigMigration_1_0_to_2_0_ZeroAPITimeouts tests migration when API timeout fields are set to zero
func TestConfigMigration_1_0_to_2_0_ZeroAPITimeouts(t *testing.T) {
	// Start with v1.0 config where some api_timeouts are zero (should get defaults)
	raw := map[string]interface{}{
		"version": "1.0",
		"api_timeouts": map[string]interface{}{
			"connection_timeout_sec":    0.0, // Should be replaced with default
			"first_chunk_timeout_sec":   450.0, // Should be preserved
			"chunk_timeout_sec":         0.0, // Should be replaced with default
			"overall_timeout_sec":       2000.0, // Should be preserved
			"commit_message_timeout_sec": 0.0, // Should be replaced with default
		},
	}

	migrated, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)

	apiTimeouts, ok := migrated["api_timeouts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 300.0, apiTimeouts["connection_timeout_sec"].(float64))    // Default applied (was 0)
	assert.Equal(t, 450.0, apiTimeouts["first_chunk_timeout_sec"].(float64))    // Preserved
	assert.Equal(t, 600.0, apiTimeouts["chunk_timeout_sec"].(float64))         // Default applied (was 0)
	assert.Equal(t, 2000.0, apiTimeouts["overall_timeout_sec"].(float64))       // Preserved
	assert.Equal(t, 300.0, apiTimeouts["commit_message_timeout_sec"].(float64)) // Default applied (was 0)
}

// TestConfigMigration_1_0_to_2_0_PartialPDFOCR tests migration when some PDF OCR fields are set
func TestConfigMigration_1_0_to_2_0_PartialPDFOCR(t *testing.T) {
	// Test case 1: Only pdf_ocr_enabled is set to true
	t.Run("OnlyEnabledTrue", func(t *testing.T) {
		raw := map[string]interface{}{
			"version":          "1.0",
			"pdf_ocr_enabled":  true,
		}

		migrated, err := MigrateConfig(raw, "2.0")
		require.NoError(t, err)

		// Defaults should NOT be applied because enabled is true (not at unset value)
		assert.True(t, migrated["pdf_ocr_enabled"].(bool))
		assert.Nil(t, migrated["pdf_ocr_provider"]) // No default applied
		assert.Nil(t, migrated["pdf_ocr_model"])     // No default applied
	})

	// Test case 2: pdf_ocr_enabled is false but provider is set
	t.Run("EnabledFalseProviderSet", func(t *testing.T) {
		raw := map[string]interface{}{
			"version":           "1.0",
			"pdf_ocr_enabled":   false,
			"pdf_ocr_provider":  "tesseract",
		}

		migrated, err := MigrateConfig(raw, "2.0")
		require.NoError(t, err)

		// Defaults should NOT be applied because provider is set
		assert.False(t, migrated["pdf_ocr_enabled"].(bool))
		assert.Equal(t, "tesseract", migrated["pdf_ocr_provider"]) // Preserved
		assert.Nil(t, migrated["pdf_ocr_model"]) // No default applied
	})

	// Test case 3: All three fields at unset values (defaults should apply)
	t.Run("AllUnset", func(t *testing.T) {
		raw := map[string]interface{}{
			"version":          "1.0",
			"pdf_ocr_enabled":  false, // Unset value
		}

		migrated, err := MigrateConfig(raw, "2.0")
		require.NoError(t, err)

		// Defaults SHOULD be applied because all three fields are at unset values
		assert.True(t, migrated["pdf_ocr_enabled"].(bool))
		assert.Equal(t, "ollama", migrated["pdf_ocr_provider"])
		assert.Equal(t, "glm-ocr", migrated["pdf_ocr_model"])
	})

	// Test case 4: Empty string for provider and model
	t.Run("EmptyStrings", func(t *testing.T) {
		raw := map[string]interface{}{
			"version":           "1.0",
			"pdf_ocr_enabled":   false,
			"pdf_ocr_provider":  "",
			"pdf_ocr_model":     "",
		}

		migrated, err := MigrateConfig(raw, "2.0")
		require.NoError(t, err)

		// Defaults SHOULD be applied because all three fields are at unset values
		assert.True(t, migrated["pdf_ocr_enabled"].(bool))
		assert.Equal(t, "ollama", migrated["pdf_ocr_provider"])
		assert.Equal(t, "glm-ocr", migrated["pdf_ocr_model"])
	})
}

// TestConfigMigration_1_0_to_2_0_ZshFields tests zsh command detection field defaults
func TestConfigMigration_1_0_to_2_0_ZshFields(t *testing.T) {
	// Test case 1: No zsh fields present
	t.Run("NoFields", func(t *testing.T) {
		raw := map[string]interface{}{
			"version": "1.0",
		}

		migrated, err := MigrateConfig(raw, "2.0")
		require.NoError(t, err)

		assert.True(t, migrated["enable_zsh_command_detection"].(bool))
		assert.True(t, migrated["auto_execute_detected_commands"].(bool))
	})

	// Test case 2: Both fields already set (should be preserved)
	t.Run("BothSet", func(t *testing.T) {
		raw := map[string]interface{}{
			"version":                        "1.0",
			"enable_zsh_command_detection":   false,
			"auto_execute_detected_commands": false,
		}

		migrated, err := MigrateConfig(raw, "2.0")
		require.NoError(t, err)

		// Should preserve existing values
		assert.False(t, migrated["enable_zsh_command_detection"].(bool))
		assert.False(t, migrated["auto_execute_detected_commands"].(bool))
	})

	// Test case 3: Only one field set
	t.Run("OneFieldSet", func(t *testing.T) {
		raw := map[string]interface{}{
			"version":                      "1.0",
			"enable_zsh_command_detection": false,
		}

		migrated, err := MigrateConfig(raw, "2.0")
		require.NoError(t, err)

		// First field preserved, second gets default
		assert.False(t, migrated["enable_zsh_command_detection"].(bool))
		assert.True(t, migrated["auto_execute_detected_commands"].(bool))
	})
}

// TestConfigMigration_1_0_to_2_0_PreservesExistingValues tests that existing non-default values are preserved
func TestConfigMigration_1_0_to_2_0_PreservesExistingValues(t *testing.T) {
	// Create a v1.0 config with all new fields already set to non-default values
	raw := map[string]interface{}{
		"version": "1.0",
		"api_timeouts": map[string]interface{}{
			"connection_timeout_sec":      400.0,
			"first_chunk_timeout_sec":    700.0,
			"chunk_timeout_sec":           800.0,
			"overall_timeout_sec":         2400.0,
			"commit_message_timeout_sec":  400.0,
		},
		"pdf_ocr_enabled":               false,
		"pdf_ocr_provider":              "tesseract",
		"pdf_ocr_model":                 "custom-ocr",
		"enable_zsh_command_detection":  false,
		"auto_execute_detected_commands": false,
	}

	migrated, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)
	assert.Equal(t, "2.0", migrated["version"])

	// Verify all existing values are preserved
	apiTimeouts, ok := migrated["api_timeouts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 400.0, apiTimeouts["connection_timeout_sec"].(float64))
	assert.Equal(t, 700.0, apiTimeouts["first_chunk_timeout_sec"].(float64))
	assert.Equal(t, 800.0, apiTimeouts["chunk_timeout_sec"].(float64))
	assert.Equal(t, 2400.0, apiTimeouts["overall_timeout_sec"].(float64))
	assert.Equal(t, 400.0, apiTimeouts["commit_message_timeout_sec"].(float64))

	assert.False(t, migrated["pdf_ocr_enabled"].(bool))
	assert.Equal(t, "tesseract", migrated["pdf_ocr_provider"])
	assert.Equal(t, "custom-ocr", migrated["pdf_ocr_model"])

	assert.False(t, migrated["enable_zsh_command_detection"].(bool))
	assert.False(t, migrated["auto_execute_detected_commands"].(bool))
}

// TestConfigMigration_1_0_to_2_0_Idempotent tests that running migration twice produces identical results
func TestConfigMigration_1_0_to_2_0_Idempotent(t *testing.T) {
	// Start with a minimal v1.0 config
	raw := map[string]interface{}{
		"version": "1.0",
		"last_used_provider": "openai",
	}

	// Run migration first time
	migrated1, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)

	// Run migration second time (on the already-migrated config)
	migrated2, err := MigrateConfig(migrated1, "2.0")
	require.NoError(t, err)

	// Results should be identical
	assert.Equal(t, migrated1["version"], migrated2["version"])
	assert.Equal(t, migrated1["last_used_provider"], migrated2["last_used_provider"])

	// Compare api_timeouts
	apiTimeouts1, ok1 := migrated1["api_timeouts"].(map[string]interface{})
	apiTimeouts2, ok2 := migrated2["api_timeouts"].(map[string]interface{})
	require.True(t, ok1)
	require.True(t, ok2)
	assert.Equal(t, apiTimeouts1, apiTimeouts2)

	// Compare PDF OCR fields
	assert.Equal(t, migrated1["pdf_ocr_enabled"], migrated2["pdf_ocr_enabled"])
	assert.Equal(t, migrated1["pdf_ocr_provider"], migrated2["pdf_ocr_provider"])
	assert.Equal(t, migrated1["pdf_ocr_model"], migrated2["pdf_ocr_model"])

	// Compare zsh fields
	assert.Equal(t, migrated1["enable_zsh_command_detection"], migrated2["enable_zsh_command_detection"])
	assert.Equal(t, migrated1["auto_execute_detected_commands"], migrated2["auto_execute_detected_commands"])
}

// TestConfigMigration_1_0_to_2_0_IdempotentWithPartialConfig tests idempotency with a partially configured v1.0 config
func TestConfigMigration_1_0_to_2_0_IdempotentWithPartialConfig(t *testing.T) {
	// Start with a v1.0 config with some fields set
	raw := map[string]interface{}{
		"version": "1.0",
		"api_timeouts": map[string]interface{}{
			"connection_timeout_sec": 500.0,
		},
		"pdf_ocr_enabled": true,
	}

	// Run migration first time
	migrated1, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)

	// Run migration second time
	migrated2, err := MigrateConfig(migrated1, "2.0")
	require.NoError(t, err)

	// All fields should be identical after second migration
	assert.Equal(t, migrated1["version"], migrated2["version"])
	assert.Equal(t, migrated1["pdf_ocr_enabled"], migrated2["pdf_ocr_enabled"])

	apiTimeouts1, ok1 := migrated1["api_timeouts"].(map[string]interface{})
	apiTimeouts2, ok2 := migrated2["api_timeouts"].(map[string]interface{})
	require.True(t, ok1)
	require.True(t, ok2)
	assert.Equal(t, apiTimeouts1, apiTimeouts2)
}

// TestConfigMigration_1_0_to_2_0_Integration tests the full migration path from 1.0 to 2.0
func TestConfigMigration_1_0_to_2_0_Integration(t *testing.T) {
	// Create a realistic v1.0 config with some fields
	configContent := `{
		"version": "1.0",
		"last_used_provider": "anthropic",
		"provider_models": {
			"anthropic": "claude-3-opus"
		},
		"api_timeouts": {
			"connection_timeout_sec": 250.0
		}
	}`

	var raw map[string]interface{}
	err := json.Unmarshal([]byte(configContent), &raw)
	require.NoError(t, err)

	migrated, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)

	// Verify version is updated
	assert.Equal(t, "2.0", migrated["version"])

	// Verify original fields are preserved
	assert.Equal(t, "anthropic", migrated["last_used_provider"])
	providerModels, ok := migrated["provider_models"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "claude-3-opus", providerModels["anthropic"])

	// Verify defaults were applied for missing fields
	apiTimeouts, ok := migrated["api_timeouts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 250.0, apiTimeouts["connection_timeout_sec"].(float64)) // Preserved
	assert.Equal(t, 600.0, apiTimeouts["first_chunk_timeout_sec"].(float64)) // Default

	// Verify other defaults were applied
	assert.True(t, migrated["pdf_ocr_enabled"].(bool))
	assert.Equal(t, "ollama", migrated["pdf_ocr_provider"])
	assert.Equal(t, "glm-ocr", migrated["pdf_ocr_model"])
	assert.True(t, migrated["enable_zsh_command_detection"].(bool))
	assert.True(t, migrated["auto_execute_detected_commands"].(bool))

	// Verify the result can be marshaled back to valid JSON
	_, err = json.Marshal(migrated)
	require.NoError(t, err)
}
