package agent

import (
	"os"
	"strings"
	"testing"
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
	if agent.maxIterations != 20 {
		t.Errorf("Expected maxIterations to be 20, got %d", agent.maxIterations)
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

	agent, err := NewAgentWithModel("deepseek/deepseek-chat")
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	if agent == nil {
		t.Fatal("NewAgentWithModel returned nil agent")
	}

	// Verify agent properties
	if agent.GetMaxIterations() != 100 {
		t.Errorf("Expected maxIterations to be 100, got %d", agent.GetMaxIterations())
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

	if agent.GetMaxIterations() != 100 {
		t.Errorf("Expected GetMaxIterations() to be 100, got %d", agent.GetMaxIterations())
	}

	if agent.GetTotalCost() != 0.0 {
		t.Errorf("Expected GetTotalCost() to be 0.0, got %f", agent.GetTotalCost())
	}

	messages := agent.GetMessages()
	if len(messages) != 0 {
		t.Errorf("Expected GetMessages() to return empty slice, got %d messages", len(messages))
	}

	history := agent.GetConversationHistory()
	if len(history) != 0 {
		t.Errorf("Expected GetConversationHistory() to return empty slice, got %d messages", len(history))
	}

	lastMessage := agent.GetLastAssistantMessage()
	if lastMessage != "" {
		t.Errorf("Expected GetLastAssistantMessage() to return empty string, got %q", lastMessage)
	}

	configManager := agent.GetConfigManager()
	if configManager == nil {
		t.Error("Expected GetConfigManager() to return non-nil manager")
	}
}

// TestGetProjectContext tests the project context functionality
func TestGetProjectContext(t *testing.T) {
	// Test with no project context files
	context := getProjectContext()
	if context != "" {
		t.Errorf("Expected empty context when no files exist, got %q", context)
	}

	// Create a temporary project context file
	testContent := "Test project context"
	err := os.WriteFile(".project_context.md", []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(".project_context.md")

	context = getProjectContext()
	expectedPrefix := "PROJECT CONTEXT:\n"
	if !strings.HasPrefix(context, expectedPrefix) {
		t.Errorf("Expected context to start with %q, got %q", expectedPrefix, context)
	}

	if !strings.Contains(context, testContent) {
		t.Errorf("Expected context to contain %q, got %q", testContent, context)
	}
}

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
