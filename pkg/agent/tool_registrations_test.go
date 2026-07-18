package agent

import (
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// TestAllToolsRegistered verifies that the handler registry contains
// the expected essential tools.
func TestAllToolsRegistered(t *testing.T) {
	reg := tools.GetNewToolRegistry()

	essentialTools := []string{
		"shell_command",
		"git",
		"commit",
		"read_file",
		"write_file",
		"edit_file",
		"write_structured_file",
		"patch_structured_file",
		"ask_user",
		"search_files",
		"repo_map",
		"web_search",
		"fetch_url",
		"browse_url",
		"analyze_ui_screenshot",
		"analyze_image_content",
		"view_history",
		"rollback_changes",
		"list_skills",
		"activate_skill",
		"manage_memory",
		"manage_settings",
		// task_queue removed 2026-07-18 — see DISABLED note in
		// pkg/agent_tools/all.go. Re-enable by uncommenting &taskQueueHandler{}
		// and adding "task_queue" back here.
		"run_automate",
		"list_automate_workflows",
		"mcp_refresh",
	}

	for _, toolName := range essentialTools {
		h, found := reg.Lookup(toolName)
		if !found {
			t.Errorf("tool %q not found in handler registry", toolName)
			continue
		}
		def := h.Definition()
		if def.Description == "" {
			t.Errorf("tool %q has empty description", toolName)
		}
	}
}

// TestAllToolsRegistered_Count verifies a reasonable number of tools exist.
func TestAllToolsRegistered_Count(t *testing.T) {
	reg := tools.GetNewToolRegistry()
	count := len(reg.All())
	if count < 35 {
		t.Errorf("expected at least 35 tools, got %d", count)
	}
	t.Logf("Handler registry has %d tools", count)
}

// TestAllToolsHaveHandlers verifies every handler has non-empty Name and Description.
func TestAllToolsHaveHandlers(t *testing.T) {
	for _, h := range tools.AllTools() {
		name := h.Name()
		def := h.Definition()
		if name == "" {
			t.Error("a tool handler has empty Name()")
		}
		if def.Description == "" {
			t.Errorf("tool %q has empty description", name)
		}
	}
}
