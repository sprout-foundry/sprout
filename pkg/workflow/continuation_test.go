//go:build !js

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// writeContinuationTodo writes a TODO file with the given items, each a
// runnable `[ ]` line, and returns its path.
func writeContinuationTodo(t *testing.T, dir string, items []string) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("## Test Items\n")
	for _, item := range items {
		sb.WriteString("- [ ] " + item + "\n")
	}
	path := filepath.Join(dir, "TODO.md")
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		t.Fatalf("write TODO.md: %v", err)
	}
	return path
}

// newContinuationAgent builds a minimal agent for the continuation loop.
// The loop only reads the agent's workspace root and budget state, and
// passes it straight to the (test-provided) queryExecutor, so a scripted
// client is enough.
func newContinuationAgent(t *testing.T) *agent.Agent {
	t.Helper()
	client := agent.NewScriptedClient()
	client.SetModel("test:test")
	return newTestLoopAgent(t, client)
}

// tickOne writes "[x]" over the first unchecked marker, simulating a
// coordinator that completes exactly one item per turn. It returns false
// once no unchecked marker remains.
func tickOne(contents string) (string, bool) {
	idx := strings.Index(contents, "- [ ]")
	if idx < 0 {
		return contents, false
	}
	return contents[:idx] + "- [x]" + contents[idx+len("- [ ]"):], true
}

// =============================================================================
// Test 1: a coordinator that stops after each item still completes three items
// =============================================================================

func TestRunInitialContinuation_StopsAfterEachItem_CompletesThree(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeContinuationTodo(t, dir, []string{"Item one", "Item two", "Item three"})

	chatAgent := newContinuationAgent(t)
	eventBus := events.NewEventBus()

	// The scripted coordinator completes exactly one item per turn, then
	// "stops" (returns). The runtime must issue a continuation turn for
	// each remaining item.
	turns := 0
	queryExecutor := func(_ context.Context, _ *agent.Agent, _ *events.EventBus, query string) error {
		turns++
		if !strings.Contains(query, "TODO.md") {
			t.Errorf("continuation prompt should reference TODO.md, got %q", query)
		}
		data, err := os.ReadFile(todoPath)
		if err != nil {
			t.Fatalf("read TODO.md: %v", err)
		}
		updated, ok := tickOne(string(data))
		if !ok {
			return nil // nothing left to do — coordinator no-ops
		}
		if err := os.WriteFile(todoPath, []byte(updated), 0644); err != nil {
			t.Fatalf("write TODO.md: %v", err)
		}
		return nil
	}

	cfg := &AgentWorkflowConfig{
		Initial:      &AgentWorkflowInitial{Prompt: "process items"},
		Continuation: &AgentWorkflowContinuation{TodoFile: todoPath},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	result, err := RunInitialContinuation(context.Background(), chatAgent, eventBus, cfg, &WorkflowExecutionState{Version: 1}, queryExecutor)
	if err != nil {
		t.Fatalf("RunInitialContinuation: %v", err)
	}
	if result.StopReason != ContinuationStopNoRunnableItems {
		t.Errorf("StopReason = %q, want %q", result.StopReason, ContinuationStopNoRunnableItems)
	}
	if result.Continuations != 3 {
		t.Errorf("Continuations = %d, want 3 (one per item)", result.Continuations)
	}
	if turns != 3 {
		t.Errorf("queryExecutor calls = %d, want 3", turns)
	}
	got := countChecked(t, todoPath)
	if got != 3 {
		t.Errorf("checked items = %d, want 3", got)
	}
	if result.RunnableItems != 0 {
		t.Errorf("RunnableItems = %d, want 0", result.RunnableItems)
	}
}

// =============================================================================
// Test 2: only non-runnable items remain → stop with no turns
// =============================================================================

func TestRunInitialContinuation_NoRunnableItems_NoTurns(t *testing.T) {
	dir := t.TempDir()
	// TODO file has no `[ ]` items — everything already ticked.
	todoPath := filepath.Join(dir, "TODO.md")
	content := "## Done\n- [x] already done\n"
	if err := os.WriteFile(todoPath, []byte(content), 0644); err != nil {
		t.Fatalf("write TODO.md: %v", err)
	}

	chatAgent := newContinuationAgent(t)
	calls := 0
	queryExecutor := func(context.Context, *agent.Agent, *events.EventBus, string) error {
		calls++
		return nil
	}

	cfg := &AgentWorkflowConfig{
		Initial:      &AgentWorkflowInitial{Prompt: "process items"},
		Continuation: &AgentWorkflowContinuation{TodoFile: todoPath},
	}
	_ = cfg.Validate()

	result, err := RunInitialContinuation(context.Background(), chatAgent, events.NewEventBus(), cfg, &WorkflowExecutionState{Version: 1}, queryExecutor)
	if err != nil {
		t.Fatalf("RunInitialContinuation: %v", err)
	}
	if result.StopReason != ContinuationStopNoRunnableItems {
		t.Errorf("StopReason = %q, want %q", result.StopReason, ContinuationStopNoRunnableItems)
	}
	if result.Continuations != 0 || calls != 0 {
		t.Errorf("expected zero turns when nothing runnable, got continuations=%d calls=%d", result.Continuations, calls)
	}
}

// =============================================================================
// Test 3: a run whose only remaining items are skipped stops after one
// no-progress turn, and the stop reason is recorded
// =============================================================================

func TestRunInitialContinuation_SkippedItem_StopsAfterOneNoProgressTurn(t *testing.T) {
	dir := t.TempDir()
	// One runnable item the coordinator never ticks (e.g. permanently
	// skipped) — the turn makes no progress.
	todoPath := writeContinuationTodo(t, dir, []string{"Permanently skipped item"})

	chatAgent := newContinuationAgent(t)
	turns := 0
	queryExecutor := func(context.Context, *agent.Agent, *events.EventBus, string) error {
		turns++
		return nil // does nothing: no tick, no commit
	}

	cfg := &AgentWorkflowConfig{
		Initial:      &AgentWorkflowInitial{Prompt: "process items"},
		Continuation: &AgentWorkflowContinuation{TodoFile: todoPath},
	}
	_ = cfg.Validate()

	result, err := RunInitialContinuation(context.Background(), chatAgent, events.NewEventBus(), cfg, &WorkflowExecutionState{Version: 1}, queryExecutor)
	if err != nil {
		t.Fatalf("RunInitialContinuation: %v", err)
	}
	if result.StopReason != ContinuationStopNoProgress {
		t.Errorf("StopReason = %q, want %q", result.StopReason, ContinuationStopNoProgress)
	}
	if result.Continuations != 1 {
		t.Errorf("Continuations = %d, want exactly 1 (stop after first no-progress turn)", result.Continuations)
	}
	if turns != 1 {
		t.Errorf("queryExecutor calls = %d, want 1 — must not loop forever", turns)
	}
	if result.RunnableItems != 1 {
		t.Errorf("RunnableItems = %d, want 1 (item still open)", result.RunnableItems)
	}

	// The stop reason must be recordable on a session record.
	sessionPath := filepath.Join(dir, "session.json")
	if err := os.WriteFile(sessionPath, []byte(`{"workflow":"workflow.json","pid":1,"started_at":"2026-01-01T00:00:00Z","kind":"automate"}`), 0600); err != nil {
		t.Fatalf("write session record: %v", err)
	}
	if err := automate.RecordSessionStopReason(sessionPath, string(result.StopReason), result.Continuations, result.RunnableItems); err != nil {
		t.Fatalf("RecordSessionStopReason: %v", err)
	}
	got, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session record: %v", err)
	}
	if !strings.Contains(string(got), `"stop_reason": "no_progress"`) {
		t.Errorf("session record missing stop_reason=no_progress:\n%s", got)
	}
}

// =============================================================================
// Test 4: no-progress stops regardless of how many items remain
// =============================================================================

func TestRunInitialContinuation_NoProgressStopsEvenWithManyItems(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeContinuationTodo(t, dir, []string{"A", "B", "C", "D", "E"})

	chatAgent := newContinuationAgent(t)
	turns := 0
	queryExecutor := func(context.Context, *agent.Agent, *events.EventBus, string) error {
		turns++
		return nil
	}

	cfg := &AgentWorkflowConfig{
		Initial:      &AgentWorkflowInitial{Prompt: "process items"},
		Continuation: &AgentWorkflowContinuation{TodoFile: todoPath, MaxContinuations: 1000},
	}
	_ = cfg.Validate()

	result, err := RunInitialContinuation(context.Background(), chatAgent, events.NewEventBus(), cfg, &WorkflowExecutionState{Version: 1}, queryExecutor)
	if err != nil {
		t.Fatalf("RunInitialContinuation: %v", err)
	}
	if result.StopReason != ContinuationStopNoProgress {
		t.Errorf("StopReason = %q, want %q", result.StopReason, ContinuationStopNoProgress)
	}
	if turns != 1 {
		t.Errorf("queryExecutor calls = %d, want 1 — no-progress must stop immediately", turns)
	}
}

// =============================================================================
// Test 5: commit-based progress is detected (git HEAD advances)
// =============================================================================

func TestRunInitialContinuation_GitCommitCountsAsProgress(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeContinuationTodo(t, dir, []string{"Item one", "Item two"})

	// Initialize a git repo in the temp dir so HEAD can advance.
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "add", "TODO.md")
	runGit(t, dir, "commit", "-m", "initial")

	// Point the agent's workspace root at the repo so the loop probes HEAD
	// there.
	client := agent.NewScriptedClient()
	client.SetModel("test:test")
	chatAgent := newTestLoopAgent(t, client)
	chatAgent.SetWorkspaceRoot(dir)

	turns := 0
	queryExecutor := func(context.Context, *agent.Agent, *events.EventBus, string) error {
		turns++
		// Make a commit each turn without ticking anything — progress is
		// detected purely from the new HEAD.
		f := filepath.Join(dir, "work"+string(rune('0'+turns))+".txt")
		if err := os.WriteFile(f, []byte("work"), 0644); err != nil {
			t.Fatalf("write work file: %v", err)
		}
		runGit(t, dir, "add", ".")
		runGit(t, dir, "commit", "-m", "work")
		if turns >= 2 {
			// After two commits, tick both items so the loop finishes.
			if err := os.WriteFile(todoPath, []byte("## Test Items\n- [x] Item one\n- [x] Item two\n"), 0644); err != nil {
				t.Fatalf("tick items: %v", err)
			}
		}
		return nil
	}

	cfg := &AgentWorkflowConfig{
		Initial:      &AgentWorkflowInitial{Prompt: "process items"},
		Continuation: &AgentWorkflowContinuation{TodoFile: todoPath},
	}
	_ = cfg.Validate()

	result, err := RunInitialContinuation(context.Background(), chatAgent, events.NewEventBus(), cfg, &WorkflowExecutionState{Version: 1}, queryExecutor)
	if err != nil {
		t.Fatalf("RunInitialContinuation: %v", err)
	}
	if result.StopReason != ContinuationStopNoRunnableItems {
		t.Errorf("StopReason = %q, want %q", result.StopReason, ContinuationStopNoRunnableItems)
	}
	// Turn 1 commits (progress), turn 2 commits + ticks (progress) → runnable 0.
	if result.Continuations != 2 {
		t.Errorf("Continuations = %d, want 2", result.Continuations)
	}
}

// =============================================================================
// Test 6: MaxContinuations caps the loop as a hard backstop
// =============================================================================

func TestRunInitialContinuation_MaxContinuationsCap(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeContinuationTodo(t, dir, []string{"Item one", "Item two", "Item three"})

	chatAgent := newContinuationAgent(t)
	turns := 0
	queryExecutor := func(context.Context, *agent.Agent, *events.EventBus, string) error {
		turns++
		return nil // never progresses
	}

	cfg := &AgentWorkflowConfig{
		Initial:      &AgentWorkflowInitial{Prompt: "process items"},
		Continuation: &AgentWorkflowContinuation{TodoFile: todoPath, MaxContinuations: 1},
	}
	_ = cfg.Validate()

	result, err := RunInitialContinuation(context.Background(), chatAgent, events.NewEventBus(), cfg, &WorkflowExecutionState{Version: 1}, queryExecutor)
	if err != nil {
		t.Fatalf("RunInitialContinuation: %v", err)
	}
	// With a no-progress turn, the loop stops after the first turn via
	// no_progress before the cap is consulted, so this asserts the loop
	// never exceeds the cap.
	if result.Continuations > 1 {
		t.Errorf("Continuations = %d, want <= 1 (cap)", result.Continuations)
	}
	if turns > 1 {
		t.Errorf("queryExecutor calls = %d, want <= 1", turns)
	}
}

// =============================================================================
// Test 7: disabled continuation is a no-op
// =============================================================================

func TestRunInitialContinuation_Disabled_NoOp(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeContinuationTodo(t, dir, []string{"Item one"})

	chatAgent := newContinuationAgent(t)
	calls := 0
	queryExecutor := func(context.Context, *agent.Agent, *events.EventBus, string) error {
		calls++
		return nil
	}

	disabled := false
	cfg := &AgentWorkflowConfig{
		Initial:      &AgentWorkflowInitial{Prompt: "process items"},
		Continuation: &AgentWorkflowContinuation{TodoFile: todoPath, ContinueUntilDone: &disabled},
	}
	_ = cfg.Validate()
	if cfg.Continuation != nil {
		t.Fatalf("expected disabled continuation to be dropped by Validate, got %+v", cfg.Continuation)
	}

	result, err := RunInitialContinuation(context.Background(), chatAgent, events.NewEventBus(), cfg, &WorkflowExecutionState{Version: 1}, queryExecutor)
	if err != nil {
		t.Fatalf("RunInitialContinuation: %v", err)
	}
	if calls != 0 || result.Continuations != 0 {
		t.Errorf("disabled continuation ran: calls=%d continuations=%d", calls, result.Continuations)
	}
}
