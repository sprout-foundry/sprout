package tools

import (
	"context"
	"fmt"
	"time"
)

// checkSubagentHandler implements check_subagent: list background subagent
// tasks, or report one task's progress or result (optionally waiting).
type checkSubagentHandler struct{}

func (h *checkSubagentHandler) Name() string { return "check_subagent" }

func (h *checkSubagentHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "check_subagent",
		Description: "Check background subagent tasks started by run_subagent or review_changes. " +
			"Without task_id: list tasks and their status. With task_id: the task's progress (step, tool calls, tokens, recent output) " +
			"while it runs, or its full result once finished. Set wait_seconds to block until it finishes (max 600). " +
			"You are notified automatically when a background task finishes, so only check when you need the result now.",
		Parameters: []ParameterDef{
			{Name: "task_id", Type: "string", Description: "Task ID returned when the background task started. Omit to list all tasks."},
			{Name: "wait_seconds", Type: "number", Description: "Block up to this many seconds (max 600) for the task to finish before returning. 0 (default) returns immediately."},
		},
	}
}

func (h *checkSubagentHandler) Validate(args map[string]any) error { return nil }

func (h *checkSubagentHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	return runAgentToolFunc(ctx, env.ResolveToolFuncs().CheckSubagent, "check_subagent", args)
}

func (h *checkSubagentHandler) Aliases() []string      { return nil }
func (h *checkSubagentHandler) Timeout() time.Duration { return 11 * time.Minute }
func (h *checkSubagentHandler) MaxResultSize() int     { return 0 }
func (h *checkSubagentHandler) SafeForParallel() bool  { return true }
func (h *checkSubagentHandler) Interactive() bool      { return false }

// stopSubagentHandler implements stop_subagent.
type stopSubagentHandler struct{}

func (h *stopSubagentHandler) Name() string { return "stop_subagent" }

func (h *stopSubagentHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "stop_subagent",
		Description: "Stop a running background subagent task (from run_subagent or review_changes). No completion notification is sent for a task you stop.",
		Required:    []string{"task_id"},
		Parameters: []ParameterDef{
			{Name: "task_id", Type: "string", Required: true, Description: "Task ID returned when the background task started."},
		},
	}
}

func (h *stopSubagentHandler) Validate(args map[string]any) error {
	if id, _ := args["task_id"].(string); id == "" {
		return fmt.Errorf("parameter 'task_id' is required")
	}
	return nil
}

func (h *stopSubagentHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	return runAgentToolFunc(ctx, env.ResolveToolFuncs().StopSubagent, "stop_subagent", args)
}

func (h *stopSubagentHandler) Aliases() []string      { return nil }
func (h *stopSubagentHandler) Timeout() time.Duration { return 30 * time.Second }
func (h *stopSubagentHandler) MaxResultSize() int     { return 0 }
func (h *stopSubagentHandler) SafeForParallel() bool  { return false }
func (h *stopSubagentHandler) Interactive() bool      { return false }

// runAgentToolFunc dispatches to an agent-provided tool func, reporting a
// missing integration as a tool error rather than failing the call.
func runAgentToolFunc(ctx context.Context, fn func(context.Context, map[string]any) (string, error), name string, args map[string]any) (ToolResult, error) {
	if fn == nil {
		return ToolResult{Output: name + " is not available: agent integration not initialized", IsError: true}, nil
	}
	result, err := fn(ctx, args)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}
