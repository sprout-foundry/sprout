//go:build !js

package workflow

// continuation_config_test.go — pure-unit tests for the continuation
// config, TODO scanning, and progress classification helpers. The
// loop-level integration tests live in continuation_test.go.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// ReadTodoStatus counting
// =============================================================================

func TestReadTodoStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TODO.md")
	content := "# TODO\n- [ ] one\n- [x] two\n- [X] three\n- [ ] four\n  - [ ] nested\nnot an item\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	st := ReadTodoStatus(path)
	if st.Runnable != 3 {
		t.Errorf("Runnable = %d, want 3", st.Runnable)
	}
	if st.Done != 2 {
		t.Errorf("Done = %d, want 2", st.Done)
	}

	// Missing file → zero status, no panic.
	if got := ReadTodoStatus(filepath.Join(dir, "absent.md")); got.Runnable != 0 || got.Done != 0 {
		t.Errorf("missing file status = %+v, want zero", got)
	}
}

// =============================================================================
// madeContinuationProgress semantics
// =============================================================================

func TestMadeContinuationProgress(t *testing.T) {
	// New commit → progress.
	if !madeContinuationProgress(continuationProgress{head: "aaa", todoDone: 1}, continuationProgress{head: "bbb", todoDone: 1}) {
		t.Error("new HEAD should count as progress")
	}
	// New tick → progress.
	if !madeContinuationProgress(continuationProgress{head: "aaa", todoDone: 1}, continuationProgress{head: "aaa", todoDone: 2}) {
		t.Error("newly ticked item should count as progress")
	}
	// Nothing changed → no progress.
	if madeContinuationProgress(continuationProgress{head: "aaa", todoDone: 1}, continuationProgress{head: "aaa", todoDone: 1}) {
		t.Error("identical snapshots should not count as progress")
	}
	// No-git (empty HEAD both sides) → tick count decides.
	if !madeContinuationProgress(continuationProgress{head: "", todoDone: 0}, continuationProgress{head: "", todoDone: 1}) {
		t.Error("no-git tick should count as progress via fallback")
	}
	if madeContinuationProgress(continuationProgress{head: "", todoDone: 0}, continuationProgress{head: "", todoDone: 0}) {
		t.Error("no-git no-tick should not count as progress")
	}
	// HEAD unknown on one side (repo appeared/disappeared) → don't claim
	// commit progress; fall back to ticks.
	if madeContinuationProgress(continuationProgress{head: "", todoDone: 0}, continuationProgress{head: "bbb", todoDone: 0}) {
		t.Error("unknown-before HEAD should not claim commit progress")
	}
}

// =============================================================================
// config validation and defaults
// =============================================================================

func TestContinuationConfig_ValidateDefaultsAndErrors(t *testing.T) {
	t.Run("defaults applied", func(t *testing.T) {
		cfg := &AgentWorkflowConfig{
			Initial:      &AgentWorkflowInitial{Prompt: "go"},
			Continuation: &AgentWorkflowContinuation{},
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if cfg.Continuation.TodoFile != DefaultContinuationTodoFile {
			t.Errorf("TodoFile = %q, want %q", cfg.Continuation.TodoFile, DefaultContinuationTodoFile)
		}
		if cfg.Continuation.MaxContinuations != DefaultMaxContinuations {
			t.Errorf("MaxContinuations = %d, want %d", cfg.Continuation.MaxContinuations, DefaultMaxContinuations)
		}
		if !cfg.Continuation.IsEnabled() {
			t.Error("continuation should be enabled by default when the block is present")
		}
	})

	t.Run("requires initial prompt", func(t *testing.T) {
		cfg := &AgentWorkflowConfig{
			Continuation: &AgentWorkflowContinuation{},
		}
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected validation error when continuation has no initial prompt")
		}
	})

	t.Run("explicit false disables and drops the block", func(t *testing.T) {
		disabled := false
		cfg := &AgentWorkflowConfig{
			Initial:      &AgentWorkflowInitial{Prompt: "go"},
			Continuation: &AgentWorkflowContinuation{ContinueUntilDone: &disabled},
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if cfg.Continuation != nil {
			t.Error("disabled continuation should be dropped")
		}
	})
}

// TestAutomateWorkflowJSONParses loads the repository's own automate
// workflow config through the real loader so a schema drift between the
// runtime and automate/workflow.json fails here, not at run time.
func TestAutomateWorkflowJSONParses(t *testing.T) {
	path := filepath.Join("..", "..", "automate", "workflow.json")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("automate/workflow.json not present: %v", err)
	}
	cfg, err := LoadAgentWorkflowConfig(path)
	if err != nil {
		t.Fatalf("LoadAgentWorkflowConfig(%s): %v", path, err)
	}
	if cfg.Continuation == nil {
		t.Fatal("automate/workflow.json should configure continuation")
	}
	if !cfg.Continuation.IsEnabled() {
		t.Error("automate/workflow.json continuation should be enabled")
	}
	if cfg.Continuation.TodoFile != "TODO.md" {
		t.Errorf("continuation todo_file = %q, want TODO.md", cfg.Continuation.TodoFile)
	}
}

// =============================================================================
// helpers
// =============================================================================

// runGit runs a git command in dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
