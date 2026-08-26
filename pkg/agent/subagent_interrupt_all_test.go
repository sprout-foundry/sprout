package agent

import (
	"context"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestTriggerInterruptCancelsActiveSubagents verifies that TriggerInterrupt
// on the primary preempts running subagents (CancelAll), not just the
// primary's own interrupt context. Previously CancelAll had no callers —
// Ctrl+C during a wedged subagent left the turn blocked until the
// subagent's 30-minute timeout because the interrupt never reached the
// subagent's run context.
func TestTriggerInterruptCancelsActiveSubagents(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	runner := parent.GetSubagentRunner()
	runner.testClientFactory = func(clientType api.ClientType, model string) (api.ClientInterface, error) {
		return NewScriptedClient(
			NewScriptedResponseBuilder().
				Content("subagent grinding").
				Delay(30 * time.Second).
				Build(),
		), nil
	}

	type runResult struct {
		result *SubagentResult
	}
	done := make(chan runResult, 1)
	go func() {
		done <- runResult{runner.Run(context.Background(), "do work", SubagentOptions{})}
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(runner.GetActiveSubagents()) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(runner.GetActiveSubagents()) == 0 {
		t.Fatal("subagent did not register as active within 5s")
	}

	parent.TriggerInterrupt()

	select {
	case r := <-done:
		if r.result == nil {
			t.Fatal("subagent Run returned nil result")
		}
		if !r.result.Cancelled && r.result.Error == nil {
			t.Fatalf("expected cancelled result or error after TriggerInterrupt, got %+v", r.result)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("subagent Run did not return within 10s of TriggerInterrupt — CancelAll wiring broken")
	}
}
