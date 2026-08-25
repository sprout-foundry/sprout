// Package agent: direct tool execution for daemon-routed one-shot calls.
package agent

import (
	"context"

	core "github.com/sprout-foundry/seed/core"
)

// ExecuteToolByName runs a single named tool against this agent's workspace
// and returns its content, or an error string when the tool failed.
//
// It builds a fresh seed ToolRegistry (with the security PreExecuteHook) per
// call and executes exactly one ToolCall through seed's full pipeline —
// unknown-tool detection, arg parse/repair, circuit breakers, pre-execute
// security hooks, timeouts, truncation, and panic recovery are all handled
// by seed's Execute. Registry construction per call is intentional: it
// matches the throwaway-agent pattern used by the daemon's one-shot queries,
// and tool registration is cheap.
func (a *Agent) ExecuteToolByName(ctx context.Context, name, argsJSON string) (content string, toolErr string) {
	registry := NewSeedToolRegistry(a)
	msgs := registry.Execute(ctx, []core.ToolCall{{
		ID:   "daemon-tool-1",
		Type: "function",
		Function: core.ToolCallFunction{
			Name:      name,
			Arguments: argsJSON,
		},
	}})
	if len(msgs) != 1 {
		return "", "tool execution returned no result"
	}
	if msgs[0].Status == core.ToolStatusError {
		return "", msgs[0].Content
	}
	return msgs[0].Content, ""
}
