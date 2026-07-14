package configuration

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

const (
	apiKeysMigratedMarker       = ".api_keys_migrated"
	configApiKeysMigratedMarker = ".config_api_keys_migrated"
)

// MigrateLegacyCustomProviders copies any custom_providers entries that lived
// inline in config.json into the new file-per-provider format under
// ~/.config/sprout/providers/. Existing files win — the inline entry is only
// promoted when no file with the same name already exists.
func MigrateLegacyCustomProviders(cfg *Config) (map[string]CustomProviderConfig, error) {
	if cfg == nil || len(cfg.CustomProviders) == 0 {
		return LoadCustomProviders()
	}

	fileProviders, err := LoadCustomProviders()
	if err != nil {
		return nil, fmt.Errorf("load custom providers: %w", err)
	}

	for name, provider := range cfg.CustomProviders {
		if _, exists := fileProviders[name]; exists {
			continue
		}
		legacy := provider
		if legacy.Name == "" {
			legacy.Name = name
		}
		if err := SaveCustomProvider(legacy); err != nil {
			return nil, fmt.Errorf("failed to migrate custom provider %s: %w", name, err)
		}
		fileProviders[name] = legacy
	}

	return fileProviders, nil
}

// MigrateEmbeddedAPIKeys moves any api_key values found in custom provider JSON files
// into the unified credential store, then strips the key from the file.
// Called on every Load() but exits immediately if the migration marker exists.
func MigrateEmbeddedAPIKeys(providers map[string]CustomProviderConfig) error {
	// Check if migration has already been completed
	providersDir, err := GetProvidersDir()
	if err != nil {
		return err
	}
	markerPath := filepath.Join(providersDir, apiKeysMigratedMarker)
	if _, err := os.Stat(markerPath); err == nil {
		// Marker exists, migration already completed
		return nil
	}

	// Even with zero providers in memory, scan the providers directory for any
	// legacy JSON files that may contain api_key values, then create the marker.
	files, err := filepath.Glob(filepath.Join(providersDir, "*.json"))
	if err != nil {
		return err
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var raw struct {
			Name   string `json:"name"`
			APIKey string `json:"api_key"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		if strings.TrimSpace(raw.APIKey) == "" {
			continue
		}
		providerName := raw.Name
		if providerName == "" {
			providerName = strings.TrimSuffix(filepath.Base(path), ".json")
		}
		if err := credentials.SetToActiveBackend(providerName, strings.TrimSpace(raw.APIKey)); err != nil {
			log.Printf("[migration] failed to migrate api_key for %s to credential store: %v", providerName, err)
			continue
		}
		var cfgMap map[string]interface{}
		if err := json.Unmarshal(data, &cfgMap); err != nil {
			continue
		}
		delete(cfgMap, "api_key")
		cleaned, err := json.MarshalIndent(cfgMap, "", "  ")
		if err != nil {
			continue
		}
		if err := os.WriteFile(path, cleaned, 0600); err != nil {
			log.Printf("[migration] failed to clean api_key from %s: %v", path, err)
			continue
		}
		log.Printf("[migration] migrated api_key for provider %q to credential store", providerName)
	}
	// Migration complete - create marker file to prevent re-running
	if err := os.WriteFile(markerPath, nil, 0600); err != nil {
		log.Printf("[migration] failed to create marker file: %v", err)
	}
	return nil
}

// MigrateConfigFileAPIKeys migrates any api_key values found in config.json's
// custom_providers entries into the unified credential store, then strips the keys
// from the config file. Called on every Load() but exits immediately if the
// migration marker exists.
//
// This is necessary because the CustomProviderConfig struct no longer has an APIKey
// field, so json.Unmarshal would silently drop these values.
func MigrateConfigFileAPIKeys(configPath string) error {
	// Extract config directory from config path
	configDir := filepath.Dir(configPath)
	markerPath := filepath.Join(configDir, configApiKeysMigratedMarker)

	// Check if migration has already been completed
	if _, err := os.Stat(markerPath); err == nil {
		// Marker exists, migration already completed
		return nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var rawConfig map[string]interface{}
	if err := json.Unmarshal(data, &rawConfig); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Check for custom_providers map
	customProviders, ok := rawConfig["custom_providers"].(map[string]interface{})
	if !ok || len(customProviders) == 0 {
		// No custom providers or not a map, still create marker to indicate completion
		return os.WriteFile(markerPath, nil, 0600)
	}

	// Track if we need to write changes
	needsWrite := false

	// Iterate through each provider entry
	for providerName, providerData := range customProviders {
		providerMap, ok := providerData.(map[string]interface{})
		if !ok {
			continue // Not a valid provider entry
		}

		// Try to get the name from the provider's name field first
		nameFromField, hasNameField := providerMap["name"].(string)
		if hasNameField && strings.TrimSpace(nameFromField) != "" {
			providerName = nameFromField
		}

		// Check for api_key field
		apiKey, hasAPIKey := providerMap["api_key"].(string)
		if !hasAPIKey {
			continue // No api_key in this provider
		}

		// Skip if api_key is empty after trimming
		apiKey = strings.TrimSpace(apiKey)
		if apiKey == "" {
			continue
		}

		// Migrate to credential store
		if err := credentials.SetToActiveBackend(providerName, apiKey); err != nil {
			log.Printf("[migration] failed to migrate api_key for config-embedded provider %q: %v", providerName, err)
			continue
		}
		log.Printf("[migration] migrated api_key for config-embedded provider %q to credential store", providerName)

		// Remove api_key from the map
		delete(providerMap, "api_key")
		needsWrite = true
	}

	// Only write if we found and migrated at least one api_key
	if needsWrite {
		cleaned, err := json.MarshalIndent(rawConfig, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal cleaned config: %w", err)
		}
		if err := os.WriteFile(configPath, cleaned, 0600); err != nil {
			return fmt.Errorf("failed to write cleaned config: %w", err)
		}
	}

	// Migration complete - create marker file to prevent re-running
	if err := os.WriteFile(markerPath, nil, 0600); err != nil {
		log.Printf("[migration] failed to create config marker file: %v", err)
	}
	return nil
}
