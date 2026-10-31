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

	// "search", not "search_files": the latter is Hidden (superseded) and so is
	// deliberately absent from the advertised roster.
	essential := []string{"read_file", "shell_command", "write_file", "search"}
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
	pathProp, ok := toolProperties(t, rf)["path"].(map[string]any)
	if !ok || pathProp["type"] != "string" {
		t.Error("read_file missing 'path' parameter of type 'string'")
	}

	// Verify shell_command has command parameter
	sc, ok := nameMap["shell_command"]
	if !ok {
		t.Fatal("shell_command not found in BuildToolDefinitions")
	}
	if _, hasCmd := toolProperties(t, sc)["command"]; !hasCmd {
		t.Error("shell_command missing 'command' parameter")
	}
}

// TestBuildToolDefinitions_KeepsEnumAndItems pins the wire fidelity of the
// schema: seed's typed core.ToolParameter has no enum or items field, so
// routing properties through it silently dropped both and the model never saw
// a closed vocabulary or an array's element shape. The properties map is built
// raw to keep them; this is the test that notices if that regresses.
func TestBuildToolDefinitions_KeepsEnumAndItems(t *testing.T) {
	defs := BuildToolDefinitions()
	var browse *api.Tool
	for i := range defs {
		if defs[i].Function.Name == "browse_url" {
			browse = &defs[i]
			break
		}
	}
	if browse == nil {
		t.Fatal("browse_url not found in BuildToolDefinitions")
	}

	props := toolProperties(t, *browse)

	// Top-level action: a closed vocabulary.
	actionProp, ok := props["action"].(map[string]any)
	if !ok {
		t.Fatalf("browse_url 'action' property is %T, want map", props["action"])
	}
	actionEnum, ok := actionProp["enum"].([]string)
	if !ok || len(actionEnum) == 0 {
		t.Fatalf("browse_url 'action' must carry a non-empty enum on the wire, got %T %v", actionProp["enum"], actionProp["enum"])
	}

	// steps: an array whose element schema (and its nested enum) must survive.
	stepsProp, ok := props["steps"].(map[string]any)
	if !ok {
		t.Fatalf("browse_url 'steps' property is %T, want map", props["steps"])
	}
	items, ok := stepsProp["items"].(map[string]any)
	if !ok {
		t.Fatalf("browse_url 'steps' must carry items on the wire, got %T", stepsProp["items"])
	}
	itemProps, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatalf("browse_url 'steps' items must carry properties, got %T", items["properties"])
	}
	stepAction, ok := itemProps["action"].(map[string]any)
	if !ok {
		t.Fatalf("step 'action' property is %T, want map", itemProps["action"])
	}
	stepEnum, ok := stepAction["enum"].([]string)
	if !ok || len(stepEnum) == 0 {
		t.Fatalf("step 'action' must carry a non-empty enum on the wire, got %T %v", stepAction["enum"], stepAction["enum"])
	}
}

// toolProperties extracts the properties map from a tool's Parameters,
// which convertHandlerToAPITool emits as a raw JSON-schema map.
func toolProperties(t *testing.T, tool api.Tool) map[string]any {
	t.Helper()
	params, ok := tool.Function.Parameters.(map[string]any)
	if !ok {
		t.Fatalf("%s: Parameters is %T, want map[string]any", tool.Function.Name, tool.Function.Parameters)
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s: parameters.properties is %T, want map[string]any", tool.Function.Name, params["properties"])
	}
	return props
}
