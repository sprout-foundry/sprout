package tools

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type runParallelSubagentsHandler struct{}

func (h *runParallelSubagentsHandler) Name() string { return "run_parallel_subagents" }

func (h *runParallelSubagentsHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "run_parallel_subagents",
		Description: "Execute MULTIPLE INDEPENDENT subagent tasks CONCURRENTLY in parallel. Use this when you have 2+ tasks that can be done SIMULTANEOUSLY without dependencies (e.g., researching different code areas, writing code + tests concurrently, analyzing multiple files). This is MUCH FASTER than running tasks sequentially. Waits for ALL tasks to complete and returns results for each task by ID. Results include stdout, stderr, exit_code, completed status, and timed_out status for each task ID. Prefer this over run_subagent when tasks are independent.\n\nAccepts simple array of strings: [\"task 1 description\", \"task 2 description\", \"task 3\"]. IDs will be auto-generated (task-1, task-2, etc.).\n\nNote: Personas are only supported for single subagent execution via run_subagent. Parallel subagents use the default subagent configuration.\n\nSubagent provider and model are configured via config settings (subagent_provider and subagent_model).",
		Required: []string{"subagents"},
		Parameters: []ParameterDef{
			{Name: "subagents", Type: "array", Required: true, Description: "Array of task descriptions as strings: [\"task 1\", \"task 2\", \"task 3\"]. Auto-generates IDs like task-1, task-2, etc. Example: [\"Research X\", \"Implement Y\", \"Write tests for Z\"]"},
		},
	}
}

func (h *runParallelSubagentsHandler) Validate(args map[string]any) error {
	subagentsRaw, ok := args["subagents"]
	if !ok {
		return fmt.Errorf("parameter 'subagents' is required")
	}
	subSlice, ok := subagentsRaw.([]interface{})
	if !ok {
		return fmt.Errorf("parameter 'subagents' must be an array")
	}
	if len(subSlice) == 0 {
		return fmt.Errorf("parameter 'subagents' must not be empty")
	}
	for i, s := range subSlice {
		if _, ok := s.(string); !ok {
			return fmt.Errorf("each subagent task at index %d must be a string", i)
		}
	}
	return nil
}

func (h *runParallelSubagentsHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": true,
			})
		}()
	}

	// TODO: Full implementation requires *Agent access for LLM client, config,
	// event bus, persona resolution, and parallel subagent spawning. This is a
	// thin wrapper stub that will be completed when the *Agent refactoring is done.
	return ToolResult{
		Output:  "Subagent tools require full *Agent refactoring for complete functionality. This handler cannot execute run_parallel_subagents without access to the Agent's LLM client, config manager, and event bus. Please use the legacy interface or complete the migration.",
		IsError: true,
	}, fmt.Errorf("run_parallel_subagents requires full *Agent refactoring")
}
