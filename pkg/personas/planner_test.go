package personas

import (
	"slices"
	"testing"
)

// plannerAllowedTools is the planner's whole tool list:
// ask, take in what they bring, show it, agree it, read an existing project.
// The list is exact, not a floor: the persona is focused by what it leaves out
// (shell, git, subagents, MCP, skills, preview ports, todo lists), so an added
// tool must be a deliberate edit to this test as well as to the catalog.
var plannerAllowedTools = []string{
	"ask_user",
	"analyze_image_content",
	"browse_url",
	"write_plan",
	"draw_mockup",
	"read_file",
	"list_directory",
	"search",
	"start_change",
}

func plannerDefinition(t *testing.T) Definition {
	t.Helper()
	definitions, err := DefaultDefinitions()
	if err != nil {
		t.Fatalf("expected embedded persona configs to load, got error: %v", err)
	}
	def, ok := definitions[IDPlanner]
	if !ok {
		t.Fatalf("expected %q in default persona definitions", IDPlanner)
	}
	return def
}

func TestPlanner_DefinitionShape(t *testing.T) {
	def := plannerDefinition(t)

	if def.ID != IDPlanner {
		t.Errorf("planner ID = %q, want %q", def.ID, IDPlanner)
	}
	if !def.Enabled {
		t.Error("planner must ship enabled")
	}
	if def.Delegatable {
		t.Error("planner is a conversation persona the host runs directly; it must not be delegatable")
	}
	if def.SystemPrompt != "pkg/agent/prompts/subagent_prompts/planner.md" {
		t.Errorf("planner system_prompt = %q, want the planner prompt file", def.SystemPrompt)
	}
	if def.SystemPromptText != "" {
		t.Error("planner prompt lives in its file, not inline text")
	}
	if def.Provider != "" || def.Model != "" {
		t.Errorf("planner must inherit the host's provider/model, got provider=%q model=%q", def.Provider, def.Model)
	}
	if def.LocalOnly {
		t.Error("planner must not be local-only: it runs in the browser build too")
	}
	if def.ReadOnly {
		t.Error("planner writes the mockup and the plan; it is not read-only")
	}
	if len(def.Capabilities) != 0 {
		t.Errorf("planner holds no capability grants (no git_write, no computer_use), got %v", def.Capabilities)
	}
	if len(def.CanSpawnNonDelegatable) != 0 {
		t.Errorf("planner spawns nothing, got can_spawn_non_delegatable=%v", def.CanSpawnNonDelegatable)
	}
}

func TestPlanner_AllowedToolsAreExactlyTheFocusedSet(t *testing.T) {
	def := plannerDefinition(t)

	got := slices.Clone(def.AllowedTools)
	want := slices.Clone(plannerAllowedTools)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("planner allowed_tools = %v, want exactly %v", got, want)
	}

	// start_change is the host's tool (a change handed to its task queue):
	// declared as a host tool, not looked up in the built-in registry.
	if !slices.Equal(def.HostTools, []string{"start_change"}) {
		t.Errorf("planner host_tools = %v, want [start_change]", def.HostTools)
	}

	// The leave-outs are the point of the persona; name the important ones
	// so a regression reads as what it is.
	for _, banned := range []string{
		"shell_command", "git", "commit", "run_subagent", "run_parallel_subagents",
		"mcp_tools", "activate_skill", "list_skills", "register_preview_port",
		"write_file", "edit_file", "write_structured_file", "patch_structured_file",
		"design_critique", "design_render",
		"deploy", "create_pull_request", "manage_settings", "manage_memory",
	} {
		if slices.Contains(def.AllowedTools, banned) {
			t.Errorf("planner allowed_tools must not include %q", banned)
		}
	}
}

func TestPlanner_WritesConfinedToDesign(t *testing.T) {
	def := plannerDefinition(t)
	if !slices.Equal(def.WritePaths, []string{"design/"}) {
		t.Errorf("planner write_paths = %v, want [design/]: the mockup tool writes design/mockup.html and write_plan owns .sprout/", def.WritePaths)
	}
}

func TestDefaultDefinitionsCloneIsolation_WritePaths(t *testing.T) {
	first, _ := DefaultDefinitions()
	second, _ := DefaultDefinitions()

	p := first[IDPlanner]
	p.WritePaths[0] = "/"
	if second[IDPlanner].WritePaths[0] != "design/" {
		t.Fatal("write_paths must be cloned per caller; a mutation leaked into the catalog")
	}
}
