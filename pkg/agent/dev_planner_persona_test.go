package agent

import (
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

func TestDevPlannerPersona_ToolsResolveAndPromptLoads(t *testing.T) {
	agent := newTestAgent(t)
	t.Cleanup(agent.Shutdown)
	agent.SetWorkspaceRoot(t.TempDir())
	if err := agent.ApplyPersona(personas.IDDevPlanner); err != nil {
		t.Fatalf("ApplyPersona(dev_planner): %v", err)
	}
	dp := agent.configManager.GetConfig().GetSubagentType(personas.IDDevPlanner)
	if dp == nil {
		t.Fatal("dev_planner persona must resolve from the catalog")
	}
	if !dp.IsConfined() {
		t.Fatal("dev_planner must be confined (write_paths set)")
	}
	host := map[string]bool{}
	for _, name := range dp.HostTools {
		host[name] = true
	}
	registry := tools.GetNewToolRegistry()
	for _, name := range dp.AllowedTools {
		if host[name] {
			continue
		}
		if _, ok := registry.Lookup(name); !ok {
			t.Errorf("dev_planner allowed tool %q is not in the tool registry", name)
		}
	}

	prompt := agent.GetSystemPrompt()
	for _, anchor := range []string{"You are the developer planner", "propose_tasks", "repo_map", "verify", "draw_mockup"} {
		if !strings.Contains(prompt, anchor) {
			t.Errorf("dev_planner system prompt missing %q", anchor)
		}
	}
}
