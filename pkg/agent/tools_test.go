package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
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

	t.Logf("DEBUG: Total tools returned: %d", len(tools))
	for i, tool := range tools {
		t.Logf("DEBUG: Tool %d: %s", i, tool.Function.Name)
	}

	if len(tools) == 0 {
		t.Fatal("expected at least one tool definition")
	}

	// Check that specific standard tools are present
	requiredTools := []string{"shell_command", "read_file", "edit_file", "write_file", "run_parallel_subagents"}
	toolMap := make(map[string]bool)

	for _, tool := range tools {
		toolMap[tool.Function.Name] = true
	}

	for _, required := range requiredTools {
		if !toolMap[required] {
			t.Errorf("Required tool '%s' not found in tool definitions", required)
		}
	}

	t.Logf("[OK] Agent has access to %d tools", len(tools))

	// Verify write_file is available
	foundWriteFile := false
	for _, tool := range tools {
		if tool.Function.Name == "write_file" {
			foundWriteFile = true
			break
		}
	}

	if !foundWriteFile {
		t.Fatal("Required tool 'write_file' not found in tool definitions")
	}
}

func TestStaticToolDefinitionsIncludeBrowseURL(t *testing.T) {
	allTools := api.GetToolDefinitions()
	found := make(map[string]bool, len(allTools))
	for _, tool := range allTools {
		found[tool.Function.Name] = true
	}

	for _, required := range []string{"browse_url", "run_parallel_subagents"} {
		if !found[required] {
			t.Fatalf("expected static tool definitions to include %s", required)
		}
	}
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

func TestExecuteToolRoutesJSONWritesAndEditsThroughStructuredValidation(t *testing.T) {
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
	writeCall.Function.Arguments = `{"path":"` + jsonPath + `","content":"{\"k\":1}"}`

	_, err = agent.executeTool(writeCall)
	if err != nil {
		t.Fatalf("expected valid JSON write_file call to auto-route to structured write, got: %v", err)
	}

	written, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("failed to read auto-routed json file: %v", err)
	}
	if !strings.Contains(string(written), `"k": 1`) {
		t.Fatalf("expected structured JSON formatting in file, got: %s", string(written))
	}

	invalidCall := api.ToolCall{ID: "call_guard_write_invalid", Type: "function"}
	invalidCall.Function.Name = "write_file"
	invalidCall.Function.Arguments = `{"path":"` + jsonPath + `","content":"{invalid"}`
	_, err = agent.executeTool(invalidCall)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "invalid json") {
		t.Fatalf("expected invalid JSON write_file to fail with invalid json error, got: %v", err)
	}

	if err := os.WriteFile(jsonPath, []byte(`{"x":1}`), 0644); err != nil {
		t.Fatalf("failed to seed json file: %v", err)
	}

	editCall := api.ToolCall{ID: "call_guard_edit", Type: "function"}
	editCall.Function.Name = "edit_file"
	editCall.Function.Arguments = `{"path":"` + jsonPath + `","old_str":"1","new_str":"2"}`

	_, err = agent.executeTool(editCall)
	if err != nil {
		t.Fatalf("expected valid edit_file on json to succeed via structured validation, got: %v", err)
	}

	edited, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("failed to read edited json file: %v", err)
	}
	if !strings.Contains(string(edited), `"x": 2`) {
		t.Fatalf("expected normalized structured json after edit, got: %s", string(edited))
	}

	badEdit := api.ToolCall{ID: "call_guard_edit_invalid", Type: "function"}
	badEdit.Function.Name = "edit_file"
	badEdit.Function.Arguments = `{"path":"` + jsonPath + `","old_str":"2","new_str":"2}"}` // makes JSON invalid
	_, err = agent.executeTool(badEdit)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "invalid json") {
		t.Fatalf("expected edit_file invalid json mutation to fail, got: %v", err)
	}

	restored, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("failed to read restored json file: %v", err)
	}
	if !strings.Contains(string(restored), `"x": 2`) {
		t.Fatalf("expected file content to be restored after invalid json edit, got: %s", string(restored))
	}
}

func TestGetOptimizedToolDefinitions_DeepInfraIncludesAnalyzeUIScreenshot(t *testing.T) {
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

	agent.clientType = api.DeepInfraClientType
	agent.activePersona = "orchestrator"

	tools := agent.getOptimizedToolDefinitions(nil)
	found := false
	for _, tool := range tools {
		if tool.Function.Name == "analyze_ui_screenshot" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected analyze_ui_screenshot in deepinfra/orchestrator tool set")
	}
}

func TestGetOptimizedToolDefinitions_CustomProviderAllowlistCanExcludeAnalyzeUIScreenshot(t *testing.T) {
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

	if err := agent.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			cfg.CustomProviders = make(map[string]configuration.CustomProviderConfig)
		}
		cfg.CustomProviders["deepinfra"] = configuration.CustomProviderConfig{
			Name:      "deepinfra",
			ToolCalls: []string{"read_file", "shell_command"},
		}
		return nil
	}); err != nil {
		t.Fatalf("failed to configure custom provider: %v", err)
	}

	agent.clientType = api.DeepInfraClientType
	agent.activePersona = "orchestrator"

	tools := agent.getOptimizedToolDefinitions(nil)
	for _, tool := range tools {
		if tool.Function.Name == "analyze_ui_screenshot" {
			t.Fatalf("did not expect analyze_ui_screenshot when deepinfra custom provider allowlist excludes it")
		}
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

func TestPatchStructuredFileAcceptsDataFallbackToWrite(t *testing.T) {
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
	jsonPath := filepath.Join(tmpDir, "patch_data_fallback.json")
	if err := os.WriteFile(jsonPath, []byte(`{"old":1}`), 0644); err != nil {
		t.Fatalf("failed to seed json file: %v", err)
	}

	call := api.ToolCall{ID: "call_patch_data_fallback", Type: "function"}
	call.Function.Name = "patch_structured_file"
	call.Function.Arguments = `{"path":"` + jsonPath + `","format":"json","data":{"new":2}}`

	_, err = agent.executeTool(call)
	if err != nil {
		t.Fatalf("expected patch_structured_file data fallback to succeed, got: %v", err)
	}

	b, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("failed reading result file: %v", err)
	}
	if !strings.Contains(string(b), `"new": 2`) {
		t.Fatalf("expected fallback write result, got: %s", string(b))
	}
}
