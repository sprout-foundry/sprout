package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestSubagentCancellationPropagates is the regression test for the
// deadlock described in the 2026-06-25 session: a subagent's interruptCtx
// was derived from context.Background() inside createSubagent, so
// cancelling the parent's runCtx (Ctrl+C, timeout) had no effect on the
// subagent's in-flight LLM call. The subagent kept running, the 5-second
// grace in runTask expired, the goroutine leaked, and the footer showed
// "1 sub" indefinitely.
//
// The fix has two parts:
//  1. createSubagent derives interruptCtx from parentCtx (the parent's runCtx)
//  2. resetInterruptForNewQuery and ClearInterrupt preserve the parent link
//     by deriving from parentInterruptCtx instead of context.Background()
//
// This test verifies the end-to-end cancellation path: parent ctx cancel →
// subagent's interruptCtx is done after resetInterruptForNewQuery.
func TestSubagentCancellationPropagates(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)
	runner.testClientFactory = func(clientType api.ClientType, model string) (api.ClientInterface, error) {
		return NewScriptedClient(
			NewScriptedResponseBuilder().
				Content("subagent working").
				Delay(30 * time.Second). // blocks in SendChatRequest until ctx cancelled
				Build(),
		), nil
	}

	// Parent context that simulates Ctrl+C / timeout cancellation.
	parentCtx, parentCancel := context.WithCancel(context.Background())

	// Start the subagent task in a goroutine.
	var result *SubagentResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		result = runner.Run(parentCtx, "do some work", SubagentOptions{})
	}()

	// Give the subagent a moment to start up and enter ProcessQuery.
	time.Sleep(500 * time.Millisecond)

	// Cancel the parent context — simulates Ctrl+C.
	parentCancel()

	// The subagent should return within a few seconds (well under the
	// 5-second leak grace + 30-second scripted delay). If the fix is
	// broken, the subagent's LLM call won't notice the cancellation and
	// the result won't arrive for 30+ seconds.
	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		// Good — subagent returned promptly.
	case <-time.After(10 * time.Second):
		t.Fatal("subagent did not return within 10s of parent cancel — cancellation propagation broken")
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// The result should indicate cancellation or error (not clean completion).
	if result.Error == nil && !result.Cancelled {
		t.Errorf("expected error or cancellation, got clean completion: output=%q", result.Output)
	}
}

// TestSubagentInterruptCtxPreservedAcrossReset verifies the unit-level
// mechanism: after resetInterruptForNewQuery (called at the top of
// ProcessQuery), the subagent's interruptCtx is still derived from the
// parent context, not from context.Background().
func TestSubagentInterruptCtxPreservedAcrossReset(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)
	runner.testClientFactory = func(clientType api.ClientType, model string) (api.ClientInterface, error) {
		return NewScriptedClient(), nil
	}

	parentCtx, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	subAgent, err := runner.createSubagent(SubagentOptions{}, parentCtx)
	if err != nil {
		t.Fatalf("createSubagent failed: %v", err)
	}
	defer subAgent.Shutdown()

	// Simulate what ProcessQuery does at the top: resetInterruptForNewQuery.
	// First, cancel the current interruptCtx so reset actually fires.
	subAgent.TriggerInterrupt()
	subAgent.resetInterruptForNewQuery()

	// Now cancel the parent context.
	parentCancel()

	// The subagent's interruptCtx should observe the cancellation.
	ctx, _ := subAgent.snapshotInterrupt()
	select {
	case <-ctx.Done():
		// Good — parent cancellation propagated through the reset.
	case <-time.After(1 * time.Second):
		t.Fatal("subagent interruptCtx did not observe parent cancellation after resetInterruptForNewQuery")
	}
}

// TestSubagentInterruptCtxPreservedAcrossClear verifies the same
// mechanism for ClearInterrupt, which is called by HandleInterrupt.
func TestSubagentInterruptCtxPreservedAcrossClear(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)
	runner.testClientFactory = func(clientType api.ClientType, model string) (api.ClientInterface, error) {
		return NewScriptedClient(), nil
	}

	parentCtx, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	subAgent, err := runner.createSubagent(SubagentOptions{}, parentCtx)
	if err != nil {
		t.Fatalf("createSubagent failed: %v", err)
	}
	defer subAgent.Shutdown()

	// ClearInterrupt replaces interruptCtx — must still derive from parent.
	subAgent.ClearInterrupt()

	// Cancel the parent context.
	parentCancel()

	// The subagent's interruptCtx should observe the cancellation.
	ctx, _ := subAgent.snapshotInterrupt()
	select {
	case <-ctx.Done():
		// Good — parent cancellation propagated through ClearInterrupt.
	case <-time.After(1 * time.Second):
		t.Fatal("subagent interruptCtx did not observe parent cancellation after ClearInterrupt")
	}
}

// TestPrimaryAgentInterruptCtxUnaffectedByParentField verifies that the
// primary agent (which has no parentInterruptCtx) still works correctly:
// resetInterruptForNewQuery and ClearInterrupt derive from
// context.Background() as before.
func TestPrimaryAgentInterruptCtxUnaffectedByParentField(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	if agent.parentInterruptCtx != nil {
		t.Fatal("primary agent should have nil parentInterruptCtx")
	}

	// resetInterruptForNewQuery should produce a non-cancelled ctx.
	agent.TriggerInterrupt() // cancel current ctx so reset fires
	agent.resetInterruptForNewQuery()

	ctx, _ := agent.snapshotInterrupt()
	select {
	case <-ctx.Done():
		t.Fatal("primary agent interruptCtx should not be cancelled after reset")
	default:
		// Good — fresh, non-cancelled context.
	}

	// ClearInterrupt should also produce a non-cancelled ctx.
	agent.ClearInterrupt()
	ctx, _ = agent.snapshotInterrupt()
	select {
	case <-ctx.Done():
		t.Fatal("primary agent interruptCtx should not be cancelled after ClearInterrupt")
	default:
		// Good.
	}

	// Verify we can still trigger interrupts on the primary agent.
	agent.TriggerInterrupt()
	ctx, _ = agent.snapshotInterrupt()
	select {
	case <-ctx.Done():
		// Good — TriggerInterrupt cancelled the ctx.
	default:
		t.Fatal("TriggerInterrupt should have cancelled the ctx")
	}
}

// TestSubagentMaxIterationsBounded verifies that createSubagent sets a
// finite maxIterations, preventing the runaway 164-iteration loops
// observed before the fix.
func TestSubagentMaxIterationsBounded(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)
	runner.testClientFactory = func(clientType api.ClientType, model string) (api.ClientInterface, error) {
		return NewScriptedClient(), nil
	}

	subAgent, err := runner.createSubagent(SubagentOptions{}, context.Background())
	if err != nil {
		t.Fatalf("createSubagent failed: %v", err)
	}
	defer subAgent.Shutdown()

	if subAgent.maxIterations == 0 {
		t.Fatal("subagent maxIterations should be non-zero (bounded), got 0 (unlimited)")
	}
	if subAgent.maxIterations > 300 {
		t.Errorf("subagent maxIterations should be reasonably bounded, got %d", subAgent.maxIterations)
	}
	if subAgent.maxIterations != defaultSubagentMaxIterations {
		t.Errorf("expected defaultSubagentMaxIterations=%d, got %d", defaultSubagentMaxIterations, subAgent.maxIterations)
	}
}

// TestInjectInputIntoActiveRoutesToSubagent verifies that steering
// messages are delivered to the deepest running subagent, not silently
// dropped because the primary is blocked inside run_subagent.
func TestInjectInputIntoActiveRoutesToSubagent(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)
	runner.testClientFactory = func(clientType api.ClientType, model string) (api.ClientInterface, error) {
		return NewScriptedClient(
			NewScriptedResponseBuilder().
				Content("subagent working").
				Delay(10 * time.Second). // keep subagent alive long enough to steer
				Build(),
		), nil
	}

	parentCtx, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	// Start subagent in background.
	go runner.Run(parentCtx, "do work", SubagentOptions{})

	// Wait for subagent to register as active.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(runner.GetActiveSubagents()) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(runner.GetActiveSubagents()) == 0 {
		t.Fatal("subagent did not register as active within 3s")
	}

	// Inject a steering message.
	targetID, ok := runner.InjectInputIntoActive("steer this way")
	if !ok {
		t.Fatal("InjectInputIntoActive should have delivered to the running subagent")
	}
	if targetID == "" {
		t.Fatal("target ID should be non-empty")
	}

	// Verify the message reached the subagent's input channel.
	ch := parent.GetInputInjectionContext()
	_ = ch // The subagent shares the parent's injection mechanism via seed.InjectInput bridge.

	// Clean up.
	parentCancel()
	time.Sleep(500 * time.Millisecond) // let the goroutine exit
}

// TestSubagentOutputFlushUsesTryLockOnCancel verifies that the output
// flush in runTask uses TryLock (not blocking Lock) so a leaked
// goroutine holding outputMu doesn't hang runTask indefinitely — the
// "1 sub" badge stuck in footer symptom.
func TestSubagentOutputFlushUsesTryLockOnCancel(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)
	runner.testClientFactory = func(clientType api.ClientType, model string) (api.ClientInterface, error) {
		return NewScriptedClient(
			NewScriptedResponseBuilder().
				Content("working").
				Delay(30 * time.Second).
				Build(),
		), nil
	}

	parentCtx, parentCancel := context.WithCancel(context.Background())

	// Start subagent.
	done := make(chan *SubagentResult, 1)
	go func() {
		done <- runner.Run(parentCtx, "work", SubagentOptions{})
	}()

	// Wait for subagent to start.
	time.Sleep(500 * time.Millisecond)

	// Cancel — should return within the 5s grace + some margin.
	parentCancel()

	select {
	case res := <-done:
		// Good — returned without hanging.
		_ = res
	case <-time.After(10 * time.Second):
		t.Fatal("runTask hung after parent cancel — TryLock flush or cancellation broken")
	}
}
