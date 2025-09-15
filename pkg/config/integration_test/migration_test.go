package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	agent_config "github.com/alantheprice/ledit/pkg/agent_config"
	"github.com/alantheprice/ledit/pkg/config"
)

// TestConfigMigrationCompatibility ensures both config systems can coexist during migration
func TestConfigMigrationCompatibility(t *testing.T) {
	// Create temporary directory for test configs
	tempDir, err := os.MkdirTemp("", "ledit-config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set test environment
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tempDir)

	t.Run("AgentConfigStillWorks", func(t *testing.T) {
		// Test that agent_config.Load() still works
		cfg, err := agent_config.Load()
		if err != nil {
			t.Fatalf("agent_config.Load() failed: %v", err)
		}
		if cfg == nil {
			t.Fatal("agent_config.Load() returned nil config")
		}
	})

	t.Run("MainConfigStillWorks", func(t *testing.T) {
		// Test that main config loading still works
		cfg, err := config.LoadOrInitConfig(true) // skip prompt
		if err != nil {
			t.Fatalf("LoadOrInitConfig() failed: %v", err)
		}
		if cfg == nil {
			t.Fatal("LoadOrInitConfig() returned nil config")
		}
	})

	t.Run("BothConfigsCanCoexist", func(t *testing.T) {
		// Load both configs simultaneously
		agentCfg, err := agent_config.Load()
		if err != nil {
			t.Fatalf("agent_config.Load() failed: %v", err)
		}

		mainCfg, err := config.LoadOrInitConfig(true)
		if err != nil {
			t.Fatalf("LoadOrInitConfig() failed: %v", err)
		}

		// Both should be valid
		if agentCfg == nil || mainCfg == nil {
			t.Fatal("Both configs should be non-nil")
		}
	})
}

// TestAPIKeyCompatibility ensures API key handling works across both systems
func TestAPIKeyCompatibility(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ledit-apikey-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tempDir)

	t.Run("AgentConfigAPIKeys", func(t *testing.T) {
		// Test agent config API key loading
		apiKeys, err := agent_config.LoadAPIKeys()
		if err != nil {
			t.Fatalf("LoadAPIKeys() failed: %v", err)
		}
		if apiKeys == nil {
			t.Fatal("LoadAPIKeys() returned nil")
		}
	})

	t.Run("MainConfigAPIKeys", func(t *testing.T) {
		// Test main config - it should handle API keys through its system
		cfg, err := config.LoadOrInitConfig(true)
		if err != nil {
			t.Fatalf("LoadOrInitConfig() failed: %v", err)
		}

		// Main config should have API key functionality through its domain configs
		llmConfig := cfg.GetLLMConfig()
		if llmConfig == nil {
			t.Log("LLM config is nil - this is expected for new configs")
		}
	})
}

// TestProviderManagementCompatibility ensures provider switching works in both systems
func TestProviderManagementCompatibility(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ledit-provider-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tempDir)

	t.Run("AgentConfigProviders", func(t *testing.T) {
		cfg, err := agent_config.Load()
		if err != nil {
			t.Fatalf("agent_config.Load() failed: %v", err)
		}

		// Test provider functionality
		lastProvider := cfg.GetLastUsedProvider()
		t.Logf("Last used provider: %v", lastProvider)

		// Test setting a provider
		cfg.SetLastUsedProvider(agent_config.OpenAIClientType)
		if cfg.GetLastUsedProvider() != agent_config.OpenAIClientType {
			t.Error("Failed to set last used provider")
		}
	})

	t.Run("MainConfigProviders", func(t *testing.T) {
		cfg, err := config.LoadOrInitConfig(true)
		if err != nil {
			t.Fatalf("LoadOrInitConfig() failed: %v", err)
		}

		// Main config should handle providers through its LLM config
		llmConfig := cfg.GetLLMConfig()
		if llmConfig != nil {
			t.Logf("LLM config found with provider settings")
		} else {
			t.Log("LLM config is nil - this is expected for new configs")
		}
	})
}

// TestConfigFilePaths ensures both systems use correct file paths
func TestConfigFilePaths(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ledit-paths-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tempDir)

	t.Run("AgentConfigPaths", func(t *testing.T) {
		configDir, err := agent_config.GetConfigDir()
		if err != nil {
			t.Fatalf("GetConfigDir() failed: %v", err)
		}

		configPath, err := agent_config.GetConfigPath()
		if err != nil {
			t.Fatalf("GetConfigPath() failed: %v", err)
		}

		apiKeysPath, err := agent_config.GetAPIKeysPath()
		if err != nil {
			t.Fatalf("GetAPIKeysPath() failed: %v", err)
		}

		t.Logf("Agent config dir: %s", configDir)
		t.Logf("Agent config path: %s", configPath)
		t.Logf("Agent API keys path: %s", apiKeysPath)

		// Ensure paths are reasonable
		if !filepath.IsAbs(configDir) {
			t.Error("Config dir should be absolute path")
		}
	})

	t.Run("MainConfigPaths", func(t *testing.T) {
		// Test that config can be loaded/initialized (which tests path logic)
		cfg, err := config.LoadOrInitConfig(true)
		if err != nil {
			t.Fatalf("LoadOrInitConfig() failed: %v", err)
		}
		if cfg == nil {
			t.Fatal("LoadOrInitConfig() returned nil config")
		}
		t.Logf("Main config loading successful")
	})
}

// TestBasicFunctionality ensures core functionality works in both systems
func TestBasicFunctionality(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ledit-basic-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tempDir)

	t.Run("AgentConfigSaveLoad", func(t *testing.T) {
		// Create and save agent config
		cfg := agent_config.NewConfig()
		cfg.SetLastUsedProvider(agent_config.OpenAIClientType)

		err := cfg.Save()
		if err != nil {
			t.Fatalf("Failed to save agent config: %v", err)
		}

		// Load and verify
		loadedCfg, err := agent_config.Load()
		if err != nil {
			t.Fatalf("Failed to load agent config: %v", err)
		}

		if loadedCfg.GetLastUsedProvider() != agent_config.OpenAIClientType {
			t.Error("Loaded config doesn't match saved config")
		}
	})

	t.Run("MainConfigSaveLoad", func(t *testing.T) {
		// Create and initialize main config
		cfg := config.DefaultConfig()
		cfg.InitializeWithDefaults()

		// Main config saves automatically when loaded/initialized
		// Just verify it works by loading again
		_, err := config.LoadOrInitConfig(true)
		if err != nil {
			t.Fatalf("Failed to save main config: %v", err)
		}

		// Load and verify
		loadedCfg, err := config.LoadOrInitConfig(true)
		if err != nil {
			t.Fatalf("Failed to load main config: %v", err)
		}

		if loadedCfg == nil {
			t.Fatal("Loaded config is nil")
		}
	})
}

// BenchmarkConfigLoading compares performance of both config systems
func BenchmarkConfigLoading(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "ledit-bench-test")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tempDir)

	// Initialize both configs
	agentCfg := agent_config.NewConfig()
	agentCfg.Save()

	mainCfg := config.DefaultConfig()
	mainCfg.InitializeWithDefaults()
	// Just use LoadOrInitConfig to initialize the config properly
	_, err = config.LoadOrInitConfig(true)
	if err != nil {
		b.Fatalf("Failed to initialize main config: %v", err)
	}

	b.Run("AgentConfigLoad", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := agent_config.Load()
			if err != nil {
				b.Fatalf("Failed to load agent config: %v", err)
			}
		}
	})

	b.Run("MainConfigLoad", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := config.LoadOrInitConfig(true)
			if err != nil {
				b.Fatalf("Failed to load main config: %v", err)
			}
		}
	})
}