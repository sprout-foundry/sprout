//go:build !js

package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// =============================================================================
// agent_modes.go — formatSpawnLine
// =============================================================================

func TestFormatSpawnLine_NilAgent(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := formatSpawnLine(nil, 0, "coder", 0, "")
	if !strings.Contains(got, "spawned") {
		t.Errorf("expected 'spawned' in output, got: %q", got)
	}
	// No provider/model suffix when agent is nil
	if strings.Contains(got, "·") {
		t.Errorf("should not have provider suffix with nil agent, got: %q", got)
	}
}

func TestFormatSpawnLine_WithAgent(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	t.Setenv("NO_COLOR", "1")
	got := formatSpawnLine(a, 1, "coder", 0, "")
	if !strings.Contains(got, "spawned") {
		t.Errorf("expected 'spawned' in output, got: %q", got)
	}
	// Should have indent for depth 1
	if !strings.HasPrefix(got, "    ") {
		t.Errorf("expected indent for depth 1, got: %q", got)
	}
}

// TestFormatSpawnLine_IncludesMaxContext pins the new context-budget
// suffix on the spawn line: when monitorProgress has emitted at least
// one snapshot the line gets "· 128.0k ctx" appended so the user can
// see how much context the subagent has to work with before it does
// anything. With maxCtx=0 the suffix is dropped — the line degrades to
// the original "(provider · model)" form.
func TestFormatSpawnLine_IncludesMaxContext(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}
	t.Setenv("NO_COLOR", "1")

	withCtx := formatSpawnLine(a, 1, "coder", 128000, "")
	if !strings.Contains(withCtx, "128.0k ctx") {
		t.Errorf("expected '128.0k ctx' suffix when maxCtx is known, got: %q", withCtx)
	}

	withoutCtx := formatSpawnLine(a, 1, "coder", 0, "")
	if strings.Contains(withoutCtx, "ctx)") {
		t.Errorf("should not have ctx suffix when maxCtx is 0, got: %q", withoutCtx)
	}
}

// =============================================================================
// agent_modes.go — buildQueueTaskQuery (coverage for uncovered branches)
// =============================================================================

func TestBuildQueueTaskQuery_CoverageExtra(t *testing.T) {
	task := tools.Task{
		ID:           "task-cov",
		Title:        "coverage task",
		Description:  "cov description",
		WorkingDir:   "/tmp/cov",
		Persona:      "coder",
		Priority:     "high",
		ParentTaskID: "parent-cov",
	}
	query := buildQueueTaskQuery(task)
	if !strings.Contains(query, "coverage task") {
		t.Errorf("expected 'coverage task' in query, got: %q", query)
	}
	if !strings.Contains(query, "cov description") {
		t.Errorf("expected 'cov description' in query, got: %q", query)
	}
	if !strings.Contains(query, "/tmp/cov") {
		t.Errorf("expected '/tmp/cov' in query, got: %q", query)
	}
	if !strings.Contains(query, "coder") {
		t.Errorf("expected 'coder' in query, got: %q", query)
	}
	if !strings.Contains(query, "parent-cov") {
		t.Errorf("expected 'parent-cov' in query, got: %q", query)
	}
	if !strings.Contains(query, "run_subagent") {
		t.Errorf("expected 'run_subagent' hint when persona specified, got: %q", query)
	}
}

// =============================================================================
// service_env.go — captureAPIKeysFromEnv
// =============================================================================

func TestCaptureAPIKeysFromEnv_Coverage(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv()
	t.Setenv("MY_API_KEY", "secret123")
	t.Setenv("GITHUB_TOKEN", "gh_abc")
	t.Setenv("NORMAL_VAR", "value")

	matches := captureAPIKeysFromEnv()
	foundAPIKey := false
	foundToken := false
	foundNormal := false
	for _, m := range matches {
		if strings.HasPrefix(m, "MY_API_KEY=") {
			foundAPIKey = true
		}
		if strings.HasPrefix(m, "GITHUB_TOKEN=") {
			foundToken = true
		}
		if strings.HasPrefix(m, "NORMAL_VAR=") {
			foundNormal = true
		}
	}
	if !foundAPIKey {
		t.Error("expected MY_API_KEY in capture results")
	}
	if !foundToken {
		t.Error("expected GITHUB_TOKEN in capture results")
	}
	if foundNormal {
		t.Error("NORMAL_VAR should NOT be captured (doesn't match API key pattern)")
	}
}

// =============================================================================
// log_redirect.go — redirectGoLogToWorkspace
// =============================================================================

func TestRedirectGoLogToWorkspace_Coverage(t *testing.T) {
	// Change to a temp dir so we don't pollute the real workspace
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	restore, err := redirectGoLogToWorkspace()
	if err != nil {
		t.Fatalf("redirectGoLogToWorkspace() error: %v", err)
	}
	defer func() {
		restore()
		// Clean up .sprout directory
		_ = os.RemoveAll(".sprout")
	}()

	// Verify .sprout/workspace.log was created
	if _, err := os.Stat(".sprout/workspace.log"); os.IsNotExist(err) {
		t.Error("expected .sprout/workspace.log to exist after redirectGoLogToWorkspace")
	}
}

// =============================================================================
// pid_alive_unix.go — isPIDAlive
// =============================================================================

func TestIsPIDAlive_Coverage(t *testing.T) {
	// PID 1 (init) should always be alive on Linux
	if !isPIDAlive(1) {
		t.Error("PID 1 should be alive")
	}
	// PID 0 should return false
	if isPIDAlive(0) {
		t.Error("PID 0 should return false")
	}
	// Negative PID should return false
	if isPIDAlive(-1) {
		t.Error("negative PID should return false")
	}
	// Very large PID that likely doesn't exist
	if isPIDAlive(999999999) {
		t.Error("very large PID should return false")
	}
}

// =============================================================================
// service_env.go — generateServiceEnvFile
// =============================================================================

func TestGenerateServiceEnvFile_NoKeys_Coverage(t *testing.T) {
	tmpDir := t.TempDir()
	// Ensure no matching env vars are set
	os.Unsetenv("MY_API_KEY")
	os.Unsetenv("GITHUB_TOKEN")

	err := generateServiceEnvFile(tmpDir)
	if err != nil {
		t.Fatalf("generateServiceEnvFile() error: %v", err)
	}

	// Should create an empty file
	path := tmpDir + "/.sprout/service.env"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected service.env to be created even with no keys")
	}
}

// =============================================================================
// first_run_hint.go — maybeShowFirstRunHint
// =============================================================================

func TestMaybeShowFirstRunHint_NoPanic_Coverage(t *testing.T) {
	// Ensure no panic. The function has many early returns,
	// so in test env it likely returns silently.
	maybeShowFirstRunHint()
}

// =============================================================================
// agent_modes.go — formatRunSubagentPreview with agent
// =============================================================================

func TestFormatRunSubagentPreview_WithAgent_Coverage(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	got := formatRunSubagentPreview(a, `{"persona":"coder"}`)
	// Should contain the persona name
	if !strings.Contains(got, "coder") {
		t.Errorf("expected 'coder' in preview, got: %q", got)
	}
}

func TestFormatRunSubagentPreview_InvalidJSON_Coverage(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	got := formatRunSubagentPreview(a, `not valid json`)
	if got != "" {
		t.Errorf("expected empty for invalid JSON, got: %q", got)
	}
}

func TestFormatRunSubagentPreview_NoPersona_Coverage(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	got := formatRunSubagentPreview(a, `{"persona":""}`)
	if got != "" {
		t.Errorf("expected empty for empty persona, got: %q", got)
	}
}

// =============================================================================
// agent_modes.go — formatToolPreview with agent
// =============================================================================

func TestFormatToolPreview_WithAgent_RunSubagent_Coverage(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	got := formatToolPreview(a, "run_subagent", `{"persona":"coder"}`, 0)
	if !strings.Contains(got, "coder") {
		t.Errorf("expected 'coder' in preview, got: %q", got)
	}
}

func TestFormatToolPreview_WithAgent_RunParallelSubagents_Coverage(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	got := formatToolPreview(a, "run_parallel_subagents", `{"subagents":["a","b","c"]}`, 0)
	if !strings.Contains(got, "3 tasks") {
		t.Errorf("expected '3 tasks' in preview, got: %q", got)
	}
}

// =============================================================================
// agent_modes.go — printPerTurnSummary
// =============================================================================

func TestPrintPerTurnSummary_NonTTY_Coverage(t *testing.T) {
	// In test env, stderr is not a TTY, so printPerTurnSummary should not output.
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	printPerTurnSummary(nil, time.Now().Add(-time.Second), 0, 0, 0)

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	if err != nil {
		t.Fatalf("read from pipe: %v", err)
	}
	out := buf.String()
	if len(out) != 0 {
		t.Errorf("expected no output in non-TTY env, got: %q", out)
	}
}

// =============================================================================
// first_run_hint.go — saveFirstRunState with error path
// =============================================================================

func TestSaveFirstRunState_WriteError_Coverage(t *testing.T) {
	// Use a path inside /dev/null which is writable but can't hold files
	state := &sproutState{SeenFirstRunHint: []string{"/test"}}
	err := saveFirstRunState("/dev/null/state.json", state)
	if err == nil {
		t.Error("expected error when saving to /dev/null")
	}
}

// =============================================================================
// first_run_hint.go — loadFirstRunState with invalid JSON
// =============================================================================

func TestLoadFirstRunState_InvalidJSON_Coverage(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/state.json"
	if err := os.WriteFile(path, []byte("{invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadFirstRunState(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
