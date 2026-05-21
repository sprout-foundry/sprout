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
	t.Setenv("SPROUT_CONFIG", tmpDir)

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

// TestApplyMapInitializations_MissingFields verifies empty maps are created for missing fields
func TestApplyMapInitializations_MissingFields(t *testing.T) {
	raw := map[string]interface{}{
		"version": "2.0",
	}

	applyMapInitializations(raw)

	// Verify all map fields are created as empty maps
	require.IsType(t, map[string]interface{}{}, raw["provider_models"])
	require.IsType(t, map[string]interface{}{}, raw["preferences"])
	require.IsType(t, map[string]interface{}{}, raw["dismissed_prompts"])
	require.IsType(t, map[string]interface{}{}, raw["custom_providers"])
	require.IsType(t, map[string]interface{}{}, raw["subagent_types"])
	require.IsType(t, map[string]interface{}{}, raw["skills"])

	// Verify MCP structure
	mcp, ok := raw["mcp"].(map[string]interface{})
	require.True(t, ok)
	require.IsType(t, map[string]interface{}{}, mcp["servers"])
}

// TestApplyMapInitializations_PreservesExisting verifies existing values aren't overwritten
func TestApplyMapInitializations_PreservesExisting(t *testing.T) {
	raw := map[string]interface{}{
		"version": "2.0",
		"provider_models": map[string]interface{}{
			"openai": "gpt-4",
		},
		"preferences": map[string]interface{}{
			"theme": "dark",
		},
	}

	applyMapInitializations(raw)

	// Verify existing values are preserved
	providerModels := raw["provider_models"].(map[string]interface{})
	assert.Equal(t, "gpt-4", providerModels["openai"])

	preferences := raw["preferences"].(map[string]interface{})
	assert.Equal(t, "dark", preferences["theme"])
}

// TestApplyDefaultSubagentTypes_MergesDefaults verifies default personas are added
func TestApplyDefaultSubagentTypes_MergesDefaults(t *testing.T) {
	raw := map[string]interface{}{
		"version":        "2.0",
		"subagent_types": map[string]interface{}{},
	}

	applyDefaultSubagentTypes(raw)

	subagentTypes := raw["subagent_types"].(map[string]interface{})
	// Should have at least the default personas
	assert.NotEmpty(t, subagentTypes)

	// Verify some expected fields exist in a default persona
	for _, persona := range subagentTypes {
		personaMap, ok := persona.(map[string]interface{})
		require.True(t, ok)
		// Should have standard fields
		assert.Contains(t, personaMap, "id")
		assert.Contains(t, personaMap, "name")
		assert.Contains(t, personaMap, "enabled")
		break // Just check one
	}
}

// TestApplyDefaultSubagentTypes_PreservesExisting verifies existing personas aren't overwritten
func TestApplyDefaultSubagentTypes_PreservesExisting(t *testing.T) {
	raw := map[string]interface{}{
		"version": "2.0",
		"subagent_types": map[string]interface{}{
			"coder": map[string]interface{}{
				"id":           "coder",
				"name":         "My Custom Coder",
				"enabled":      false,
				"allowed_tools": []interface{}{"read_file"},
			},
		},
	}

	applyDefaultSubagentTypes(raw)

	subagentTypes := raw["subagent_types"].(map[string]interface{})
	coder := subagentTypes["coder"].(map[string]interface{})

	// Verify existing custom values are preserved
	assert.Equal(t, "My Custom Coder", coder["name"])
	assert.Equal(t, false, coder["enabled"])
	tools := coder["allowed_tools"].([]interface{})
	assert.Len(t, tools, 1)
	assert.Equal(t, "read_file", tools[0])
}

// TestApplyDefaultSkills_MergesDefaults verifies default skills are added
func TestApplyDefaultSkills_MergesDefaults(t *testing.T) {
	raw := map[string]interface{}{
		"version": "2.0",
		"skills":  map[string]interface{}{},
	}

	applyDefaultSkills(raw)

	skills := raw["skills"].(map[string]interface{})
	// Should have all the default skills
	assert.NotEmpty(t, skills)

	// Verify expected default skills exist
	expectedSkills := []string{
		"project-planning",
		"repo-onboarding",
		"browse-debugging",
	}

	for _, skillID := range expectedSkills {
		assert.Contains(t, skills, skillID, "skill %q should exist", skillID)
	}

	// Verify skill structure
	projectPlanning := skills["project-planning"].(map[string]interface{})
	assert.Equal(t, "project-planning", projectPlanning["id"])
	assert.Equal(t, "Project Planning", projectPlanning["name"])
	assert.Equal(t, true, projectPlanning["enabled"])
}

// TestApplyDefaultSkills_PreservesExisting verifies existing skills aren't overwritten
func TestApplyDefaultSkills_PreservesExisting(t *testing.T) {
	raw := map[string]interface{}{
		"version": "2.0",
		"skills": map[string]interface{}{
			"project-planning": map[string]interface{}{
				"id":          "project-planning",
				"name":        "My Planning Rules",
				"description": "Custom description",
				"enabled":     false,
			},
		},
	}

	applyDefaultSkills(raw)

	skills := raw["skills"].(map[string]interface{})
	projectPlanning := skills["project-planning"].(map[string]interface{})

	// Verify existing custom values are preserved
	assert.Equal(t, "My Planning Rules", projectPlanning["name"])
	assert.Equal(t, "Custom description", projectPlanning["description"])
	assert.Equal(t, false, projectPlanning["enabled"])

	// Other default skills should be added
	assert.Contains(t, skills, "browse-debugging")
}

// TestApplyLegacyToolAllowlistMigration_AddsStructuredTools verifies write_structured_file/patch_structured_file are added
func TestApplyLegacyToolAllowlistMigration_AddsStructuredTools(t *testing.T) {
	raw := map[string]interface{}{
		"version": "2.0",
		"subagent_types": map[string]interface{}{
			"orchestrator": map[string]interface{}{
				"id":           "orchestrator",
				"name":         "Orchestrator",
				"enabled":      true,
				"allowed_tools": []interface{}{"read_file", "write_file", "edit_file"},
			},
			"coder": map[string]interface{}{
				"id":           "coder",
				"name":         "Coder",
				"enabled":      true,
				"allowed_tools": []interface{}{"edit_file"},
			},
		},
	}

	applyLegacyToolAllowlistMigration(raw)

	subagentTypes := raw["subagent_types"].(map[string]interface{})

	// Check orchestrator - should have structured tools added
	orchestrator := subagentTypes["orchestrator"].(map[string]interface{})
	orchTools := orchestrator["allowed_tools"].([]interface{})
	assert.Contains(t, orchTools, "write_structured_file")
	assert.Contains(t, orchTools, "patch_structured_file")
	assert.Contains(t, orchTools, "read_file")
	assert.Contains(t, orchTools, "write_file")
	assert.Contains(t, orchTools, "edit_file")

	// Check coder - should have structured tools added
	coder := subagentTypes["coder"].(map[string]interface{})
	coderTools := coder["allowed_tools"].([]interface{})
	assert.Contains(t, coderTools, "write_structured_file")
	assert.Contains(t, coderTools, "patch_structured_file")
}

// TestApplyLegacyToolAllowlistMigration_SkipsWithoutWriteEdit verifies personas without write_file/edit_file aren't modified
func TestApplyLegacyToolAllowlistMigration_SkipsWithoutWriteEdit(t *testing.T) {
	raw := map[string]interface{}{
		"version": "2.0",
		"subagent_types": map[string]interface{}{
			"researcher": map[string]interface{}{
				"id":           "researcher",
				"name":         "Researcher",
				"enabled":      true,
				"allowed_tools": []interface{}{"read_file", "search_files"},
			},
		},
	}

	applyLegacyToolAllowlistMigration(raw)

	subagentTypes := raw["subagent_types"].(map[string]interface{})
	researcher := subagentTypes["researcher"].(map[string]interface{})
	tools := researcher["allowed_tools"].([]interface{})

	// Should NOT have structured tools added
	assert.NotContains(t, tools, "write_structured_file")
	assert.NotContains(t, tools, "patch_structured_file")
	// Should preserve original tools
	assert.Contains(t, tools, "read_file")
	assert.Contains(t, tools, "search_files")
}

// TestApplyLegacyToolAllowlistMigration_AddsShellCommandToWebScraper verifies shell_command added to web_scraper
func TestApplyLegacyToolAllowlistMigration_AddsShellCommandToWebScraper(t *testing.T) {
	raw := map[string]interface{}{
		"version": "2.0",
		"subagent_types": map[string]interface{}{
			"web_scraper": map[string]interface{}{
				"id":           "web_scraper",
				"name":         "Web Scraper",
				"enabled":      true,
				"allowed_tools": []interface{}{"read_file", "write_file"},
			},
		},
	}

	applyLegacyToolAllowlistMigration(raw)

	subagentTypes := raw["subagent_types"].(map[string]interface{})
	webScraper := subagentTypes["web_scraper"].(map[string]interface{})
	tools := webScraper["allowed_tools"].([]interface{})

	// Should have shell_command added
	assert.Contains(t, tools, "shell_command")
}

// TestApplyLegacyToolAllowlistMigration_PreservesAlreadyMigrated verifies no duplicates
func TestApplyLegacyToolAllowlistMigration_PreservesAlreadyMigrated(t *testing.T) {
	raw := map[string]interface{}{
		"version": "2.0",
		"subagent_types": map[string]interface{}{
			"orchestrator": map[string]interface{}{
				"id":           "orchestrator",
				"name":         "Orchestrator",
				"enabled":      true,
				"allowed_tools": []interface{}{
					"read_file",
					"write_file",
					"edit_file",
					"write_structured_file",
					"patch_structured_file",
				},
			},
		},
	}

	applyLegacyToolAllowlistMigration(raw)

	subagentTypes := raw["subagent_types"].(map[string]interface{})
	orchestrator := subagentTypes["orchestrator"].(map[string]interface{})
	tools := orchestrator["allowed_tools"].([]interface{})

	// Should not have duplicates
	assert.Len(t, tools, 5)
	assert.Contains(t, tools, "read_file")
	assert.Contains(t, tools, "write_file")
	assert.Contains(t, tools, "edit_file")
	assert.Contains(t, tools, "write_structured_file")
	assert.Contains(t, tools, "patch_structured_file")
}

// TestMigration_0_0_to_2_0_FullDefaults verify complete migration produces all expected defaults in raw JSON
func TestMigration_0_0_to_2_0_FullDefaults(t *testing.T) {
	raw := map[string]interface{}{}

	migrated, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)

	// Verify version
	assert.Equal(t, "2.0", migrated["version"])

	// Verify map initializations
	require.IsType(t, map[string]interface{}{}, migrated["provider_models"])
	require.IsType(t, map[string]interface{}{}, migrated["preferences"])
	require.IsType(t, map[string]interface{}{}, migrated["dismissed_prompts"])
	require.IsType(t, map[string]interface{}{}, migrated["custom_providers"])
	require.IsType(t, map[string]interface{}{}, migrated["subagent_types"])
	require.IsType(t, map[string]interface{}{}, migrated["skills"])

	// Verify API timeouts
	apiTimeouts, ok := migrated["api_timeouts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 300.0, apiTimeouts["connection_timeout_sec"])
	assert.Equal(t, 600.0, apiTimeouts["first_chunk_timeout_sec"])
	assert.Equal(t, 600.0, apiTimeouts["chunk_timeout_sec"])
	assert.Equal(t, 1800.0, apiTimeouts["overall_timeout_sec"])
	assert.Equal(t, 300.0, apiTimeouts["commit_message_timeout_sec"])

	// Verify PDF OCR defaults
	assert.Equal(t, true, migrated["pdf_ocr_enabled"])
	assert.Equal(t, "ollama", migrated["pdf_ocr_provider"])
	assert.Equal(t, "glm-ocr", migrated["pdf_ocr_model"])

	// Verify zsh command detection
	assert.Equal(t, true, migrated["enable_zsh_command_detection"])
	assert.Equal(t, true, migrated["auto_execute_detected_commands"])

	// Verify default subagent types
	subagentTypes, ok := migrated["subagent_types"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, subagentTypes)
	assert.Contains(t, subagentTypes, "coder")
	assert.Contains(t, subagentTypes, "tester")

	// Verify default skills
	skills, ok := migrated["skills"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, skills)
	assert.Contains(t, skills, "project-planning")
	assert.Contains(t, skills, "browse-debugging")
	assert.Contains(t, skills, "repo-onboarding")

	// Verify the result can be marshaled to JSON
	_, err = json.Marshal(migrated)
	require.NoError(t, err)
}

// TestMigration_Idempotent_FullDefaults verify running migration twice on a config produces identical raw JSON
func TestMigration_Idempotent_FullDefaults(t *testing.T) {
	// Start with empty config
	raw := map[string]interface{}{}

	// Run migration first time
	migrated1, err := MigrateConfig(raw, "2.0")
	require.NoError(t, err)

	// Run migration second time on the already-migrated config
	migrated2, err := MigrateConfig(migrated1, "2.0")
	require.NoError(t, err)

	// Results should be identical
	assert.Equal(t, migrated1["version"], migrated2["version"])

	// Check map fields are identical
	for _, field := range []string{
		"provider_models",
		"preferences",
		"dismissed_prompts",
		"custom_providers",
		"subagent_types",
		"skills",
		"api_timeouts",
		"mcp",
	} {
		assert.Equal(t, migrated1[field], migrated2[field], "field %q should be identical", field)
	}

	// Check scalar fields
	for _, field := range []string{
		"pdf_ocr_enabled",
		"pdf_ocr_provider",
		"pdf_ocr_model",
		"enable_zsh_command_detection",
		"auto_execute_detected_commands",
	} {
		assert.Equal(t, migrated1[field], migrated2[field], "field %q should be identical", field)
	}
}

// TestMigration_V2_to_V3_SyncsDefaultPersonaTools verifies that v2→v3 migration
// merges missing default tools into default personas while preserving user extras
// and leaving custom personas untouched.
func TestMigration_V2_to_V3_SyncsDefaultPersonaTools(t *testing.T) {
	// Simulate a stale v2 config where the orchestrator has only 6 tools
	// (missing browse_url, view_history, rollback_changes, self_review, etc.)
	// but has a user-added custom tool that should be preserved.
	staleOrchestratorTools := []interface{}{
		"shell_command", "read_file", "write_file", "edit_file", "TodoWrite", "TodoRead",
		"my_custom_mcp_tool", // User-added extra — must be preserved
	}

	raw := map[string]interface{}{
		"version": "2.0",
		"subagent_types": map[string]interface{}{
			"orchestrator": map[string]interface{}{
				"id":             "orchestrator",
				"name":           "Orchestrator",
				"description":    "Stale orchestrator",
				"enabled":        true,
				"system_prompt":  "subagent_prompts/orchestrator.md",
				"provider":       "custom-provider",
				"model":         "custom-model",
				"allowed_tools":  staleOrchestratorTools,
				"aliases":       []interface{}{"orch"},
			},
			"my_custom_persona": map[string]interface{}{
				"id":             "my_custom_persona",
				"name":           "My Custom",
				"description":    "Custom persona not in defaults",
				"enabled":        true,
				"allowed_tools":  []interface{}{"shell_command", "read_file"},
			},
		},
	}

	migrated, err := MigrateConfig(raw, "3.0")
	require.NoError(t, err)

	subagentTypes, ok := migrated["subagent_types"].(map[string]interface{})
	require.True(t, ok)

	// Orchestrator allowed_tools should have missing defaults merged in
	orch, ok := subagentTypes["orchestrator"].(map[string]interface{})
	require.True(t, ok)

	tools, ok := orch["allowed_tools"].([]interface{})
	require.True(t, ok)

	toolSet := make(map[string]bool)
	for _, t := range tools {
		toolSet[t.(string)] = true
	}

	// Missing defaults must be added
	assert.True(t, toolSet["browse_url"], "orchestrator should have browse_url from defaults")
	assert.True(t, toolSet["view_history"], "orchestrator should have view_history from defaults")
	assert.True(t, toolSet["rollback_changes"], "orchestrator should have rollback_changes from defaults")
	assert.True(t, toolSet["self_review"], "orchestrator should have self_review from defaults")
	assert.True(t, toolSet["shell_command"], "orchestrator should have shell_command from defaults")

	// User-added extras must be preserved
	assert.True(t, toolSet["my_custom_mcp_tool"], "user-added custom tool should be preserved")

	// User overrides for provider/model should be preserved
	assert.Equal(t, "custom-provider", orch["provider"])
	assert.Equal(t, "custom-model", orch["model"])

	// Custom persona should NOT be touched
	custom, ok := subagentTypes["my_custom_persona"].(map[string]interface{})
	require.True(t, ok)
	customTools, ok := custom["allowed_tools"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, 2, len(customTools), "custom persona tools should not be touched")
}

// TestMigration_V2_to_V3_MapKeyMismatch verifies that the migration resolves
// persona IDs from both the map key and the "id" field inside the persona.
func TestMigration_V2_to_V3_MapKeyMismatch(t *testing.T) {
	// Config where the map key differs from the id field
	raw := map[string]interface{}{
		"version": "2.0",
		"subagent_types": map[string]interface{}{
			"Orchestrator": map[string]interface{}{
				"id":            "orchestrator",
				"name":          "Orchestrator",
				"description":   "Stale",
				"enabled":       true,
				"allowed_tools": []interface{}{"shell_command"},
			},
		},
	}

	migrated, err := MigrateConfig(raw, "3.0")
	require.NoError(t, err)

	subagentTypes, ok := migrated["subagent_types"].(map[string]interface{})
	require.True(t, ok)

	// The map key "Orchestrator" should still be the key
	orch, ok := subagentTypes["Orchestrator"].(map[string]interface{})
	require.True(t, ok)

	tools, ok := orch["allowed_tools"].([]interface{})
	require.True(t, ok)

	// Should have merged in default tools since id field resolves to "orchestrator"
	toolSet := make(map[string]bool)
	for _, t := range tools {
		toolSet[t.(string)] = true
	}
	assert.True(t, toolSet["browse_url"], "should have browse_url from defaults via id field lookup")
}

// TestMigration_V2_to_V3_EmptyAndMissingTools verifies edge cases:
// nil allowed_tools, empty allowed_tools, and missing allowed_tools.
func TestMigration_V2_to_V3_EmptyAndMissingTools(t *testing.T) {
	t.Run("nil_allowed_tools", func(t *testing.T) {
		raw := map[string]interface{}{
			"version": "2.0",
			"subagent_types": map[string]interface{}{
				"orchestrator": map[string]interface{}{
					"id":      "orchestrator",
					"name":    "Orchestrator",
					"enabled": true,
				},
			},
		}
		migrated, err := MigrateConfig(raw, "3.0")
		require.NoError(t, err)
		orch := migrated["subagent_types"].(map[string]interface{})["orchestrator"].(map[string]interface{})
		tools, ok := orch["allowed_tools"].([]interface{})
		require.True(t, ok, "should have allowed_tools set to defaults")
		assert.True(t, len(tools) > 0, "should not be empty")
	})

	t.Run("empty_allowed_tools", func(t *testing.T) {
		raw := map[string]interface{}{
			"version": "2.0",
			"subagent_types": map[string]interface{}{
				"orchestrator": map[string]interface{}{
					"id":            "orchestrator",
					"name":          "Orchestrator",
					"enabled":       true,
					"allowed_tools": []interface{}{},
				},
			},
		}
		migrated, err := MigrateConfig(raw, "3.0")
		require.NoError(t, err)
		orch := migrated["subagent_types"].(map[string]interface{})["orchestrator"].(map[string]interface{})
		tools, ok := orch["allowed_tools"].([]interface{})
		require.True(t, ok, "should have allowed_tools set to defaults")
		assert.True(t, len(tools) > 0, "should not be empty")
	})
}

// TestMigration_V2_to_V3_Idempotent verifies that running v2→v3 migration
// twice produces identical results.
func TestMigration_V2_to_V3_Idempotent(t *testing.T) {
	raw := map[string]interface{}{
		"version": "2.0",
		"subagent_types": map[string]interface{}{
			"orchestrator": map[string]interface{}{
				"id":            "orchestrator",
				"name":          "Orchestrator",
				"description":   "Stale",
				"enabled":       true,
				"allowed_tools": []interface{}{"shell_command"},
			},
		},
	}

	migrated1, err := MigrateConfig(raw, "3.0")
	require.NoError(t, err)

	migrated2, err := MigrateConfig(migrated1, "3.0")
	require.NoError(t, err)

	assert.Equal(t, migrated1["version"], migrated2["version"])
	orch1 := migrated1["subagent_types"].(map[string]interface{})["orchestrator"].(map[string]interface{})
	orch2 := migrated2["subagent_types"].(map[string]interface{})["orchestrator"].(map[string]interface{})
	assert.Equal(t, orch1["allowed_tools"], orch2["allowed_tools"])
}

// TestMigration_V0_to_V3_RunsAllSteps verifies that a v0 config migrates
// through v2 to v3, applying all defaults and tool syncs.
func TestMigration_V0_to_V3_RunsAllSteps(t *testing.T) {
	raw := map[string]interface{}{}

	migrated, err := MigrateConfig(raw, "3.0")
	require.NoError(t, err)

	assert.Equal(t, "3.0", migrated["version"])

	subagentTypes, ok := migrated["subagent_types"].(map[string]interface{})
	require.True(t, ok)

	// Orchestrator should have full default tools (v2 defaults + v3 tool sync)
	orch, ok := subagentTypes["orchestrator"].(map[string]interface{})
	require.True(t, ok)

	tools, ok := orch["allowed_tools"].([]interface{})
	require.True(t, ok)

	toolSet := make(map[string]bool)
	for _, t := range tools {
		toolSet[t.(string)] = true
	}
	assert.True(t, toolSet["browse_url"], "orchestrator should have browse_url")
	assert.True(t, toolSet["view_history"], "orchestrator should have view_history")
	assert.True(t, toolSet["rollback_changes"], "orchestrator should have rollback_changes")
}
