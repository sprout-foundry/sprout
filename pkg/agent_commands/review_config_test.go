package commands

import (
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/configuration"
)

// TestReviewCommandUsesConfiguredProvider tests that review command uses configured provider
func TestReviewCommandUsesConfiguredProvider(t *testing.T) {
	// Create a test agent with temp config directory
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OPENROUTER_API_KEY", "test-key-for-unit-tests")

	chatAgent, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	// Set review provider via configuration manager
	cm := chatAgent.GetConfigManager()
	err = cm.UpdateConfig(func(c *configuration.Config) error {
		c.ReviewProvider = "ollama-local"
		c.ReviewModel = "qwen3-coder:30b"
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	// Verify the configuration was set
	config := cm.GetConfig()
	if config.ReviewProvider != "ollama-local" {
		t.Errorf("expected review provider to be 'ollama-local', got %q", config.ReviewProvider)
	}
	if config.ReviewModel != "qwen3-coder:30b" {
		t.Errorf("expected review model to be 'qwen3-coder:30b', got %q", config.ReviewModel)
	}

	// Verify getter methods return the configured values
	if provider := config.GetReviewProvider(); provider != "ollama-local" {
		t.Errorf("GetReviewProvider() returned %q, want 'ollama-local'", provider)
	}
	if model := config.GetReviewModel(); model != "qwen3-coder:30b" {
		t.Errorf("GetReviewModel() returned %q, want 'qwen3-coder:30b'", model)
	}
}

// TestReviewCommandFallsBackToLastUsedProvider tests fallback behavior
func TestReviewCommandFallsBackToLastUsedProvider(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OPENROUTER_API_KEY", "test-key-for-unit-tests")

	chatAgent, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	cm := chatAgent.GetConfigManager()

	// Set LastUsedProvider but leave ReviewProvider empty
	err = cm.UpdateConfig(func(c *configuration.Config) error {
		c.ReviewProvider = ""
		c.ReviewModel = ""
		c.LastUsedProvider = "zai"
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	config := cm.GetConfig()
	if provider := config.GetReviewProvider(); provider != "zai" {
		t.Errorf("GetReviewProvider() should fall back to LastUsedProvider, got %q", provider)
	}
}

// TestReviewCommandPersistsToDisk tests that config changes are persisted
func TestReviewCommandPersistsToDisk(t *testing.T) {
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
		c.ReviewProvider = "openai"
		c.ReviewModel = "gpt-4-turbo"
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
	if provider := config2.GetReviewProvider(); provider != "openai" {
		t.Errorf("review provider not persisted to disk: got %q, want 'openai'", provider)
	}
	if model := config2.GetReviewModel(); model != "gpt-4-turbo" {
		t.Errorf("review model not persisted to disk: got %q, want 'gpt-4-turbo'", model)
	}
}

// TestReviewCommandName tests the review command name
func TestReviewCommandName(t *testing.T) {
	cmd := &ReviewCommand{}
	if name := cmd.Name(); name != "review" {
		t.Errorf("expected name 'review', got %q", name)
	}
}

// TestReviewCommandDescription tests the review command description
func TestReviewCommandDescription(t *testing.T) {
	cmd := &ReviewCommand{}
	desc := cmd.Description()
	if !strings.Contains(desc, "review") {
		t.Errorf("description should contain 'review', got %q", desc)
	}
}

// TestReviewDeepCommandName tests the review-deep command name
func TestReviewDeepCommandName(t *testing.T) {
	cmd := &ReviewDeepCommand{}
	if name := cmd.Name(); name != "review-deep" {
		t.Errorf("expected name 'review-deep', got %q", name)
	}
}

// TestReviewDeepCommandDescription tests the review-deep command description
func TestReviewDeepCommandDescription(t *testing.T) {
	cmd := &ReviewDeepCommand{}
	desc := cmd.Description()
	if !strings.Contains(desc, "deep") {
		t.Errorf("description should contain 'deep', got %q", desc)
	}
}

// TestCommitAndReviewConfigsIndependent tests that commit and review configs are independent
func TestCommitAndReviewConfigsIndependent(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OPENROUTER_API_KEY", "test-key-for-unit-tests")

	chatAgent, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	cm := chatAgent.GetConfigManager()
	err = cm.UpdateConfig(func(c *configuration.Config) error {
		// Set different providers for commit and review
		c.CommitProvider = "openai"
		c.CommitModel = "gpt-4"
		c.ReviewProvider = "ollama-local"
		c.ReviewModel = "qwen3-coder:30b"
		c.LastUsedProvider = "openrouter"
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	config := cm.GetConfig()

	// Verify commit config
	if config.GetCommitProvider() != "openai" {
		t.Errorf("expected commit provider 'openai', got %q", config.GetCommitProvider())
	}
	if config.GetCommitModel() != "gpt-4" {
		t.Errorf("expected commit model 'gpt-4', got %q", config.GetCommitModel())
	}

	// Verify review config
	if config.GetReviewProvider() != "ollama-local" {
		t.Errorf("expected review provider 'ollama-local', got %q", config.GetReviewProvider())
	}
	if config.GetReviewModel() != "qwen3-coder:30b" {
		t.Errorf("expected review model 'qwen3-coder:30b', got %q", config.GetReviewModel())
	}
}

// TestReviewConfigWithMultipleProviders tests review config with different providers
func TestReviewConfigWithMultipleProviders(t *testing.T) {
	tests := []struct {
		name          string
		provider      string
		model         string
		lastUsed      string
		expected      string
	}{
		{
			name:     "openai",
			provider: "openai",
			model:    "gpt-4-turbo",
			expected: "openai",
		},
		{
			name:     "ollama-turbo",
			provider: "ollama-turbo",
			model:    "deepseek-v3.1:671b",
			expected: "ollama-turbo",
		},
		{
			name:     "zai",
			provider: "zai",
			model:    "GLM-4.6",
			expected: "zai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("OPENROUTER_API_KEY", "test-key-for-unit-tests")

			chatAgent, err := agent.NewAgent()
			if err != nil {
				t.Fatalf("NewAgent: %v", err)
			}

			cm := chatAgent.GetConfigManager()
			err = cm.UpdateConfig(func(c *configuration.Config) error {
				c.ReviewProvider = tt.provider
				c.ReviewModel = tt.model
				return nil
			})
			if err != nil {
				t.Fatalf("UpdateConfig: %v", err)
			}

			config := cm.GetConfig()
			if provider := config.GetReviewProvider(); provider != tt.expected {
				t.Errorf("expected review provider %q, got %q", tt.expected, provider)
			}
		})
	}
}
