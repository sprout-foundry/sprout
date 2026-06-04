package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBuildToolDefinitions is the post-unification successor to the
// pre-existing TestGetToolDefinitions in pkg/agent_api/interface_test.go.
// agent_api/tools.go (the old hand-maintained static catalog) was deleted
// in favour of BuildToolDefinitions(), which walks pkg/agent/tool_registrations.go
// so there is one source of truth for both the LLM (via seedRegistry) and
// every legacy caller of the api.GetToolDefinitions() shape.
//
// The essential-tools list mirrors what the model is expected to know
// about; if any of these disappear from the registry, the build should
// fail rather than have the model silently lose access.
func TestBuildToolDefinitions(t *testing.T) {
	tools := BuildToolDefinitions()
	assert.NotEmpty(t, tools, "should return tool definitions")

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		assert.Equal(t, "function", tool.Type, "tool type should be 'function'")
		assert.NotEmpty(t, tool.Function.Name, "tool should have a name")
		assert.NotEmpty(t, tool.Function.Description, "tool should have a description")
		assert.False(t, toolNames[tool.Function.Name], "duplicate tool name: %s", tool.Function.Name)
		toolNames[tool.Function.Name] = true
	}

	essentialTools := []string{
		"shell_command", "read_file", "write_file", "edit_file",
		"search_files", "web_search", "fetch_url",
		"run_subagent", "mcp_tools",
	}
	for _, name := range essentialTools {
		assert.True(t, toolNames[name], "missing essential tool: %s", name)
	}
}
