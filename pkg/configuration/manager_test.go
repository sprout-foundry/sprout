package configuration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveConfig_AppliesDeletionAndScalarUpdates(t *testing.T) {
	// Set CI mode and HOME before creating managers
	t.Setenv("CI", "1")
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	// Clear SPROUT_CONFIG/SPROUT_CONFIG — if either is set in the test
	// runner's environment, GetConfigDir() uses it before HOME, breaking
	// the test's hermetic homeDir.
	t.Setenv("SPROUT_CONFIG", "")

	m1, err := NewManager()
	if err != nil {
		t.Fatalf("new manager 1: %v", err)
	}

	m2, err := NewManager()
	if err != nil {
		t.Fatalf("new manager 2: %v", err)
	}

	if err := m1.UpdateConfig(func(cfg *Config) error {
		cfg.ProviderPriority = []string{"openrouter", "deepinfra"}
		cfg.ResourceDirectory = "resources-a"
		return nil
	}); err != nil {
		t.Fatalf("save manager 1 config: %v", err)
	}

	// Stale manager updates one scalar and clears provider priority (deletion/change).
	cfg2 := m2.GetConfig()
	t.Logf("m2 config before: ResourceDirectory=%q, ProviderPriority=%v", cfg2.ResourceDirectory, cfg2.ProviderPriority)
	if err := m2.UpdateConfig(func(cfg *Config) error {
		cfg.ResourceDirectory = "resources-b"
		cfg.ProviderPriority = nil
		return nil
	}); err != nil {
		t.Fatalf("save manager 2 config: %v", err)
	}
	cfg2 = m2.GetConfig()
	t.Logf("m2 config after: ResourceDirectory=%q, ProviderPriority=%v", cfg2.ResourceDirectory, cfg2.ProviderPriority)

	loaded, err := Load()
	if err != nil {
		t.Fatalf("reload merged config: %v", err)
	}
	t.Logf("loaded config: ResourceDirectory=%q, ProviderPriority=%v", loaded.ResourceDirectory, loaded.ProviderPriority)
	if loaded.ResourceDirectory != "resources-b" {
		t.Fatalf("expected latest scalar from manager2, got %q", loaded.ResourceDirectory)
	}
	if len(loaded.ProviderPriority) != 0 {
		t.Fatalf("expected provider_priority to be cleared, got %#v", loaded.ProviderPriority)
	}
}

// TestManager_RefreshAPIKeys tests that RefreshAPIKeys updates the in-memory cache
func TestManager_RefreshAPIKeys(t *testing.T) {
	// Set up a test environment
	t.Setenv("CI", "1")
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SPROUT_CONFIG", "")

	// Create a manager
	m, err := NewManager()
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Use a key that won't collide with anything already in the backend
	testKey := "test-key-refresh-" + t.Name()

	// Set the key directly in the backend
	if err := credentials.SetToActiveBackend("test", testKey); err != nil {
		t.Fatalf("failed to set test key: %v", err)
	}

	// Verify the backend has the new key
	backendValue, _, err := credentials.GetFromActiveBackend("test")
	if err != nil {
		t.Fatalf("failed to get backend key: %v", err)
	}
	if backendValue != testKey {
		t.Fatalf("backend key mismatch: expected %q, got %q", testKey, backendValue)
	}

	// Manager's in-memory cache may or may not have the key yet.
	// Set a DIFFERENT key in the backend to prove RefreshAPIKeys picks up the change.
	differentKey := testKey + "-updated"
	if err := credentials.SetToActiveBackend("test", differentKey); err != nil {
		t.Fatalf("failed to set different key: %v", err)
	}

	// Refresh the API keys
	if err := m.RefreshAPIKeys(); err != nil {
		t.Fatalf("RefreshAPIKeys failed: %v", err)
	}

	// Verify Manager's in-memory cache now has the latest key from the backend
	managerValue := m.GetAPIKeyForProvider(api.TestClientType)
	if managerValue != differentKey {
		t.Errorf("Manager cache not refreshed: expected %q, got %q", differentKey, managerValue)
	}

	// Clean up
	_ = credentials.DeleteFromActiveBackend("test")
}

func TestCustomProviderPerModelContextSizes(t *testing.T) {
	t.Run("per-model context sizes are preserved", func(t *testing.T) {
		cfg := CustomProviderConfig{
			Name:        "test-provider",
			Endpoint:    "https://api.example.com/v1",
			ModelName:   "test-model",
			ContextSize: 32768,
			ModelContextSizes: map[string]int{
				"small-model": 8192,
				"large-model": 131072,
				"ultra-model": 2097152,
			},
		}

		normalized, err := NormalizeCustomProviderConfig(cfg)
		require.NoError(t, err)
		assert.Equal(t, 32768, normalized.ContextSize)
		assert.Equal(t, 3, len(normalized.ModelContextSizes))
		assert.Equal(t, 8192, normalized.ModelContextSizes["small-model"])
		assert.Equal(t, 131072, normalized.ModelContextSizes["large-model"])
		assert.Equal(t, 2097152, normalized.ModelContextSizes["ultra-model"])
	})

	t.Run("empty model context sizes map is initialized", func(t *testing.T) {
		cfg := CustomProviderConfig{
			Name:      "test-provider",
			Endpoint:  "https://api.example.com/v1",
			ModelName: "test-model",
		}

		normalized, err := NormalizeCustomProviderConfig(cfg)
		require.NoError(t, err)
		assert.NotNil(t, normalized.ModelContextSizes)
		assert.Equal(t, 0, len(normalized.ModelContextSizes))
	})

	t.Run("nil model context sizes map is initialized", func(t *testing.T) {
		cfg := CustomProviderConfig{
			Name:              "test-provider",
			Endpoint:          "https://api.example.com/v1",
			ModelName:         "test-model",
			ModelContextSizes: nil,
		}

		normalized, err := NormalizeCustomProviderConfig(cfg)
		require.NoError(t, err)
		assert.NotNil(t, normalized.ModelContextSizes)
		assert.Equal(t, 0, len(normalized.ModelContextSizes))
	})

	t.Run("context size defaults to 32768 when not set", func(t *testing.T) {
		cfg := CustomProviderConfig{
			Name:        "test-provider",
			Endpoint:    "https://api.example.com/v1",
			ModelName:   "test-model",
			ContextSize: 0,
		}

		normalized, err := NormalizeCustomProviderConfig(cfg)
		require.NoError(t, err)
		assert.Equal(t, 32768, normalized.ContextSize)
	})

	t.Run("toProviderConfig converts model context sizes to overrides", func(t *testing.T) {
		cfg := CustomProviderConfig{
			Name:        "test-provider",
			Endpoint:    "https://api.example.com/v1",
			ModelName:   "test-model",
			ContextSize: 32768,
			ModelContextSizes: map[string]int{
				"small-model": 8192,
				"large-model": 131072,
			},
		}

		providerConfig, err := cfg.ToProviderConfig()
		require.NoError(t, err)
		assert.Equal(t, 32768, providerConfig.Models.DefaultContextLimit)
		assert.Equal(t, 2, len(providerConfig.Models.ModelOverrides))
		assert.Equal(t, 8192, providerConfig.Models.ModelOverrides["small-model"])
		assert.Equal(t, 131072, providerConfig.Models.ModelOverrides["large-model"])
	})

	t.Run("toProviderConfig only includes positive context sizes", func(t *testing.T) {
		cfg := CustomProviderConfig{
			Name:        "test-provider",
			Endpoint:    "https://api.example.com/v1",
			ModelName:   "test-model",
			ContextSize: 32768,
			ModelContextSizes: map[string]int{
				"zero-model":  0,
				"negative":    -1,
				"valid-model": 65536,
			},
		}

		providerConfig, err := cfg.ToProviderConfig()
		require.NoError(t, err)
		assert.Equal(t, 1, len(providerConfig.Models.ModelOverrides))
		assert.Equal(t, 65536, providerConfig.Models.ModelOverrides["valid-model"])
		_, exists := providerConfig.Models.ModelOverrides["zero-model"]
		assert.False(t, exists)
		_, exists = providerConfig.Models.ModelOverrides["negative"]
		assert.False(t, exists)
	})

	t.Run("save and load preserves model context sizes", func(t *testing.T) {
		cfg := CustomProviderConfig{
			Name:        "test-provider",
			Endpoint:    "https://api.example.com/v1",
			ModelName:   "test-model",
			ContextSize: 32768,
			ModelContextSizes: map[string]int{
				"small-model": 8192,
				"large-model": 131072,
			},
		}

		normalized, err := NormalizeCustomProviderConfig(cfg)
		require.NoError(t, err)

		// Use a test-specific path
		testPath := filepath.Join(t.TempDir(), "test-custom-provider.json")
		data, err := json.MarshalIndent(normalized, "", "  ")
		require.NoError(t, err)

		err = os.WriteFile(testPath, data, 0600)
		require.NoError(t, err)

		// Load from temp directory
		loadedCfg, err := LoadCustomProviderFile(testPath)
		require.NoError(t, err)
		assert.Equal(t, cfg.Name, loadedCfg.Name)
		assert.Equal(t, cfg.ContextSize, loadedCfg.ContextSize)
		assert.Equal(t, 2, len(loadedCfg.ModelContextSizes))
		assert.Equal(t, 8192, loadedCfg.ModelContextSizes["small-model"])
		assert.Equal(t, 131072, loadedCfg.ModelContextSizes["large-model"])
	})
}

// LoadCustomProviderFile loads a custom provider from a specific file path
func LoadCustomProviderFile(path string) (CustomProviderConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CustomProviderConfig{}, fmt.Errorf("failed to read custom provider file %s: %w", path, err)
	}

	var cfg CustomProviderConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return CustomProviderConfig{}, fmt.Errorf("failed to parse custom provider file %s: %w", path, err)
	}

	cfg, err = NormalizeCustomProviderConfig(cfg)
	if err != nil {
		return CustomProviderConfig{}, fmt.Errorf("failed to normalize custom provider config from %s: %w", path, err)
	}
	return cfg, nil
}

func TestValidateCustomProviderEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid https", "https://api.example.com/v1", false},
		{"valid http", "http://localhost:11434/v1", false},
		{"valid with port", "https://api.openai.com:443/v1", false},
		{"valid models path", "https://api.example.com/v1/models", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"no scheme", "api.example.com/v1", true},
		{"ftp scheme", "ftp://example.com/v1", true},
		{"file scheme", "file:///etc/passwd", true},
		{"missing host", "https:///v1", true},
		{"missing host no slash", "https://", true},
		{"garbage", "://not a url", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCustomProviderEndpoint(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("ValidateCustomProviderEndpoint(%q) expected error, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidateCustomProviderEndpoint(%q) unexpected error: %v", tc.input, err)
			}
		})
	}
}

func TestValidateCustomProviderEndpoint_ErrorMentionsScheme(t *testing.T) {
	err := ValidateCustomProviderEndpoint("ftp://example.com")
	if err == nil {
		t.Fatal("expected error for ftp scheme")
	}
	if !strings.Contains(err.Error(), "scheme") && !strings.Contains(err.Error(), "http") {
		t.Errorf("error should mention scheme/http, got: %v", err)
	}
}

func TestValidateCustomProviderEndpoint_ErrorMentionsHost(t *testing.T) {
	err := ValidateCustomProviderEndpoint("https://")
	if err == nil {
		t.Fatal("expected error for missing host")
	}
	if !strings.Contains(err.Error(), "host") {
		t.Errorf("error should mention host, got: %v", err)
	}
}
