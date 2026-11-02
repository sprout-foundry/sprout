package personas

import (
	"slices"
	"testing"
)

// devPlannerAllowedTools is the developer planner's whole tool list: read the
// repository, propose the tasks, and show a UI change first. Exact, like the
// planner's: an added tool is a deliberate edit here as well as in the catalog.
var devPlannerAllowedTools = []string{
	"ask_user",
	"read_file",
	"list_directory",
	"search",
	"repo_map",
	"write_plan",
	"draw_mockup",
	"propose_tasks",
}

func TestDevPlanner_DefinitionShape(t *testing.T) {
	definitions, err := DefaultDefinitions()
	if err != nil {
		t.Fatalf("load persona configs: %v", err)
	}
	def, ok := definitions[IDDevPlanner]
	if !ok {
		t.Fatalf("expected %q in default persona definitions", IDDevPlanner)
	}
	if !def.Enabled || def.Delegatable {
		t.Errorf("dev_planner must ship enabled and not delegatable, got enabled=%v delegatable=%v", def.Enabled, def.Delegatable)
	}
	if def.SystemPrompt != "pkg/agent/prompts/subagent_prompts/dev_planner.md" {
		t.Errorf("dev_planner system_prompt = %q", def.SystemPrompt)
	}
	if def.Provider != "" || def.Model != "" || def.LocalOnly {
		t.Errorf("dev_planner must inherit the host's model and run in the browser build, got provider=%q model=%q local_only=%v", def.Provider, def.Model, def.LocalOnly)
	}
	if len(def.Capabilities) != 0 {
		t.Errorf("dev_planner holds no capability grants, got %v", def.Capabilities)
	}

	got := slices.Clone(def.AllowedTools)
	want := slices.Clone(devPlannerAllowedTools)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("dev_planner allowed_tools = %v, want exactly %v", got, want)
	}
	if !slices.Equal(def.HostTools, []string{"propose_tasks"}) {
		t.Errorf("dev_planner host_tools = %v, want [propose_tasks]", def.HostTools)
	}
	for _, banned := range []string{
		"shell_command", "git", "commit", "run_subagent", "run_parallel_subagents",
		"mcp_tools", "activate_skill", "write_file", "edit_file", "browse_url",
		"start_change",
	} {
		if slices.Contains(def.AllowedTools, banned) {
			t.Errorf("dev_planner allowed_tools must not include %q", banned)
		}
	}
	if !slices.Equal(def.WritePaths, []string{"design/"}) {
		t.Errorf("dev_planner write_paths = %v, want [design/]", def.WritePaths)
	}
}
