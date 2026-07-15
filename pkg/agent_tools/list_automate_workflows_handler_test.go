package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestListAutomateWorkflowsHandler_UsesWorkspaceRoot verifies that the
// list_automate_workflows handler resolves automate/ against
// ToolEnv.WorkspaceRoot in daemon mode (SP-119). When the workspace
// directory contains an automate/ subdir with workflows, those should be
// returned. When it doesn't, the empty-activate-skill-hint message should
// be returned (not an error scanning a different directory).
func TestListAutomateWorkflowsHandler_UsesWorkspaceRoot(t *testing.T) {
	t.Parallel()

	// Build a workspace whose automate/ subdir contains one valid workflow.
	workspace := t.TempDir()
	automateDir := filepath.Join(workspace, "automate")
	if err := os.Mkdir(automateDir, 0755); err != nil {
		t.Fatalf("mkdir automate/: %v", err)
	}
	validJSON := `{
		"description": "Workspace-local workflow",
		"initial": {"prompt": "hi"}
	}`
	if err := os.WriteFile(filepath.Join(automateDir, "hello.json"), []byte(validJSON), 0644); err != nil {
		t.Fatalf("write hello.json: %v", err)
	}

	h := &listAutomateWorkflowsHandler{}
	env := ToolEnv{WorkspaceRoot: workspace}

	result, err := h.Execute(context.Background(), env, map[string]any{})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got IsError=true: %s", result.Output)
	}

	// Decode and verify the workflow was discovered from <workspace>/automate/.
	var parsed struct {
		Directory string `json:"directory"`
		Count     int    `json:"count"`
		Workflows []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"workflows"`
	}
	if err := json.Unmarshal([]byte(result.Output), &parsed); err != nil {
		t.Fatalf("decode output: %v\noutput was: %s", err, result.Output)
	}

	if parsed.Count != 1 {
		t.Fatalf("expected 1 workflow, got %d. Output: %s", parsed.Count, result.Output)
	}
	if parsed.Workflows[0].Name != "hello.json" {
		t.Errorf("expected hello.json, got %q", parsed.Workflows[0].Name)
	}
	if !strings.Contains(parsed.Directory, automateDir) {
		t.Errorf("expected directory %q to contain %q", parsed.Directory, automateDir)
	}
}

// TestListAutomateWorkflowsHandler_EmptyWorkspaceActivatesSkillHint verifies
// the friendly empty-state response when the workspace has no automate/
// directory.
func TestListAutomateWorkflowsHandler_EmptyWorkspaceActivatesSkillHint(t *testing.T) {
	t.Parallel()

	// Workspace exists but has no automate/ subdir.
	workspace := t.TempDir()

	h := &listAutomateWorkflowsHandler{}
	env := ToolEnv{WorkspaceRoot: workspace}

	result, err := h.Execute(context.Background(), env, map[string]any{})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected non-error empty-state, got IsError=true: %s", result.Output)
	}
	if !strings.Contains(result.Output, "No automate/ directory found") {
		t.Errorf("expected activate-skill hint in output, got: %s", result.Output)
	}
}

// TestListAutomateWorkflowsHandler_EmptyWorkspaceRootFallsBackToCwd
// verifies backwards-compat behavior: when ToolEnv.WorkspaceRoot is empty,
// the handler falls back to automate.Dir() (CWD-based), matching legacy CLI
// behavior where the user's shell CWD IS the workspace.
//
// IMPORTANT: do NOT add t.Parallel() here — os.Chdir mutates process-global
// state shared with every other test in the binary, and parallel tests
// running chdir simultaneously produce flaky failures. See
// pkg/agent_tools/zero_coverage_test.go:275 and
// pkg/agent_tools/background_process_signal_unix_test.go:27, 52 for the
// codebase convention on this.
func TestListAutomateWorkflowsHandler_EmptyWorkspaceRootFallsBackToCwd(t *testing.T) {
	// t.Parallel() intentionally omitted — see comment above.

	// Create a fake CWD-relative automate/ directory and chdir into it.
	tmp := t.TempDir()
	automateDir := filepath.Join(tmp, "automate")
	if err := os.Mkdir(automateDir, 0755); err != nil {
		t.Fatalf("mkdir automate/: %v", err)
	}
	validJSON := `{"description": "CWD workflow", "initial": {"prompt": "hi"}}`
	if err := os.WriteFile(filepath.Join(automateDir, "cwdonly.json"), []byte(validJSON), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	h := &listAutomateWorkflowsHandler{}
	env := ToolEnv{WorkspaceRoot: ""} // explicit empty — exercises the Dir() fallback

	result, err := h.Execute(context.Background(), env, map[string]any{})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success via CWD fallback, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "cwdonly.json") {
		t.Errorf("expected cwdonly.json in output, got: %s", result.Output)
	}
}
