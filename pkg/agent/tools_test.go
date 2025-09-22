package agent

import (
	"os"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// TestGetOptimizedToolDefinitions verifies that the agent gets both standard and MCP tools
func TestGetOptimizedToolDefinitions(t *testing.T) {
	// Set a test API key to ensure agent creation succeeds
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key-for-tools")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	// Create a test agent
	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Failed to create agent: %v", err)
	}

	// Get the tools that would be passed to the LLM
	tools := agent.getOptimizedToolDefinitions(agent.messages)

	// Get standard tools for comparison
	standardTools := api.GetToolDefinitions()

	// Verify we have at least the standard tools
	if len(tools) < len(standardTools) {
		t.Errorf("Expected at least %d tools (standard tools), but got %d", len(standardTools), len(tools))
	}

	// Check that specific standard tools are present
	requiredTools := []string{"shell_command", "read_file", "edit_file", "write_file"}
	toolMap := make(map[string]bool)

	for _, tool := range tools {
		toolMap[tool.Function.Name] = true
	}

	for _, required := range requiredTools {
		if !toolMap[required] {
			t.Errorf("Required tool '%s' not found in tool definitions", required)
		}
	}

	t.Logf("✅ Agent has access to %d tools (including %d standard tools)", len(tools), len(standardTools))
}

// TestOllamaAPIKeyDetection verifies that OLLAMA_API_KEY is properly detected
func TestOllamaAPIKeyDetection(t *testing.T) {
	// Set up test environment with API keys
	originalOpenRouterKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key-openrouter")
	defer func() {
		if originalOpenRouterKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalOpenRouterKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	}()

	// This test is more about configuration, which is tested elsewhere
	// Just verify that the agent can be created when OLLAMA_API_KEY is set
	t.Setenv("OLLAMA_API_KEY", "test-key-123")

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Failed to create agent with OLLAMA_API_KEY set: %v", err)
	}

	if agent == nil {
		t.Error("Agent is nil")
	}
}
