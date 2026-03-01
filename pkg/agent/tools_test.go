package agent

import (
	"os"
	"path/filepath"
	"strings"
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

	if len(tools) == 0 {
		t.Fatal("expected at least one tool definition")
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

	t.Logf("✅ Agent has access to %d tools", len(tools))
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

func TestExecuteToolAppliesOpenFileAlias(t *testing.T) {
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

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Failed to create agent: %v", err)
	}

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "alias_read_test.txt")
	if err := os.WriteFile(filePath, []byte("alias works"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	toolCall := api.ToolCall{
		ID:   "call_alias_test",
		Type: "function",
	}
	toolCall.Function.Name = "open_file"
	toolCall.Function.Arguments = `{"path":"` + filePath + `"}`

	result, err := agent.executeTool(toolCall)
	if err != nil {
		t.Fatalf("expected open_file alias to execute as read_file, got error: %v", err)
	}
	if !strings.Contains(result, "alias works") {
		t.Fatalf("expected file contents in result, got: %s", result)
	}
}

func TestExecuteToolRejectsRawStructuredWrites(t *testing.T) {
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

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Failed to create agent: %v", err)
	}

	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "guard_test.json")

	writeCall := api.ToolCall{ID: "call_guard_write", Type: "function"}
	writeCall.Function.Name = "write_file"
	writeCall.Function.Arguments = `{"path":"` + jsonPath + `","content":"{}"}`

	_, err = agent.executeTool(writeCall)
	if err == nil || !strings.Contains(err.Error(), "write_structured_file") {
		t.Fatalf("expected write_file guard to suggest structured tools, got: %v", err)
	}

	if err := os.WriteFile(jsonPath, []byte(`{"x":1}`), 0644); err != nil {
		t.Fatalf("failed to seed json file: %v", err)
	}

	editCall := api.ToolCall{ID: "call_guard_edit", Type: "function"}
	editCall.Function.Name = "edit_file"
	editCall.Function.Arguments = `{"path":"` + jsonPath + `","old_str":"1","new_str":"2"}`

	_, err = agent.executeTool(editCall)
	if err == nil || !strings.Contains(err.Error(), "patch_structured_file") {
		t.Fatalf("expected edit_file guard to suggest structured tools, got: %v", err)
	}
}

func TestPatchStructuredFileAcceptsOperationsAlias(t *testing.T) {
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

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Failed to create agent: %v", err)
	}

	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "alias_patch.json")
	if err := os.WriteFile(jsonPath, []byte(`{"items":[]}`), 0644); err != nil {
		t.Fatalf("failed to seed json file: %v", err)
	}

	patchCall := api.ToolCall{ID: "call_patch_alias", Type: "function"}
	patchCall.Function.Name = "patch_structured_file"
	patchCall.Function.Arguments = `{"path":"` + jsonPath + `","operations":[{"op":"add","path":"/items/0","value":"x"}]}`

	_, err = agent.executeTool(patchCall)
	if err != nil {
		t.Fatalf("expected operations alias to work, got error: %v", err)
	}

	b, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("failed to read patched file: %v", err)
	}
	if !strings.Contains(string(b), `"x"`) {
		t.Fatalf("expected patched value in file, got: %s", string(b))
	}
}
