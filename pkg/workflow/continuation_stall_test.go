//go:build !js

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stallTurn returns an executor that reports a stall on its nth call
// (1-based; 0 means every turn stalls). Earlier turns succeed with progress
// behaviour supplied by the caller via progress.
func stallExecutor(t *testing.T, todoPath string, stallOn int, progress func(turn int)) QueryExecutor {
	t.Helper()
	turns := 0
	return func(_ context.Context, chatAgent *agent.Agent, _ *events.EventBus, _ string) error {
		turns++
		if stallOn == 0 || turns == stallOn {
			// The in-turn guard would have recorded the summary before the
			// turn was cut; simulate exactly that bookkeeping.
			chatAgent.RecordStallSummary(&agent.StallSummary{
				Tool:    "shell_command",
				Args:    "git status",
				Output:  "nothing to commit",
				Repeats: 8,
				Reason:  "identical_calls",
			})
			return agent.ErrTurnStalled
		}
		if progress != nil {
			progress(turns)
		}
		return nil
	}
}

func TestContinuation_StalledTurnIsNoProgressButRunContinues(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeContinuationTodo(t, dir, []string{"A", "B"})

	// Turn 1 stalls; turn 2 (steered by the stall prompt) ticks item A; the
	// run finishes normally.
	var prompts []string
	outerTurns := 0
	exec := func(_ context.Context, chatAgent *agent.Agent, _ *events.EventBus, prompt string) error {
		outerTurns++
		prompts = append(prompts, prompt)
		if outerTurns == 1 {
			// The in-turn guard would have recorded the summary before the
			// turn was cut; simulate exactly that bookkeeping.
			chatAgent.RecordStallSummary(&agent.StallSummary{
				Tool:    "shell_command",
				Args:    "git status",
				Output:  "nothing to commit",
				Repeats: 8,
				Reason:  "identical_calls",
			})
			return agent.ErrTurnStalled
		}
		tickNext(t, todoPath)
		return nil
	}

	result := runContinuation(t, newContinuationAgent(t), &AgentWorkflowContinuation{TodoFile: todoPath}, exec)

	assert.Equal(t, ContinuationStopNoRunnableItems, result.StopReason)
	assert.Equal(t, 1, result.StalledTurns)
	require.NotNil(t, result.LastStallSummary)
	assert.Equal(t, "shell_command", result.LastStallSummary.Tool)
	require.Len(t, prompts, 3)
	// The turn AFTER the stall is steered by the stall prompt, which names
	// the stalled call.
	assert.Contains(t, prompts[1], DefaultContinuationStallPrompt)
	assert.Contains(t, prompts[1], "shell_command")
	assert.Contains(t, prompts[1], "git status")
}

func TestContinuation_TwoStallsOnSameItemMarksItSkipped(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeContinuationTodo(t, dir, []string{"Stuck", "Other"})

	// Every turn stalls on the same probe; nothing ticks. MaxIdleTurns=2
	// stops the loop after the second no-progress turn, and the leading
	// item is marked skipped.
	exec := stallExecutor(t, todoPath, 0, nil)

	result := runContinuation(t, newContinuationAgent(t), &AgentWorkflowContinuation{TodoFile: todoPath, MaxIdleTurns: 2}, exec)

	assert.Equal(t, ContinuationStopNoProgress, result.StopReason)
	assert.Equal(t, 2, result.StalledTurns)
	require.Len(t, result.SkippedItems, 1)
	assert.Contains(t, result.SkippedItems[0], "Stuck")
	require.NotNil(t, result.LastStallSummary)
	assert.Equal(t, "git status", result.LastStallSummary.Args)
}

func TestContinuation_StallThenProgressResetsSkipChain(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeContinuationTodo(t, dir, []string{"A", "B"})

	// Turn 1 stalls, turn 2 makes progress (ticks A) — the consecutive
	// chain resets, so a later stall never marks the leading item skipped
	// unless it stalls twice IN A ROW.
	outerTurns := 0
	exec := func(_ context.Context, chatAgent *agent.Agent, _ *events.EventBus, prompt string) error {
		outerTurns++
		if outerTurns == 1 {
			chatAgent.RecordStallSummary(&agent.StallSummary{Tool: "shell_command", Repeats: 8, Reason: "identical_calls"})
			return agent.ErrTurnStalled
		}
		tickNext(t, todoPath)
		return nil
	}

	result := runContinuation(t, newContinuationAgent(t), &AgentWorkflowContinuation{TodoFile: todoPath}, exec)

	assert.Equal(t, ContinuationStopNoRunnableItems, result.StopReason)
	assert.Equal(t, 1, result.StalledTurns)
	assert.Empty(t, result.SkippedItems, "stalls separated by progress never skip")
}

func TestContinuation_StallDefaultsOffDoesNotAffectHealthyRuns(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeContinuationTodo(t, dir, []string{"A"})

	var prompts []string
	exec := func(_ context.Context, _ *agent.Agent, _ *events.EventBus, query string) error {
		prompts = append(prompts, query)
		tickNext(t, todoPath)
		return nil
	}

	result := runContinuation(t, newContinuationAgent(t), &AgentWorkflowContinuation{TodoFile: todoPath}, exec)

	assert.Equal(t, ContinuationStopNoRunnableItems, result.StopReason)
	assert.Equal(t, 0, result.StalledTurns)
	assert.Nil(t, result.LastStallSummary)
	assert.Empty(t, result.SkippedItems)
}

func TestStallWorkflowConfigDefaults(t *testing.T) {
	cfg := &AgentWorkflowConfig{
		Initial: &AgentWorkflowInitial{Prompt: "work"},
		Stall:   &AgentWorkflowStallConfig{},
	}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, DefaultStallIdenticalCalls, cfg.Stall.StallIdenticalCalls)
	assert.Equal(t, DefaultStallIdleCalls, cfg.Stall.StallIdleCalls)

	// Explicit values win.
	cfg = &AgentWorkflowConfig{
		Initial: &AgentWorkflowInitial{Prompt: "work"},
		Stall:   &AgentWorkflowStallConfig{StallIdenticalCalls: 3, StallIdleCalls: 20},
	}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, 3, cfg.Stall.StallIdenticalCalls)
	assert.Equal(t, 20, cfg.Stall.StallIdleCalls)

	// An absent block leaves the agent's own defaults — the attach helper
	// is a no-op, not a zeroing write.
	a := newContinuationAgent(t)
	AttachWorkflowStallThresholds(a, nil)
	AttachWorkflowStallThresholds(a, &AgentWorkflowConfig{})
	// A configured block reaches the agent.
	AttachWorkflowStallThresholds(a, cfg)
	// A nil agent never panics.
	AttachWorkflowStallThresholds(nil, cfg)
}

func TestStallContinuationPromptNamesTheCall(t *testing.T) {
	got := stallContinuationPrompt(&ContinuationStallSummary{
		Tool:   "shell_command",
		Args:   "git status --porcelain=v1 --branch=very-long-branch-name-that-goes-on " + strings.Repeat("x", 200),
		Output: "## main",
	})
	assert.True(t, strings.HasPrefix(got, DefaultContinuationStallPrompt))
	assert.Contains(t, got, "shell_command")
	// The snippet is capped so a huge arg does not bloat the prompt.
	assert.Less(t, len(got), len(DefaultContinuationStallPrompt)+400)
	assert.Nil(t, (*ContinuationStallSummary)(nil), "a nil summary falls back to the bare prompt")
	assert.Equal(t, DefaultContinuationStallPrompt, stallContinuationPrompt(nil))
}

func TestFirstRunnableTodoItem(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeContinuationTodo(t, dir, []string{"First item", "Second item"})
	assert.Contains(t, firstRunnableTodoItem(todoPath), "First item")

	empty := filepath.Join(dir, "empty.md")
	require.NoError(t, os.WriteFile(empty, []byte("# nothing\n"), 0o600))
	assert.Empty(t, firstRunnableTodoItem(empty))
	assert.Empty(t, firstRunnableTodoItem(filepath.Join(dir, "missing.md")))
}
