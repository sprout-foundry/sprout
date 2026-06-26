package agent

import (
	"testing"
)

// TestNewDefaultToolRegistry_Count verifies that the default registry contains
// the expected number of tools after the refactor.
func TestNewDefaultToolRegistry_Count(t *testing.T) {
	registry := newDefaultToolRegistry()
	expectedCount := 36
	if len(registry.tools) != expectedCount {
		t.Errorf("expected %d registered tools, got %d", expectedCount, len(registry.tools))
	}
}

// TestNewDefaultToolRegistry_AllToolsRegistered verifies that every expected
// tool is registered in the default registry.
func TestNewDefaultToolRegistry_AllToolsRegistered(t *testing.T) {
	registry := newDefaultToolRegistry()

	expectedTools := []string{
		"shell_command",
		"git",
		"commit",
		"create_pull_request",
		"read_file",
		"write_file",
		"edit_file",
		"write_structured_file",
		"patch_structured_file",
		"TodoWrite",
		"TodoRead",
		"ask_user",
		"run_subagent",
		"run_parallel_subagents",
		"search_files",
		"request_clarification",
		"respond_clarification",
		"repo_map",
		"web_search",
		"fetch_url",
		"browse_url",
		"analyze_ui_screenshot",
		"analyze_image_content",
		"view_history",
		"rollback_changes",
		"self_review",
		"list_skills",
		"activate_skill",
		"manage_memory",
		"manage_settings",
		"task_queue",
		"run_automate",
		"list_automate_workflows",
		"list_changes",
		"recover_file",
		"revert_my_changes",
	}

	for _, toolName := range expectedTools {
		config, found := registry.GetToolConfig(toolName)
		if !found {
			t.Errorf("tool %q not found in default registry", toolName)
			continue
		}
		if config.Name != toolName {
			t.Errorf("tool name mismatch: expected %q, got %q", toolName, config.Name)
		}
		if config.Description == "" {
			t.Errorf("tool %q has empty description", toolName)
		}
		if config.Handler == nil {
			t.Errorf("tool %q has nil handler", toolName)
		}
	}
}

// TestNewDefaultToolRegistry_ToolParameters verifies that each tool has the
// correct number of parameters and that required parameters are properly marked.
func TestNewDefaultToolRegistry_ToolParameters(t *testing.T) {
	registry := newDefaultToolRegistry()

	// Each tool's expected parameter count and required parameter count.
	type toolParamExpectation struct {
		paramCount    int
		requiredCount int
		requiredNames []string
	}

	expectations := map[string]toolParamExpectation{
		"shell_command":           {paramCount: 5, requiredCount: 0, requiredNames: []string{}},
		"git":                     {paramCount: 2, requiredCount: 1, requiredNames: []string{"operation"}},
		"commit":                  {paramCount: 2, requiredCount: 0, requiredNames: []string{}},
		"create_pull_request":     {paramCount: 6, requiredCount: 1, requiredNames: []string{"title"}},
		"read_file":               {paramCount: 2, requiredCount: 1, requiredNames: []string{"path"}},
		"write_file":              {paramCount: 2, requiredCount: 2, requiredNames: []string{"path", "content"}},
		"edit_file":               {paramCount: 3, requiredCount: 3, requiredNames: []string{"path", "old_str", "new_str"}},
		"write_structured_file":   {paramCount: 4, requiredCount: 2, requiredNames: []string{"path", "data"}},
		"patch_structured_file":   {paramCount: 5, requiredCount: 1, requiredNames: []string{"path"}},
		"TodoWrite":               {paramCount: 1, requiredCount: 1, requiredNames: []string{"todos"}},
		"TodoRead":                {paramCount: 0, requiredCount: 0, requiredNames: []string{}},
		"ask_user":                {paramCount: 5, requiredCount: 1, requiredNames: []string{"question"}},
		"run_subagent":            {paramCount: 5, requiredCount: 2, requiredNames: []string{"prompt", "persona"}},
		"run_parallel_subagents":  {paramCount: 1, requiredCount: 1, requiredNames: []string{"subagents"}},
		"search_files":            {paramCount: 6, requiredCount: 1, requiredNames: []string{"search_pattern"}},
		"repo_map":                {paramCount: 1, requiredCount: 0, requiredNames: []string{}},
		"web_search":              {paramCount: 1, requiredCount: 1, requiredNames: []string{"query"}},
		"fetch_url":               {paramCount: 1, requiredCount: 1, requiredNames: []string{"url"}},
		"browse_url":              {paramCount: 20, requiredCount: 1, requiredNames: []string{"url"}},
		"analyze_ui_screenshot":   {paramCount: 4, requiredCount: 1, requiredNames: []string{"image_path"}},
		"analyze_image_content":   {paramCount: 3, requiredCount: 1, requiredNames: []string{"image_path"}},
		"view_history":            {paramCount: 4, requiredCount: 0, requiredNames: []string{}},
		"rollback_changes":        {paramCount: 3, requiredCount: 0, requiredNames: []string{}},
		"self_review":             {paramCount: 1, requiredCount: 0, requiredNames: []string{}},
		"list_skills":             {paramCount: 0, requiredCount: 0, requiredNames: []string{}},
		"activate_skill":          {paramCount: 1, requiredCount: 1, requiredNames: []string{"skill_id"}},
		"manage_memory":           {paramCount: 6, requiredCount: 1, requiredNames: []string{"operation"}},
		"manage_settings":         {paramCount: 4, requiredCount: 1, requiredNames: []string{"operation"}},
		"task_queue":              {paramCount: 11, requiredCount: 1, requiredNames: []string{"operation"}},
		"run_automate":            {paramCount: 1, requiredCount: 1, requiredNames: []string{"workflow"}},
		"list_automate_workflows": {paramCount: 0, requiredCount: 0, requiredNames: []string{}},
	}

	for toolName, expected := range expectations {
		config, found := registry.GetToolConfig(toolName)
		if !found {
			t.Errorf("tool %q not found in registry (cannot validate parameters)", toolName)
			continue
		}

		if len(config.Parameters) != expected.paramCount {
			t.Errorf("tool %q: expected %d parameters, got %d",
				toolName, expected.paramCount, len(config.Parameters))
		}

		// Count actual required parameters.
		actualRequired := 0
		for _, p := range config.Parameters {
			if p.Required {
				actualRequired++
			}
		}
		if actualRequired != expected.requiredCount {
			t.Errorf("tool %q: expected %d required parameters, got %d",
				toolName, expected.requiredCount, actualRequired)
		}

		// Verify each expected required param is actually marked required.
		for _, reqName := range expected.requiredNames {
			found := false
			for _, p := range config.Parameters {
				if p.Name == reqName {
					found = true
					if !p.Required {
						t.Errorf("tool %q: parameter %q should be required but isn't",
							toolName, reqName)
					}
					break
				}
			}
			if !found {
				t.Errorf("tool %q: required parameter %q not found in parameter list",
					toolName, reqName)
			}
		}
	}
}

// TestNewDefaultToolRegistry_ParameterAlternatives verifies that tools with
// alternative parameter names have the correct alternatives configured.
func TestNewDefaultToolRegistry_ParameterAlternatives(t *testing.T) {
	registry := newDefaultToolRegistry()

	type altCheck struct {
		tool         string
		paramName    string
		alternatives []string
	}

	tests := []altCheck{
		{"shell_command", "command", []string{"cmd"}},
		{"git", "operation", []string{"op"}},
		{"commit", "message", []string{"msg"}},
		{"commit", "notes", []string{"context", "extra_context"}},
		{"read_file", "path", []string{"file_path"}},
		{"write_file", "path", []string{"file_path"}},
		{"edit_file", "path", []string{"file_path"}},
		{"edit_file", "old_str", []string{"old_string"}},
		{"edit_file", "new_str", []string{"new_string"}},
		{"search_files", "search_pattern", []string{"pattern"}},
		{"search_files", "directory", []string{"root"}},
		{"ask_user", "question", []string{}},
		{"activate_skill", "skill_id", []string{"skill", "id"}},
		{"manage_memory", "name", []string{"title", "memory"}},
		{"rollback_changes", "file_path", []string{"filename"}},
		{"view_history", "file_filter", []string{"filename"}},
	}

	for _, tc := range tests {
		config, found := registry.GetToolConfig(tc.tool)
		if !found {
			t.Errorf("tool %q not found", tc.tool)
			continue
		}
		for _, p := range config.Parameters {
			if p.Name == tc.paramName {
				if len(p.Alternatives) != len(tc.alternatives) {
					t.Errorf("tool %q param %q: expected %d alternatives %v, got %d %v",
						tc.tool, tc.paramName,
						len(tc.alternatives), tc.alternatives,
						len(p.Alternatives), p.Alternatives)
				} else {
					for i, alt := range tc.alternatives {
						if i >= len(p.Alternatives) || p.Alternatives[i] != alt {
							t.Errorf("tool %q param %q: expected alternative %q at index %d, got %q",
								tc.tool, tc.paramName, alt, i,
								func() string {
									if i < len(p.Alternatives) {
										return p.Alternatives[i]
									}
									return "<missing>"
								}())
						}
					}
				}
				break
			}
		}
	}
}

// TestNewDefaultToolRegistry_ParameterTypes verifies that each parameter has
// the correct type declaration.
func TestNewDefaultToolRegistry_ParameterTypes(t *testing.T) {
	registry := newDefaultToolRegistry()

	type typeCheck struct {
		tool         string
		paramName    string
		expectedType string
	}

	tests := []typeCheck{
		{"shell_command", "command", "string"},
		{"shell_command", "background", "boolean"},
		{"shell_command", "check_background", "string"},
		{"shell_command", "stop_background", "string"},
		{"git", "operation", "string"},
		{"git", "args", "string"},
		{"commit", "message", "string"},
		{"commit", "notes", "string"},
		{"create_pull_request", "title", "string"},
		{"create_pull_request", "body", "string"},
		{"create_pull_request", "base", "string"},
		{"create_pull_request", "head", "string"},
		{"create_pull_request", "draft", "boolean"},
		{"create_pull_request", "repo_dir", "string"},
		{"read_file", "path", "string"},
		{"read_file", "view_range", "array"},
		{"write_file", "path", "string"},
		{"write_file", "content", "string"},
		{"edit_file", "path", "string"},
		{"edit_file", "old_str", "string"},
		{"edit_file", "new_str", "string"},
		{"write_structured_file", "path", "string"},
		{"write_structured_file", "format", "string"},
		{"write_structured_file", "data", "object"},
		{"write_structured_file", "schema", "object"},
		{"patch_structured_file", "path", "string"},
		{"patch_structured_file", "format", "string"},
		{"patch_structured_file", "patch_ops", "array"},
		{"patch_structured_file", "schema", "object"},
		{"patch_structured_file", "data", "object"},
		{"TodoWrite", "todos", "array"},
		{"ask_user", "question", "string"},
		{"run_subagent", "prompt", "string"},
		{"run_subagent", "persona", "string"},
		{"run_subagent", "context", "string"},
		{"run_subagent", "files", "string"},
		{"run_subagent", "working_dir", "string"},
		{"run_parallel_subagents", "subagents", "array"},
		{"search_files", "search_pattern", "string"},
		{"search_files", "directory", "string"},
		{"search_files", "file_glob", "string"},
		{"search_files", "case_sensitive", "boolean"},
		{"search_files", "max_results", "integer"},
		{"search_files", "max_bytes", "integer"},
		{"repo_map", "directory", "string"},
		{"web_search", "query", "string"},
		{"fetch_url", "url", "string"},
		{"browse_url", "url", "string"},
		{"browse_url", "action", "string"},
		{"browse_url", "screenshot_path", "string"},
		{"browse_url", "session_id", "string"},
		{"browse_url", "persist_session", "boolean"},
		{"browse_url", "close_session", "boolean"},
		{"browse_url", "viewport_width", "integer"},
		{"browse_url", "viewport_height", "integer"},
		{"browse_url", "user_agent", "string"},
		{"browse_url", "wait_for_selector", "string"},
		{"browse_url", "wait_timeout_ms", "integer"},
		{"browse_url", "steps", "array"},
		{"browse_url", "capture_selectors", "array"},
		{"browse_url", "capture_dom", "boolean"},
		{"browse_url", "capture_text", "boolean"},
		{"browse_url", "include_console", "boolean"},
		{"browse_url", "capture_network", "boolean"},
		{"browse_url", "capture_storage", "boolean"},
		{"browse_url", "capture_cookies", "boolean"},
		{"browse_url", "response_max_chars", "integer"},
		{"analyze_ui_screenshot", "image_path", "string"},
		{"analyze_ui_screenshot", "analysis_prompt", "string"},
		{"analyze_ui_screenshot", "viewport_width", "integer"},
		{"analyze_ui_screenshot", "viewport_height", "integer"},
		{"analyze_image_content", "image_path", "string"},
		{"analyze_image_content", "analysis_prompt", "string"},
		{"analyze_image_content", "analysis_mode", "string"},
		{"view_history", "limit", "integer"},
		{"view_history", "file_filter", "string"},
		{"view_history", "since", "string"},
		{"view_history", "show_content", "boolean"},
		{"rollback_changes", "revision_id", "string"},
		{"rollback_changes", "file_path", "string"},
		{"rollback_changes", "confirm", "boolean"},
		{"self_review", "revision_id", "string"},
		{"activate_skill", "skill_id", "string"},
		{"manage_memory", "operation", "string"},
		{"manage_memory", "name", "string"},
		{"manage_memory", "content", "string"},
		{"manage_memory", "query", "string"},
		{"manage_memory", "threshold", "number"},
		{"manage_memory", "top_k", "integer"},
		{"manage_settings", "operation", "string"},
		{"manage_settings", "key", "string"},
		{"manage_settings", "value", "string"},
		{"manage_settings", "provider", "string"},
		{"task_queue", "operation", "string"},
		{"task_queue", "status", "string"},
		{"task_queue", "limit", "integer"},
		{"task_queue", "title", "string"},
		{"task_queue", "description", "string"},
		{"task_queue", "priority", "string"},
		{"task_queue", "working_dir", "string"},
		{"task_queue", "persona", "string"},
		{"task_queue", "task_id", "string"},
		{"task_queue", "result", "string"},
		{"task_queue", "subtasks", "array"},
	}

	for _, tc := range tests {
		config, found := registry.GetToolConfig(tc.tool)
		if !found {
			t.Errorf("tool %q not found", tc.tool)
			continue
		}
		paramFound := false
		for _, p := range config.Parameters {
			if p.Name == tc.paramName {
				paramFound = true
				if p.Type != tc.expectedType {
					t.Errorf("tool %q param %q: expected type %q, got %q",
						tc.tool, tc.paramName, tc.expectedType, p.Type)
				}
				break
			}
		}
		if !paramFound {
			t.Errorf("tool %q: parameter %q not found", tc.tool, tc.paramName)
		}
	}
}
