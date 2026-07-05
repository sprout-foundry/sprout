package tools

import (
	"context"
	"fmt"
	"time"
)

// RunParallelSubagentsFunc is a function pointer set by pkg/agent at startup.
// It bridges the new ToolHandler interface with the legacy handleRunParallelSubagents
// implementation that requires *Agent access.
//
// The function signature matches the legacy handler:
//
//	handleRunParallelSubagents(ctx, args) → JSON string
//
// The agent sets this pointer during initialization, capturing the *Agent
// reference in a closure so the handler doesn't need direct access.
//
// Phase 4 of SP-109 will migrate the execute logic into this package,
// eliminating the need for this indirection.
var RunParallelSubagentsFunc func(ctx context.Context, args map[string]any) (string, error)

// runParallelSubagentsHandler implements ToolHandler for the run_parallel_subagents tool.
// It executes multiple independent subagent tasks concurrently, waits for all
// to complete, and returns per-task results.
//
// This is a THIN WRAPPER that delegates Execute to the function pointer
// RunParallelSubagentsFunc. All metadata (Name, Definition, Validate, Aliases,
// Timeout, etc.) lives here.
type runParallelSubagentsHandler struct{}

func (h *runParallelSubagentsHandler) Name() string { return "run_parallel_subagents" }

func (h *runParallelSubagentsHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "run_parallel_subagents",
		Description: "Execute 2+ INDEPENDENT subagent tasks CONCURRENTLY. " +
			"Use when tasks have no dependencies on each other (e.g. researching different code areas, code + tests, analyzing multiple files). " +
			"Waits for ALL to complete; returns per-ID `{stdout, stderr, exit_code, completed, timed_out}`.\n\n" +
			"Accepts an array of strings: `[\"task 1\", \"task 2\", …]`. " +
			"IDs auto-generate as task-1, task-2, etc.\n\n" +
			"Personas are NOT supported here (use `run_subagent` for per-task personas) " +
			"— parallel subagents use the default subagent config. " +
			"Provider/model from `subagent_provider` / `subagent_model`.\n\n" +
			"**Result contract**: each subagent's `files_modified` (also mirrored as " +
			"`[subagent files modified] … [/subagent files modified]` at the top of its `stdout`) " +
			"is the AUTHORITATIVE record of what it edited. " +
			"Do NOT revert files unless they appear in some subagent's list AND you've decided to undo that specific work. " +
			"Multiple subagents may touch related files — check every result's manifest before concluding a file is unrelated.",
		Required: []string{"subagents"},
		Parameters: []ParameterDef{
			{
				Name:        "subagents",
				Type:        "array",
				Required:    true,
				Description: "Array of task descriptions as strings: [\"task 1\", \"task 2\", \"task 3\"]. Auto-generates IDs like task-1, task-2, etc. Example: [\"Research X\", \"Implement Y\", \"Write tests for Z\"]",
			},
		},
	}
}

func (h *runParallelSubagentsHandler) Validate(args map[string]any) error {
	val, exists := args["subagents"]
	if !exists || val == nil {
		return fmt.Errorf("parameter 'subagents' is required")
	}
	if _, ok := val.([]any); !ok {
		return fmt.Errorf("parameter 'subagents' must be an array, got %T", val)
	}
	return nil
}

func (h *runParallelSubagentsHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	if RunParallelSubagentsFunc == nil {
		return ToolResult{
			Output:  "subagent support not configured: agent integration not initialized (RunParallelSubagentsFunc is nil)",
			IsError: true,
		}, nil
	}

	result, err := RunParallelSubagentsFunc(ctx, args)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *runParallelSubagentsHandler) Aliases() []string      { return nil }
func (h *runParallelSubagentsHandler) Timeout() time.Duration { return 30 * time.Minute }
func (h *runParallelSubagentsHandler) MaxResultSize() int     { return 0 }
func (h *runParallelSubagentsHandler) SafeForParallel() bool  { return false }
func (h *runParallelSubagentsHandler) Interactive() bool      { return false }
