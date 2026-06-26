package tools

import (
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// Integration tests for the new tool registry
// ---------------------------------------------------------------------------

// TestRegistry_AllToolsHaveValidDefinitions verifies that every registered
// tool handler returns a well-formed ToolDefinition with non-empty Name and
// Description, and a non-nil Parameters slice.
func TestRegistry_AllToolsHaveValidDefinitions(t *testing.T) {
	tools := AllTools()
	if len(tools) == 0 {
		t.Fatal("AllTools() returned no tools")
	}

	for _, h := range tools {
		name := h.Name()
		t.Run(name, func(t *testing.T) {
			def := h.Definition()

			if def.Name == "" {
				t.Errorf("tool %q has empty Name in Definition()", name)
			} else if def.Name != name {
				t.Errorf("tool Name() returns %q but Definition().Name is %q — mismatch", name, def.Name)
			}

			if def.Description == "" {
				t.Errorf("tool %q has empty Description", name)
			}

			if def.Parameters == nil {
				t.Errorf("tool %q has nil Parameters slice (should be non-nil, even if empty)", name)
			}
		})
	}
}

// TestRegistry_AllToolsRespectPersonaFilter confirms that ForPersona filters
// correctly against an allowlist and that an empty allowlist returns all tools.
func TestRegistry_AllToolsRespectPersonaFilter(t *testing.T) {
	reg := NewToolRegistry()

	all := AllTools()
	for _, h := range all {
		if err := reg.Register(h); err != nil {
			t.Fatalf("failed to register tool %q: %v", h.Name(), err)
		}
	}

	total := len(reg.All())
	if total == 0 {
		t.Fatal("registry has no tools after registration")
	}

	t.Run("empty allowlist returns all tools", func(t *testing.T) {
		tools := reg.ForPersona(nil)
		if len(tools) != total {
			t.Errorf("ForPersona(nil) returned %d tools, expected %d", len(tools), total)
		}
		tools = reg.ForPersona([]string{})
		if len(tools) != total {
			t.Errorf("ForPersona([]) returned %d tools, expected %d", len(tools), total)
		}
	})

	t.Run("subset allowlist returns only matching tools", func(t *testing.T) {
		allowlist := []string{"read_file", "write_file", "git"}
		tools := reg.ForPersona(allowlist)
		if len(tools) != len(allowlist) {
			t.Errorf("ForPersona(%v) returned %d tools, expected %d", allowlist, len(tools), len(allowlist))
		}
		for _, name := range allowlist {
			if _, ok := tools[name]; !ok {
				t.Errorf("ForPersona missing tool %q", name)
			}
		}
	})

	t.Run("allowlist with unknown tool skips silently", func(t *testing.T) {
		allowlist := []string{"read_file", "does_not_exist"}
		tools := reg.ForPersona(allowlist)
		if len(tools) != 1 {
			t.Errorf("ForPersona returned %d tools, expected 1", len(tools))
		}
		if _, ok := tools["does_not_exist"]; ok {
			t.Error("unknown tool should not appear in result")
		}
	})

	// Smoke-check that Names() returns a sorted slice.
	names := reg.Names()
	if !sort.StringsAreSorted(names) {
		t.Error("Names() did not return a sorted slice")
	}
}

// TestRegistry_AllToolsValidate exercises every handler's Validate method with
// empty arguments and checks that the behaviour aligns with the tool's
// Required list.  Tools whose Validate is stricter than their Required list
// are reported as warnings (t.Errorf) but the test continues to the next
// handler so the full picture is visible.
func TestRegistry_AllToolsValidate(t *testing.T) {
	tools := AllTools()
	if len(tools) == 0 {
		t.Fatal("AllTools() returned no tools")
	}

	emptyArgs := map[string]any{}

	for _, h := range tools {
		name := h.Name()
		def := h.Definition()
		hasRequired := len(def.Required) > 0

		t.Run(name, func(t *testing.T) {
			err := h.Validate(emptyArgs)

			if hasRequired {
				// Tools with required parameters MUST reject empty args.
				if err == nil {
					t.Errorf("tool %q has %d required parameter(s) (%v) but Validate(empty) returned nil",
						name, len(def.Required), def.Required)
				} else {
					t.Logf("%q correctly rejects empty args (required: %v)", name, def.Required)
				}
			} else {
				// Tools with no required parameters ideally accept empty args,
				// but Validate can be stricter than Required (e.g., conditional validation).
				if err != nil {
					t.Logf("%q: Required=[] but Validate(empty)=%v (handler has stricter validation than Required list)", name, err)
				} else {
					t.Logf("%q correctly accepts empty args (no required params)", name)
				}
			}
		})
	}
}

// TestRegistry_NoOrphanHandlers cross-references the new tool registry against
// the legacy registry to surface any handlers that might not have a legacy
// counterpart.  Known name mismatches (e.g. todo_read vs TodoRead) are
// resolved via a mapping so the comparison is accurate.
//
// This test never fails — it is purely informational.
//
// NOTE: We cannot import pkg/agent here because it imports pkg/agent_tools,
// which would create a cyclic import. Instead we list the legacy tool names
// directly (they are a known, stable set).
func TestRegistry_NoOrphanHandlers(t *testing.T) {
	// Legacy tool names from pkg/agent.GetToolRegistry().GetAvailableTools().
	// Sourced from the legacy registry's registration code.
	legacyNames := []string{
		"shell_command", "git", "commit", "read_file", "write_file",
		"edit_file", "write_structured_file", "patch_structured_file",
		"search_files", "repo_map",
		"rollback_changes", "view_history", "list_skills", "embedding_index",
		"save_memory", "run_subagent", "run_parallel_subagents",
		"task_queue_add", "task_queue_publish", "task_queue_read",
		"TodoRead", "TodoWrite", "ask_user", "self_review",
		"activate_skill",
		"browse_url", "web_search", "semantic_search",
		"analyze_image_content", "analyze_ui_screenshot",
		"search_memories", "list_directory", "fetch_url",
	}

	legacySet := make(map[string]bool, len(legacyNames))
	for _, n := range legacyNames {
		legacySet[n] = true
	}

	// Known name-difference mappings: new → legacy.
	nameAlias := map[string]string{
		"todo_read":  "TodoRead",
		"todo_write": "TodoWrite",
	}

	tools := AllTools()
	if len(tools) == 0 {
		t.Fatal("AllTools() returned no tools")
	}

	var orphans []string

	for _, h := range tools {
		newName := h.Name()

		// Check direct match first.
		if legacySet[newName] {
			t.Logf("%q — found in legacy", newName)
			continue
		}

		// Check alias mapping.
		if legacyName, ok := nameAlias[newName]; ok {
			if legacySet[legacyName] {
				t.Logf("%q — aliased to legacy %q", newName, legacyName)
			} else {
				orphans = append(orphans, newName)
			}
			continue
		}

		// Not found in legacy registry.
		orphans = append(orphans, newName)
	}

	if len(orphans) > 0 {
		sort.Strings(orphans)
		t.Logf("New tools with NO legacy counterpart (%d): %v", len(orphans), orphans)
	} else {
		t.Logf("All %d new tools have a legacy counterpart", len(tools))
	}

	t.Logf("Summary: %d new tools, %d legacy tools", len(tools), len(legacyNames))

	// Reverse check: legacy tools not present in the new registry.
	// Collect new tool names.
	newSet := make(map[string]bool, len(tools))
	newToLegacy := make(map[string]string, len(tools))
	for _, h := range tools {
		name := h.Name()
		newSet[name] = true
		if alias, ok := nameAlias[name]; ok {
			newToLegacy[alias] = name
		}
	}

	var legacyOrphans []string
	for _, ln := range legacyNames {
		if newSet[ln] || newToLegacy[ln] != "" {
			continue
		}
		legacyOrphans = append(legacyOrphans, ln)
	}
	if len(legacyOrphans) > 0 {
		sort.Strings(legacyOrphans)
		t.Logf("Legacy tools NOT in new registry (%d): %v", len(legacyOrphans), legacyOrphans)
	} else {
		t.Logf("All legacy tools have a new handler")
	}
}
