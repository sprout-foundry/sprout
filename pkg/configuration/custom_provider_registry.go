package configuration

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const ProvidersDirName = "providers"

type ProviderDiscoveryModel struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	ContextLength int      `json:"context_length,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// GetGlobalProvidersDir returns the global providers directory
// (~/.config/sprout/providers/). Custom providers are always stored here
// regardless of SPROUT_CONFIG — they are user-global resources, not
// project-scoped. This prevents a split-brain where a provider saved from
// a scoped session is invisible to another scope and can't be deleted
// from a different scope.
//
// Uses getDefaultConfigDir (HOME-based, ignores SPROUT_CONFIG) so the
// location is stable across all sessions.
func GetGlobalProvidersDir() (string, error) {
	configDir, err := getDefaultConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get global config directory: %w", err)
	}
	providersDir := filepath.Join(configDir, ProvidersDirName)
	if err := os.MkdirAll(providersDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create providers directory: %w", err)
	}
	return providersDir, nil
}

// GetProvidersDir returns the global providers directory. Kept as an alias
// for GetGlobalProvidersDir for backward compatibility with callers that
// display or reference the providers directory (e.g. cmd/diag.go).
func GetProvidersDir() (string, error) {
	return GetGlobalProvidersDir()
}

// GetCustomProviderPath returns the path where a custom provider JSON
// file is stored. Always resolves to the global providers directory
// (~/.config/sprout/providers/) so that save, load, and delete all
// agree on the same location regardless of SPROUT_CONFIG.
func GetCustomProviderPath(name string) (string, error) {
	providersDir, err := GetGlobalProvidersDir()
	if err != nil {
		return "", fmt.Errorf("failed to get providers directory: %w", err)
	}
	normalized, err := CanonicalizeCustomProviderName(name)
	if err != nil {
		return "", fmt.Errorf("failed to normalize provider name: %w", err)
	}
	return filepath.Join(providersDir, normalized+".json"), nil
}

// getScopedProvidersDir returns the SPROUT_CONFIG-resolved providers
// directory, if SPROUT_CONFIG is set. Returns ("", nil) when it is not
// set or differs from the global dir. Used by DeleteCustomProvider to
// clean up stale copies from before the global-only fix.
func getScopedProvidersDir() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	globalDir, err := getDefaultConfigDir()
	if err != nil {
		return "", err
	}
	if configDir == globalDir {
		return "", nil
	}
	return filepath.Join(configDir, ProvidersDirName), nil
}

// LoadCustomProviders loads all custom provider configs. It merges
// providers from the SPROUT_CONFIG-resolved directory and the global
// home directory (~/.config/sprout/providers/), with the global home
// directory winning on name conflicts.
//
// This matches the layered manager's effective behavior (which only
// reads from global) and ensures the factory's /provider switch path
// resolves the same providers the /provider listing shows. Without
// the merge, a user running sprout from a project workspace with
// SPROUT_CONFIG overridden sees custom providers listed (via the
// layered manager) but receives a "not registered as a custom
// provider" error when trying to switch to one (via LoadCustomProviders,
// which would otherwise only see the SPROUT_CONFIG-resolved dir).
func LoadCustomProviders() (map[string]CustomProviderConfig, error) {
	// Read from both the SPROUT_CONFIG-resolved dir and the global home
	// dir. Custom providers are always saved to the global dir now, but
	// stale copies may still exist in the scoped dir from before the fix.
	// Global wins on name conflicts.
	scopedDir, err := GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("get scoped config directory: %w", err)
	}
	scopedProvidersDir := filepath.Join(scopedDir, ProvidersDirName)
	globalDir, err := getDefaultConfigDir()
	if err != nil {
		return nil, fmt.Errorf("get default config directory: %w", err)
	}
	globalProvidersDir := filepath.Join(globalDir, ProvidersDirName)

	merged := make(map[string]CustomProviderConfig)

	// Read the SPROUT_CONFIG-scoped dir first so the global home dir
	// can override on conflict (matching the layered manager: global
	// is the source of truth for custom providers).
	if scopedProviders, scopedErr := LoadCustomProvidersFromDir(scopedProvidersDir); scopedErr != nil {
		log.Printf("[config] warning: failed to read scoped custom providers from %s: %v", scopedProvidersDir, scopedErr)
	} else {
		for name, provider := range scopedProviders {
			merged[name] = provider
		}
	}

	if globalProviders, globalErr := LoadCustomProvidersFromDir(globalProvidersDir); globalErr != nil {
		log.Printf("[config] warning: failed to read global custom providers from %s: %v", globalDir, globalErr)
	} else {
		for name, provider := range globalProviders {
			merged[name] = provider
		}
	}

	return merged, nil
}

// LoadCustomProvidersFromDir loads all custom provider JSON files from the
// given directory. Used by LoadCustomProviders for both the
// SPROUT_CONFIG-resolved dir and the global home dir (see the merge
// behavior in LoadCustomProviders for context).
func LoadCustomProvidersFromDir(providersDir string) (map[string]CustomProviderConfig, error) {

	files, err := filepath.Glob(filepath.Join(providersDir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to list custom provider files: %w", err)
	}

	result := make(map[string]CustomProviderConfig, len(files))
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read custom provider file %s: %w", path, err)
		}

		var cfg CustomProviderConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse custom provider file %s: %w", path, err)
		}

		cfg, err = NormalizeCustomProviderConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("invalid custom provider file %s: %w", path, err)
		}
		result[cfg.Name] = cfg
	}

	return result, nil
}

func SaveCustomProvider(cfg CustomProviderConfig) error {
	normalized, err := NormalizeCustomProviderConfig(cfg)
	if err != nil {
		return fmt.Errorf("normalize custom provider config: %w", err)
	}

	path, err := GetCustomProviderPath(normalized.Name)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal custom provider config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write custom provider config: %w", err)
	}

	return nil
}

func DeleteCustomProvider(name string) error {
	normalized, err := CanonicalizeCustomProviderName(name)
	if err != nil {
		return fmt.Errorf("normalize provider name: %w", err)
	}

	// Delete from the global dir (the canonical location after the
	// split-brain fix). This is where GetCustomProviderPath resolves to.
	globalPath, err := GetGlobalProvidersDir()
	if err != nil {
		return fmt.Errorf("get global providers directory: %w", err)
	}
	globalFile := filepath.Join(globalPath, normalized+".json")
	if err := os.Remove(globalFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove custom provider %s: %w", name, err)
	}

	// Also clean up any stale copy in the scoped dir (SPROUT_CONFIG-resolved).
	// Providers saved before this fix may still live there. A missing file
	// is expected and not an error.
	if scopedDir, scopedErr := getScopedProvidersDir(); scopedErr == nil {
		scopedFile := filepath.Join(scopedDir, normalized+".json")
		if err := os.Remove(scopedFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove scoped custom provider %s: %w", name, err)
		}
	}

	return nil
}

func DiscoverCustomProviderModels(cfg CustomProviderConfig) ([]ProviderDiscoveryModel, error) {
	normalized, err := NormalizeCustomProviderConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("normalize custom provider config: %w", err)
	}

	if normalized.Endpoint == "" {
		return nil, fmt.Errorf("endpoint URL cannot be empty")
	}

	url := strings.TrimSuffix(normalized.Endpoint, "/") + "/models"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build discovery request: %w", err)
	}
	if normalized.EnvVar != "" {
		if key := strings.TrimSpace(os.Getenv(normalized.EnvVar)); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("model discovery request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model discovery returned HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Data []struct {
			ID            string   `json:"id"`
			Name          string   `json:"name,omitempty"`
			Description   string   `json:"description,omitempty"`
			ContextLength int      `json:"context_length,omitempty"`
			Tags          []string `json:"tags,omitempty"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode discovery response: %w", err)
	}

	models := make([]ProviderDiscoveryModel, 0, len(payload.Data))
	for _, model := range payload.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		models = append(models, ProviderDiscoveryModel{
			ID:            id,
			Name:          strings.TrimSpace(model.Name),
			Description:   strings.TrimSpace(model.Description),
			ContextLength: model.ContextLength,
			Tags:          normalizeUniqueStrings(model.Tags),
		})
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	return models, nil
}
