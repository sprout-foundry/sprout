// seed_tool_repeat_guard.go — the executor wrapper that routes every seed
// tool call through the agent's per-turn repeat guard (tool_repeat_guard.go).
//
// It is the single observation point for BOTH the call (tool name + args)
// and its result: seed's conversation loop executes tools through
// core.Options.Executor, and sprout builds that executor as a stack of
// wrappers around the seed tool registry. This wrapper sits above the
// steer-flush executor so it sees the final tool results and can:
//
//   - prefix the model-visible result with a repeat nudge from the 3rd
//     identical call onward (the generalised repeat guard);
//   - end the in-flight turn (cancel the run context) when a call crosses
//     a stall threshold, recording a stall summary for the run record and
//     the continuation loop.
//
// The read_file handler's own ReadCallGuard short-circuit still runs inside
// the registry; the general guard additionally observes that call (its note
// composes harmlessly with read_file's pinned note).

package agent

import (
	"context"

	core "github.com/sprout-foundry/seed/core"
)

// repeatGuardExecutor wraps a core.ToolExecutor so every tool call is
// observed by the agent's per-turn repeat guard. It delegates execution to
// the inner executor, then, for each call/result pair, runs the observation
// (repeat note + stall detection). When a stall is detected it ends the
// turn's run context so seed's loop stops at the next iteration boundary.
type repeatGuardExecutor struct {
	inner core.ToolExecutor
	agent *Agent
}

// newRepeatGuardExecutor builds the wrapper for a turn's executor stack.
// inner is the executor it observes (the steer-flush executor over the seed
// registry); agent supplies the per-turn tracker and the stall-stop
// callback. A nil agent disables the observation (the wrapper becomes a
// pass-through), so a bare standalone run never trips the guard.
func newRepeatGuardExecutor(inner core.ToolExecutor, agent *Agent) *repeatGuardExecutor {
	return &repeatGuardExecutor{inner: inner, agent: agent}
}

// GetTools delegates to the inner executor.
func (e *repeatGuardExecutor) GetTools() []core.Tool {
	return e.inner.GetTools()
}

// Execute runs the batch, observes each call against the per-turn repeat
// guard, and applies the repeat note / stall stop. The returned messages
// carry the (possibly note-prefixed) content so the model sees the nudge.
func (e *repeatGuardExecutor) Execute(ctx context.Context, calls []core.ToolCall) []core.Message {
	results := e.inner.Execute(ctx, calls)
	if e.agent == nil || len(calls) == 0 {
		return results
	}

	// Index results by tool-call id so each call pairs with its result. A
	// missing id (a synthetic recovery result) is skipped: it has no call
	// to attribute.
	byID := make(map[string]*core.Message, len(results))
	for i := range results {
		if results[i].ToolCallID != "" {
			byID[results[i].ToolCallID] = &results[i]
		}
	}

	stalled := false
	for i, call := range calls {
		msg, ok := byID[call.ID]
		if !ok || msg == nil {
			continue
		}
		toolName := call.Function.Name
		args := call.Function.Arguments
		note, isStalled, summary := e.agent.recordToolCall(toolName, args, msg.Content)
		_ = i
		if note != "" {
			// Prepend the nudge so the model sees it before the result; the
			// result body is unchanged (it is the actual tool output).
			msg.Content = note + "\n\n" + msg.Content
		}
		if isStalled && !stalled {
			stalled = true
			if summary != nil {
				e.agent.markTurnStalled(summary)
			}
			// End the in-flight turn: seed's loop observes the cancelled run
			// context at its next iteration boundary and returns
			// ErrInterrupted, which handleQueryResult classifies as a stall
			// (a stall summary is set) rather than a user interrupt.
			e.agent.requestStallStop()
		}
	}
	return results
}
