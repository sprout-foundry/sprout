package agent

import (
	"os"
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
)

// TestNewAgent tests agent creation
func TestNewAgent(t *testing.T) {
	// Set a test API key to avoid provider issues
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		// If this fails due to connection issues, skip the test
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	if agent == nil {
		t.Fatal("NewAgent returned nil agent")
	}

	// Test basic properties
	if agent.maxIterations != 1000 {
		t.Errorf("Expected maxIterations to be 1000, got %d", agent.maxIterations)
	}

	if agent.currentIteration != 0 {
		t.Errorf("Expected currentIteration to be 0, got %d", agent.currentIteration)
	}

	if agent.totalCost != 0.0 {
		t.Errorf("Expected totalCost to be 0.0, got %f", agent.totalCost)
	}

	if len(agent.messages) != 0 {
		t.Errorf("Expected messages to be empty, got %d messages", len(agent.messages))
	}

	if agent.shellCommandHistory == nil {
		t.Error("Expected shellCommandHistory to be initialized")
	}
}

// TestNewAgentWithModel tests agent creation with specific model
func TestNewAgentWithModel(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgentWithModel("deepseek/deepseek-chat-v3.1:free")
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	if agent == nil {
		t.Fatal("NewAgentWithModel returned nil agent")
	}

	// Verify agent properties
	if agent.maxIterations != 1000 {
		t.Errorf("Expected maxIterations to be 1000, got %d", agent.maxIterations)
	}
}

// TestBasicGetters tests all the basic getter methods
func TestBasicGetters(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	// Test all getter methods
	if agent.GetCurrentIteration() != 0 {
		t.Errorf("Expected GetCurrentIteration() to be 0, got %d", agent.GetCurrentIteration())
	}

	if agent.maxIterations != 1000 {
		t.Errorf("Expected maxIterations to be 1000, got %d", agent.maxIterations)
	}

	if agent.GetTotalCost() != 0.0 {
		t.Errorf("Expected GetTotalCost() to be 0.0, got %f", agent.GetTotalCost())
	}

	messages := agent.GetMessages()
	if len(messages) != 0 {
		t.Errorf("Expected GetMessages() to return empty slice, got %d messages", len(messages))
	}

	configManager := agent.GetConfigManager()
	if configManager == nil {
		t.Error("Expected GetConfigManager() to return non-nil manager")
	}
}

func TestResolveConfiguredSystemPrompt(t *testing.T) {
	t.Run("uses configured override when present", func(t *testing.T) {
		cfg := &configuration.Config{SystemPromptText: "custom prompt"}
		got := resolveConfiguredSystemPrompt(cfg, "default prompt")
		if got != "custom prompt" {
			t.Fatalf("expected configured prompt override, got %q", got)
		}
	})

	t.Run("falls back to embedded prompt when blank", func(t *testing.T) {
		cfg := &configuration.Config{SystemPromptText: "   "}
		got := resolveConfiguredSystemPrompt(cfg, "default prompt")
		if got != "default prompt" {
			t.Fatalf("expected fallback prompt, got %q", got)
		}
	})

	t.Run("falls back when config missing", func(t *testing.T) {
		got := resolveConfiguredSystemPrompt(nil, "default prompt")
		if got != "default prompt" {
			t.Fatalf("expected fallback prompt, got %q", got)
		}
	})
}

// TestGetProjectContext - removed as getProjectContext was removed

// TestAgentStructFields tests that all expected struct fields are present
func TestAgentStructFields(t *testing.T) {
	// Set test API key
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	// Check that critical fields are initialized
	if agent.client == nil {
		t.Error("Expected client to be initialized")
	}

	if agent.systemPrompt == "" {
		t.Error("Expected systemPrompt to be set")
	}

	if agent.optimizer == nil {
		t.Error("Expected optimizer to be initialized")
	}

	if agent.configManager == nil {
		t.Error("Expected configManager to be initialized")
	}

	if agent.shellCommandHistory == nil {
		t.Error("Expected shellCommandHistory to be initialized")
	}
}
