//go:build !js

package workflow

// Regression tests for three TODO-loop liveness defects:
//  1. Stale checkpoint trusted without validation → wrong item.
//     Fix: validate the checkpointed line still holds an unchecked item;
//     if not, ignore the checkpoint and rescan (and delete the file).
//  2. Gate parse failure loops forever (continue with no cap).
//     Fix: circuit breaker — after gateFailureLimit consecutive gate
//     failures with no success, abort the loop with an error.
//  3. Gate answers prose where JSON is required, with no repair round.
//     Fix: one repair re-ask carrying the parse error and a JSON-only
//     instruction; a prose-then-JSON sequence now succeeds.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// loopConfigFor builds a minimal loop config for the given todo file.
func loopConfigFor(todoPath, gatePromptPath string) *AgentWorkflowConfig {
	return &AgentWorkflowConfig{
		Loop: &AgentWorkflowLoopConfig{
			TodoFile:       todoPath,
			GatePromptFile: gatePromptPath,
			MaxRetries:     1,
			MaxIterations:  50,
			BuildCommand:   "true",
		},
	}
}

// TestStaleFallbackCheckpoint_IgnoredWhenLineNotUnchecked: the fallback
// checkpoint points at a line that no longer holds an unchecked item. The
// loop must drop the checkpoint, rescan from the top, and process the
// first genuinely unchecked item in file order — and the stale checkpoint
// file must be removed so the next run does not repeat the mistake.
func TestStaleFallbackCheckpoint_IgnoredWhenLineNotUnchecked(t *testing.T) {
	dir := t.TempDir()
	// Checkpoint says "resume at line 2" — but line 2 is checked [x].
	todoPath := filepath.Join(dir, "TODO.md")
	content := `## Workflow Section
- [x] Already done item
- [ ] Real work item
`
	if err := os.WriteFile(todoPath, []byte(content), 0644); err != nil {
		t.Fatalf("write TODO.md: %v", err)
	}
	if err := PersistLoopCheckpoint(dir, 2); err != nil {
		t.Fatalf("PersistLoopCheckpoint: %v", err)
	}
	gatePromptPath := writeGatePromptFile(t, dir)

	// Exactly ONE gate response — if the loop honored the stale checkpoint
	// it would fail to find an unchecked item at line 2, and with the old
	// code startAfter=1 (line 2, 0-based skip of line 1)... which would
	// find line 3 anyway. To distinguish "validated" from "happens to
	// work", assert the warning path ran by checking the checkpoint file
	// was DELETED (the fix removes stale checkpoints) and the item
	// processed is line 3.
	client := agent.NewScriptedClient(
		gateResponse("Real work item", "Do the real work", false),
	)
	client.SetModel("test:test")
	chatAgent := newTestLoopAgent(t, client)
	eventBus := events.NewEventBus()

	var processed []string
	queryExecutor := func(ctx context.Context, _ *agent.Agent, _ *events.EventBus, query string) error {
		processed = append(processed, query)
		return nil
	}

	state := &WorkflowExecutionState{Version: 1}
	yielded, err := RunAgentWorkflowLoop(context.Background(), chatAgent, eventBus, loopConfigFor(todoPath, gatePromptPath), state, queryExecutor, nil)
	if err != nil {
		t.Fatalf("RunAgentWorkflowLoop error: %v", err)
	}
	if yielded {
		t.Fatalf("yielded = true, want false")
	}
	if !state.Complete {
		t.Errorf("state.Complete = false, want true")
	}
	if len(processed) != 1 {
		t.Fatalf("processed %d items, want 1", len(processed))
	}
	if !strings.Contains(processed[0], "Do the real work") {
		t.Errorf("processed prompt %q, want the line-3 item's gate prompt", processed[0])
	}
	// The stale checkpoint must have been removed by the validation path.
	if _, err := os.Stat(LoopCheckpointFilePath(dir)); !os.IsNotExist(err) {
		t.Errorf("stale fallback checkpoint still exists — validation did not clean it up")
	}
}

// TestGateParseFailure_CircuitBreakerAborts: gate returns prose forever →
// after the repair round, parse still fails → the loop must abort with an
// error after gateFailureLimit consecutive failures, NOT spin.
func TestGateParseFailure_CircuitBreakerAborts(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeTempTodoFile(t, dir, []string{"Item 1"})
	gatePromptPath := writeGatePromptFile(t, dir)

	// MaxRetries=1 → limit 3. Each failed attempt costs TWO scripted gate
	// responses (original + repair). Script 8 prose responses; if the
	// circuit breaker works, at most 6 are consumed before the abort.
	prose := []string{
		"BLOCKER: this TODO content is not what I expected.",
		"BLOCKED: cannot select an item from this section.",
		"BLOCKER: no eligible item found in the provided excerpt.",
		"BLOCKED: stopping instead of skipping ahead.",
		"BLOCKER: the section is missing from the provided content.",
		"BLOCKED: unable to select an item.",
		"BLOCKER: giving up (should never be reached).",
		"BLOCKED: should never be reached either.",
	}
	responses := make([]*agent.ScriptedResponse, 0, len(prose))
	for _, p := range prose {
		responses = append(responses, &agent.ScriptedResponse{Content: p})
	}
	client := agent.NewScriptedClient(responses...)
	client.SetModel("test:test")
	chatAgent := newTestLoopAgent(t, client)
	eventBus := events.NewEventBus()

	processCalls := 0
	queryExecutor := func(ctx context.Context, _ *agent.Agent, _ *events.EventBus, _ string) error {
		processCalls++
		return nil
	}

	state := &WorkflowExecutionState{Version: 1}
	_, err := RunAgentWorkflowLoop(context.Background(), chatAgent, eventBus, loopConfigFor(todoPath, gatePromptPath), state, queryExecutor, nil)
	if err == nil {
		t.Fatalf("expected abort error from circuit breaker, got nil (processCalls=%d)", processCalls)
	}
	if !strings.Contains(err.Error(), "consecutive") {
		t.Errorf("abort error %q should mention consecutive-failure limit", err)
	}
	if processCalls != 0 {
		t.Errorf("queryExecutor called %d times during gate failures, want 0", processCalls)
	}
}

// TestGateProseThenJSON_Repaired: first gate call answers in prose (fenced
// or bare), repair round returns valid JSON → the item processes normally
// and the loop completes with the single scripted JSON response remaining.
func TestGateProseThenJSON_Repaired(t *testing.T) {
	dir := t.TempDir()
	todoPath := writeTempTodoFile(t, dir, []string{"Item 1"})
	gatePromptPath := writeGatePromptFile(t, dir)

	client := agent.NewScriptedClient(
		&agent.ScriptedResponse{Content: "I cannot select an item: BLOCKED by unknown circumstances."},
		gateResponse("Item 1", "Process item 1", false),
	)
	client.SetModel("test:test")
	chatAgent := newTestLoopAgent(t, client)
	eventBus := events.NewEventBus()

	calls := 0
	queryExecutor := func(ctx context.Context, _ *agent.Agent, _ *events.EventBus, _ string) error {
		calls++
		return nil
	}

	state := &WorkflowExecutionState{Version: 1}
	yielded, err := RunAgentWorkflowLoop(context.Background(), chatAgent, eventBus, loopConfigFor(todoPath, gatePromptPath), state, queryExecutor, nil)
	if err != nil {
		t.Fatalf("RunAgentWorkflowLoop error: %v", err)
	}
	if yielded {
		t.Errorf("yielded = true, want false")
	}
	if !state.Complete {
		t.Errorf("state.Complete = false, want true")
	}
	if calls != 1 {
		t.Errorf("queryExecutor calls = %d, want 1", calls)
	}
	if got := countChecked(t, todoPath); got != 1 {
		t.Errorf("checked items = %d, want 1", got)
	}
}

// TestGateFailureLimit: unit for the derived limit.
func TestGateFailureLimit(t *testing.T) {
	cases := []struct{ maxRetries, want int }{
		{0, 3},
		{1, 3},
		{2, 3},
		{3, 4},
		{5, 6},
	}
	for _, tc := range cases {
		if got := gateFailureLimit(tc.maxRetries); got != tc.want {
			t.Errorf("gateFailureLimit(%d) = %d, want %d", tc.maxRetries, got, tc.want)
		}
	}
}
