package configuration

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomProviderPerModelContextSizes(t *testing.T) {
	t.Run("per-model context sizes are preserved", func(t *testing.T) {
		cfg := CustomProviderConfig{
			Name:      "test-provider",
			Endpoint:  "https://api.example.com/v1",
			ModelName: "test-model",
			ContextSize: 32768,
			ModelContextSizes: map[string]int{
				"small-model":  8192,
				"large-model":  131072,
				"ultra-model":  2097152,
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
			Name:      "test-provider",
			Endpoint:  "https://api.example.com/v1",
			ModelName: "test-model",
			ModelContextSizes: nil,
		}

		normalized, err := NormalizeCustomProviderConfig(cfg)
		require.NoError(t, err)
		assert.NotNil(t, normalized.ModelContextSizes)
		assert.Equal(t, 0, len(normalized.ModelContextSizes))
	})

	t.Run("context size defaults to 32768 when not set", func(t *testing.T) {
		cfg := CustomProviderConfig{
			Name:      "test-provider",
			Endpoint:  "https://api.example.com/v1",
			ModelName: "test-model",
			ContextSize: 0,
		}

		normalized, err := NormalizeCustomProviderConfig(cfg)
		require.NoError(t, err)
		assert.Equal(t, 32768, normalized.ContextSize)
	})

	t.Run("toProviderConfig converts model context sizes to overrides", func(t *testing.T) {
		cfg := CustomProviderConfig{
			Name:      "test-provider",
			Endpoint:  "https://api.example.com/v1",
			ModelName: "test-model",
			ContextSize: 32768,
			ModelContextSizes: map[string]int{
				"small-model":  8192,
				"large-model":  131072,
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
			Name:      "test-provider",
			Endpoint:  "https://api.example.com/v1",
			ModelName: "test-model",
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
			Name:      "test-provider",
			Endpoint:  "https://api.example.com/v1",
			ModelName: "test-model",
			ContextSize: 32768,
			ModelContextSizes: map[string]int{
				"small-model":  8192,
				"large-model":  131072,
			},
		}

		normalized, err := NormalizeCustomProviderConfig(cfg)
		require.NoError(t, err)

		// Use a test-specific path
		testPath := "/tmp/test-custom-provider.json"
		data, err := json.MarshalIndent(normalized, "", "  ")
		require.NoError(t, err)

		err = os.WriteFile(testPath, data, 0600)
		require.NoError(t, err)
		defer os.Remove(testPath)

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
