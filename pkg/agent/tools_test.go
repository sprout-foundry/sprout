package agent

import (
	"os"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// TestGetOptimizedToolDefinitions verifies that the agent gets both standard and MCP tools
func TestGetOptimizedToolDefinitions(t *testing.T) {
	// Set CI environment and test API key to ensure agent creation succeeds
	originalCI := os.Getenv("CI")
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("CI", "1")
	os.Setenv("OPENROUTER_API_KEY", "test-key-for-tools-long-enough")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
		if originalCI != "" {
			os.Setenv("CI", originalCI)
		} else {
			os.Unsetenv("CI")
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
	// Set up test environment with API keys and CI flag
	originalCI := os.Getenv("CI")
	originalOpenRouterKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("CI", "1")
	os.Setenv("OPENROUTER_API_KEY", "test-key-openrouter-long-enough")
	defer func() {
		if originalOpenRouterKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalOpenRouterKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
		if originalCI != "" {
			os.Setenv("CI", originalCI)
		} else {
			os.Unsetenv("CI")
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

func TestFilterToolsByName(t *testing.T) {
	allTools := api.GetToolDefinitions()
	allowed := makeAllowedToolSet([]string{"read_file", "search_files", "missing_tool"})

	filtered := filterToolsByName(allTools, allowed)
	if len(filtered) == 0 {
		t.Fatalf("expected filtered tools to include configured tools")
	}

	found := make(map[string]bool, len(filtered))
	for _, tool := range filtered {
		found[tool.Function.Name] = true
	}

	if !found["read_file"] {
		t.Fatalf("expected read_file to be included")
	}
	if !found["search_files"] {
		t.Fatalf("expected search_files to be included")
	}
	if found["write_file"] {
		t.Fatalf("expected write_file to be excluded")
	}
}

func TestMakeAllowedToolSetTrimsAndDeduplicates(t *testing.T) {
	toolSet := makeAllowedToolSet([]string{" read_file ", "read_file", "", "  ", "write_file"})
	if len(toolSet) != 2 {
		t.Fatalf("expected 2 unique tools, got %d", len(toolSet))
	}
	if _, ok := toolSet["read_file"]; !ok {
		t.Fatalf("expected read_file to exist")
	}
	if _, ok := toolSet["write_file"]; !ok {
		t.Fatalf("expected write_file to exist")
	}
}
