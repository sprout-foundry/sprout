package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// TestBuildToolDefinitions verifies that BuildToolDefinitions returns all
// handler-based tool definitions plus the synthetic mcp_tools entry.
func TestBuildToolDefinitions(t *testing.T) {
	defs := BuildToolDefinitions()
	if len(defs) == 0 {
		t.Fatal("BuildToolDefinitions returned no tools")
	}

	// Check for essential tools
	nameSet := make(map[string]bool)
	for _, d := range defs {
		nameSet[d.Function.Name] = true
	}

	essential := []string{"read_file", "shell_command", "write_file", "search_files"}
	for _, name := range essential {
		if !nameSet[name] {
			t.Errorf("BuildToolDefinitions missing essential tool: %s", name)
		}
	}

	// Verify sorted
	for i := 1; i < len(defs); i++ {
		if defs[i].Function.Name < defs[i-1].Function.Name {
			t.Errorf("Tools not sorted: %q after %q", defs[i].Function.Name, defs[i-1].Function.Name)
		}
	}
}

func TestBuildToolDefinitions_Count(t *testing.T) {
	defs := BuildToolDefinitions()
	handlerTools := tools.GetNewToolRegistry().All()

	// BuildToolDefinitions adds a synthetic mcp_tools entry, so count = handlers + 1
	if len(defs) != len(handlerTools)+1 {
		t.Logf("BuildToolDefinitions returned %d tools, handler registry has %d tools (+1 mcp_tools)", len(defs), len(handlerTools))
	}
}

func TestBuildToolDefinitions_HasMCPTools(t *testing.T) {
	defs := BuildToolDefinitions()
	found := false
	for _, d := range defs {
		if d.Function.Name == "mcp_tools" {
			found = true
			break
		}
	}
	if !found {
		t.Error("BuildToolDefinitions missing mcp_tools synthetic entry")
	}
}

func TestBuildToolDefinitions_Parameters(t *testing.T) {
	defs := BuildToolDefinitions()
	nameMap := make(map[string]api.Tool)
	for _, d := range defs {
		nameMap[d.Function.Name] = d
	}

	// Verify read_file has path parameter
	rf, ok := nameMap["read_file"]
	if !ok {
		t.Fatal("read_file not found in BuildToolDefinitions")
	}
	props, ok := rf.Function.Parameters.(api.ToolParameters).Properties["path"]
	if !ok || props.Type != "string" {
		t.Error("read_file missing 'path' parameter of type 'string'")
	}

	// Verify shell_command has command parameter
	sc, ok := nameMap["shell_command"]
	if !ok {
		t.Fatal("shell_command not found in BuildToolDefinitions")
	}
	_, hasCmd := sc.Function.Parameters.(api.ToolParameters).Properties["command"]
	if !hasCmd {
		t.Error("shell_command missing 'command' parameter")
	}
}
