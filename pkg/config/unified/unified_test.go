package unified

import (
	"os"
	"testing"

	agent_config "github.com/alantheprice/ledit/pkg/agent_config"
)

func TestUnifiedConfig(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "unified-config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set test environment
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tempDir)

	t.Run("CreateUnifiedConfig", func(t *testing.T) {
		unified, err := NewUnifiedConfig()
		if err != nil {
			t.Fatalf("Failed to create unified config: %v", err)
		}

		if unified == nil {
			t.Fatal("Unified config is nil")
		}

		// Test basic functionality
		mainConfig := unified.GetMainConfig()
		if mainConfig == nil {
			t.Error("Main config is nil")
		}

		agentConfig := unified.GetAgentConfig()
		if agentConfig == nil {
			t.Error("Agent config is nil")
		}
	})

	t.Run("APIKeyManagement", func(t *testing.T) {
		unified, err := NewUnifiedConfig()
		if err != nil {
			t.Fatalf("Failed to create unified config: %v", err)
		}

		// Test setting and getting API key
		testKey := "test-key-123"
		err = unified.SetAPIKey(agent_config.OpenAIClientType, testKey)
		if err != nil {
			t.Fatalf("Failed to set API key: %v", err)
		}

		retrievedKey, err := unified.GetAPIKey(agent_config.OpenAIClientType)
		if err != nil {
			t.Fatalf("Failed to get API key: %v", err)
		}

		if retrievedKey != testKey {
			t.Errorf("Expected API key %s, got %s", testKey, retrievedKey)
		}
	})

	t.Run("ProviderManagement", func(t *testing.T) {
		unified, err := NewUnifiedConfig()
		if err != nil {
			t.Fatalf("Failed to create unified config: %v", err)
		}

		// Test setting and getting model
		testModel := "test-model"
		unified.SetModelForProvider(agent_config.OpenAIClientType, testModel)

		retrievedModel := unified.GetModelForProvider(agent_config.OpenAIClientType)
		if retrievedModel != testModel {
			t.Errorf("Expected model %s, got %s", testModel, retrievedModel)
		}

		// Test setting and getting last used provider
		unified.SetLastUsedProvider(agent_config.DeepInfraClientType)
		lastProvider := unified.GetLastUsedProvider()
		if lastProvider != agent_config.DeepInfraClientType {
			t.Errorf("Expected last provider %v, got %v", agent_config.DeepInfraClientType, lastProvider)
		}
	})

	t.Run("Validation", func(t *testing.T) {
		unified, err := NewUnifiedConfig()
		if err != nil {
			t.Fatalf("Failed to create unified config: %v", err)
		}

		err = unified.Validate()
		if err != nil {
			t.Errorf("Validation failed: %v", err)
		}
	})

	t.Run("LegacyConfigDetection", func(t *testing.T) {
		unified, err := NewUnifiedConfig()
		if err != nil {
			t.Fatalf("Failed to create unified config: %v", err)
		}

		// Since we're in a clean temp directory, legacy config shouldn't exist
		isPresent := unified.IsLegacyConfigPresent()
		t.Logf("Legacy config present: %v", isPresent)
	})
}