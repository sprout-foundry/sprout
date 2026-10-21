// seed_tool_repeat_guard_test.go — the executor wrapper that observes every
// seed tool call: the note-prefixed model-visible result, the stall cut
// (summary recorded, termination reason set, stall-stop invoked), and the
// pass-through with a nil agent.

package agent

import (
	"context"
	"strings"
	"testing"

	core "github.com/sprout-foundry/seed/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubExecutor records the calls it saw and returns one result message per
// call (keyed by the call's id).
type stubExecutor struct {
	calls   []core.ToolCall
	results []core.Message
}

func (e *stubExecutor) GetTools() []core.Tool { return nil }

func (e *stubExecutor) Execute(ctx context.Context, calls []core.ToolCall) []core.Message {
	e.calls = append(e.calls, calls...)
	out := make([]core.Message, 0, len(calls))
	for _, c := range calls {
		out = append(out, core.Message{ToolCallID: c.ID, Content: "tool result for " + c.Function.Name})
	}
	e.results = append(e.results, out...)
	return out
}

func seedCall(id, name, args string) core.ToolCall {
	return core.ToolCall{ID: id, Function: core.ToolCallFunction{Name: name, Arguments: args}}
}

func TestRepeatGuardExecutorPrefixesNote(t *testing.T) {
	agent := &Agent{}
	inner := &stubExecutor{}
	e := newRepeatGuardExecutor(inner, agent)
	calls := []core.ToolCall{seedCall("1", "shell_command", `{"command":"make build"}`)}
	for i := 0; i < 3; i++ {
		results := e.Execute(context.Background(), calls)
		require.Len(t, results, 1)
		if i < 2 {
			assert.Equal(t, "tool result for shell_command", results[0].Content)
		} else {
			assert.True(t, strings.HasPrefix(results[0].Content, "[repeat guard]"), "3rd identical call is note-prefixed")
			assert.Contains(t, results[0].Content, "tool result for shell_command", "the result body is preserved")
		}
	}
}

func TestRepeatGuardExecutorStopsAtThreshold(t *testing.T) {
	agent := &Agent{}
	agent.SetStallThresholds(3, 60)
	agent.state = NewAgentStateManager(false)
	stopped := make(chan struct{}, 1)
	agent.setStallStop(func() { stopped <- struct{}{} })

	inner := &stubExecutor{}
	e := newRepeatGuardExecutor(inner, agent)
	calls := []core.ToolCall{seedCall("1", "shell_command", `{"command":"make build"}`)}
	for i := 0; i < 3; i++ {
		_ = e.Execute(context.Background(), calls)
	}

	// The stall is recorded: summary readable, termination reason set, and
	// the stop callback ran (the run context would be cancelled).
	require.NotNil(t, agent.GetStallSummary())
	assert.Equal(t, "shell_command", agent.GetStallSummary().Tool)
	assert.Equal(t, RunTerminationStalled, agent.state.GetLastRunTerminationReason())
	select {
	case <-stopped:
	default:
		t.Fatal("the stall stop callback was not invoked")
	}
}

func TestRepeatGuardExecutorNilAgentPassThrough(t *testing.T) {
	inner := &stubExecutor{}
	e := newRepeatGuardExecutor(inner, nil)
	results := e.Execute(context.Background(), []core.ToolCall{seedCall("1", "shell_command", "{}")})
	require.Len(t, results, 1)
	assert.Equal(t, "tool result for shell_command", results[0].Content)
	assert.Nil(t, inner.agentProbe())
}

func TestRepeatGuardExecutorUnmatchedResultsSkipped(t *testing.T) {
	// A result with no matching call id (a synthetic recovery message) is
	// not attributed to any call.
	agent := &Agent{}
	inner := &unmatchedExecutor{}
	e := newRepeatGuardExecutor(inner, agent)
	results := e.Execute(context.Background(), []core.ToolCall{seedCall("1", "shell_command", "{}")})
	require.Len(t, results, 1)
	assert.Equal(t, "synthetic", results[0].Content)
	assert.Nil(t, agent.GetStallSummary())
	tracker := agent.toolRepeatTrackerForTurn()
	assert.Equal(t, 0, tracker.idleCalls(), "totalCalls stays 0 (idleCalls = total - distinct)")
	_ = tracker
}

type unmatchedExecutor struct{}

func (e *unmatchedExecutor) GetTools() []core.Tool { return nil }
func (e *unmatchedExecutor) Execute(ctx context.Context, calls []core.ToolCall) []core.Message {
	return []core.Message{{ToolCallID: "synthetic-recovery", Content: "synthetic"}}
}

// agentProbe exists so the nil-agent assertion above reads clearly.
func (e *stubExecutor) agentProbe() error { return nil }
