//go:build !js

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// tickNext ticks the first unchecked item in the TODO file at path.
func tickNext(t *testing.T, path string) {
	t.Helper()
	path = filepath.Clean(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read TODO.md: %v", err)
	}
	updated, ok := tickOne(string(data))
	if !ok {
		return
	}
	if err := os.WriteFile(path, []byte(updated), 0600); err != nil { // #nosec G703 -- path is the test's own TempDir TODO file
		t.Fatalf("write TODO.md: %v", err)
	}
}

func runContinuation(t *testing.T, chatAgent *agent.Agent, cont *AgentWorkflowContinuation, exec QueryExecutor) ContinuationResult {
	t.Helper()
	cfg := &AgentWorkflowConfig{
		Initial:      &AgentWorkflowInitial{Prompt: "process items"},
		Continuation: cont,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	result, err := RunInitialContinuation(context.Background(), chatAgent, events.NewEventBus(), cfg, &WorkflowExecutionState{Version: 1}, exec)
	if err != nil {
		t.Fatalf("RunInitialContinuation: %v", err)
	}
	return result
}

func TestRunInitialContinuation_IdleTurnThenProgressContinues(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeContinuationTodo(t, dir, []string{"A", "B"})

	var prompts []string
	exec := func(_ context.Context, _ *agent.Agent, _ *events.EventBus, query string) error {
		prompts = append(prompts, query)
		if len(prompts) == 1 {
			return nil // idle: re-verified, did not commit or tick
		}
		tickNext(t, todoPath)
		return nil
	}

	result := runContinuation(t, newContinuationAgent(t), &AgentWorkflowContinuation{TodoFile: todoPath}, exec)

	if result.StopReason != ContinuationStopNoRunnableItems {
		t.Fatalf("StopReason = %q, want %q (one idle turn must not stop the run)", result.StopReason, ContinuationStopNoRunnableItems)
	}
	if len(prompts) != 3 {
		t.Fatalf("turns = %d, want 3 (idle, tick A, tick B)", len(prompts))
	}
	if prompts[0] != DefaultContinuationPrompt {
		t.Errorf("turn 1 prompt = %q, want the default prompt", prompts[0])
	}
	if prompts[1] != DefaultContinuationIdlePrompt {
		t.Errorf("turn 2 prompt = %q, want the idle prompt after an idle turn", prompts[1])
	}
	if prompts[2] != DefaultContinuationPrompt {
		t.Errorf("turn 3 prompt = %q, want the default prompt again after progress", prompts[2])
	}
}

func TestRunInitialContinuation_IdleCountResetsOnProgress(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeContinuationTodo(t, dir, []string{"A", "B", "C"})

	// idle, tick, idle, tick, idle, tick: never two idle turns in a row, so
	// the default limit of 2 is never reached.
	turns := 0
	exec := func(context.Context, *agent.Agent, *events.EventBus, string) error {
		turns++
		if turns%2 == 0 {
			tickNext(t, todoPath)
		}
		return nil
	}

	result := runContinuation(t, newContinuationAgent(t), &AgentWorkflowContinuation{TodoFile: todoPath}, exec)

	if result.StopReason != ContinuationStopNoRunnableItems {
		t.Fatalf("StopReason = %q, want %q (progress must reset the idle count)", result.StopReason, ContinuationStopNoRunnableItems)
	}
	if turns != 6 {
		t.Errorf("turns = %d, want 6", turns)
	}
}

func TestRunInitialContinuation_ConfiguredMaxIdleTurns(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeContinuationTodo(t, dir, []string{"A"})

	turns := 0
	exec := func(context.Context, *agent.Agent, *events.EventBus, string) error {
		turns++
		return nil
	}

	result := runContinuation(t, newContinuationAgent(t), &AgentWorkflowContinuation{TodoFile: todoPath, MaxIdleTurns: 4}, exec)

	if result.StopReason != ContinuationStopNoProgress {
		t.Fatalf("StopReason = %q, want %q", result.StopReason, ContinuationStopNoProgress)
	}
	if turns != 4 {
		t.Errorf("turns = %d, want 4 (the configured idle limit)", turns)
	}
}

func TestRunInitialContinuation_WorkingTreeEditsAreNotProgress(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeContinuationTodo(t, dir, []string{"A"})
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "add", "TODO.md")
	runGit(t, dir, "commit", "-m", "initial")

	client := agent.NewScriptedClient()
	if err := client.SetModel("test:test"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	chatAgent := newTestLoopAgent(t, client)
	chatAgent.SetWorkspaceRoot(dir)

	// Every turn edits files, stages them and even rewrites the item's
	// text, but never commits or ticks: the loop must still stop.
	turns := 0
	exec := func(context.Context, *agent.Agent, *events.EventBus, string) error {
		turns++
		f := filepath.Join(dir, "wip"+strconv.Itoa(turns)+".txt")
		if err := os.WriteFile(f, []byte("in progress"), 0644); err != nil {
			t.Fatalf("write wip file: %v", err)
		}
		runGit(t, dir, "add", f)
		if err := os.WriteFile(todoPath, []byte("## Test Items\n- [ ] A\n      note: still in progress\n"), 0644); err != nil {
			t.Fatalf("rewrite TODO.md: %v", err)
		}
		return nil
	}

	result := runContinuation(t, chatAgent, &AgentWorkflowContinuation{TodoFile: todoPath}, exec)

	if result.StopReason != ContinuationStopNoProgress {
		t.Fatalf("StopReason = %q, want %q (uncommitted edits are not progress)", result.StopReason, ContinuationStopNoProgress)
	}
	if turns != DefaultMaxIdleTurns {
		t.Errorf("turns = %d, want %d", turns, DefaultMaxIdleTurns)
	}
}

func TestValidate_DefaultsMaxIdleTurns(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Initial:      &AgentWorkflowInitial{Prompt: "process items"},
		Continuation: &AgentWorkflowContinuation{},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.Continuation.MaxIdleTurns != DefaultMaxIdleTurns {
		t.Errorf("MaxIdleTurns = %d, want default %d", cfg.Continuation.MaxIdleTurns, DefaultMaxIdleTurns)
	}
}
