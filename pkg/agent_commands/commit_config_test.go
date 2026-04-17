package commands

import (
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/configuration"
)

// TestCommitCommandUsesConfiguredProvider tests that commit command uses configured provider
func TestCommitCommandUsesConfiguredProvider(t *testing.T) {
	// Create a test agent with temp config directory
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OPENROUTER_API_KEY", "test-key-for-unit-tests")

	chatAgent, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	// Set commit provider via configuration manager
	cm := chatAgent.GetConfigManager()
	err = cm.UpdateConfig(func(c *configuration.Config) error {
		c.CommitProvider = "zai"
		c.CommitModel = "GLM-4.6"
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	// Verify the configuration was set
	config := cm.GetConfig()
	if config.CommitProvider != "zai" {
		t.Errorf("expected commit provider to be 'zai', got %q", config.CommitProvider)
	}
	if config.CommitModel != "GLM-4.6" {
		t.Errorf("expected commit model to be 'GLM-4.6', got %q", config.CommitModel)
	}

	// Verify getter methods return the configured values
	if provider := config.GetCommitProvider(); provider != "zai" {
		t.Errorf("GetCommitProvider() returned %q, want 'zai'", provider)
	}
	if model := config.GetCommitModel(); model != "GLM-4.6" {
		t.Errorf("GetCommitModel() returned %q, want 'GLM-4.6'", model)
	}
}

// TestCommitCommandFallsBackToLastUsedProvider tests fallback behavior
func TestCommitCommandFallsBackToLastUsedProvider(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OPENROUTER_API_KEY", "test-key-for-unit-tests")

	chatAgent, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	cm := chatAgent.GetConfigManager()

	// Set LastUsedProvider but leave CommitProvider empty
	err = cm.UpdateConfig(func(c *configuration.Config) error {
		c.CommitProvider = ""
		c.CommitModel = ""
		c.LastUsedProvider = "openrouter"
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	config := cm.GetConfig()
	if provider := config.GetCommitProvider(); provider != "openrouter" {
		t.Errorf("GetCommitProvider() should fall back to LastUsedProvider, got %q", provider)
	}
}

// TestCommitCommandPersistsToDisk tests that config changes are persisted
func TestCommitCommandPersistsToDisk(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OPENROUTER_API_KEY", "test-key-for-unit-tests")

	// Create first agent and set config
	chatAgent1, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	cm1 := chatAgent1.GetConfigManager()
	err = cm1.UpdateConfig(func(c *configuration.Config) error {
		c.CommitProvider = "deepinfra"
		c.CommitModel = "deepseek-v3"
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	// Create second manager pointing to same config directory
	cm2, err := configuration.NewManagerSilent()
	if err != nil {
		t.Fatalf("NewManagerSilent: %v", err)
	}

	config2 := cm2.GetConfig()
	if provider := config2.GetCommitProvider(); provider != "deepinfra" {
		t.Errorf("commit provider not persisted to disk: got %q, want 'deepinfra'", provider)
	}
	if model := config2.GetCommitModel(); model != "deepseek-v3" {
		t.Errorf("commit model not persisted to disk: got %q, want 'deepseek-v3'", model)
	}
}

// TestCommitConfigSaveLoadRoundTrip tests that config can be saved and loaded
func TestCommitConfigSaveLoadRoundTrip(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	// Create a config with commit settings
	cfg := configuration.NewConfig()
	cfg.CommitProvider = "ollama-turbo"
	cfg.CommitModel = "deepseek-v3.1:671b"
	cfg.ReviewProvider = "openai"
	cfg.ReviewModel = "gpt-4-turbo"

	// Save config
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load config
	cfg2, err := configuration.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Verify settings
	if cfg2.CommitProvider != "ollama-turbo" {
		t.Errorf("expected commit provider 'ollama-turbo', got %q", cfg2.CommitProvider)
	}
	if cfg2.CommitModel != "deepseek-v3.1:671b" {
		t.Errorf("expected commit model 'deepseek-v3.1:671b', got %q", cfg2.CommitModel)
	}
	if cfg2.ReviewProvider != "openai" {
		t.Errorf("expected review provider 'openai', got %q", cfg2.ReviewProvider)
	}
	if cfg2.ReviewModel != "gpt-4-turbo" {
		t.Errorf("expected review model 'gpt-4-turbo', got %q", cfg2.ReviewModel)
	}

	// Verify getters work correctly
	if provider := cfg2.GetCommitProvider(); provider != "ollama-turbo" {
		t.Errorf("GetCommitProvider() returned %q, want 'ollama-turbo'", provider)
	}
	if model := cfg2.GetCommitModel(); model != "deepseek-v3.1:671b" {
		t.Errorf("GetCommitModel() returned %q, want 'deepseek-v3.1:671b'", model)
	}
	if provider := cfg2.GetReviewProvider(); provider != "openai" {
		t.Errorf("GetReviewProvider() returned %q, want 'openai'", provider)
	}
	if model := cfg2.GetReviewModel(); model != "gpt-4-turbo" {
		t.Errorf("GetReviewModel() returned %q, want 'gpt-4-turbo'", model)
	}
}

// TestCommitCommandDescription tests the commit command description
func TestCommitCommandDescription(t *testing.T) {
	cmd := &CommitCommand{}
	desc := cmd.Description()
	if !strings.Contains(desc, "commit") {
		t.Errorf("description should contain 'commit', got %q", desc)
	}
}

// TestCommitCommandName tests the commit command name
func TestCommitCommandName(t *testing.T) {
	cmd := &CommitCommand{}
	if name := cmd.Name(); name != "commit" {
		t.Errorf("expected name 'commit', got %q", name)
	}
}
