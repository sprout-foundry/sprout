package tools

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type runSubagentHandler struct{}

func (h *runSubagentHandler) Name() string { return "run_subagent" }

func (h *runSubagentHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "run_subagent",
		Description: "Delegate a SINGLE implementation task to a subagent. Runs an in-process agent with a focused task, waits for completion, and returns all output. Use this when: (1) Tasks must be done SEQUENTIALLY with dependencies between them, (2) You need to review results before deciding next steps, (3) Working on a single focused feature. For MULTIPLE INDEPENDENT tasks, use run_parallel_subagents instead.\n\n**REQUIRED**: You MUST specify a persona parameter. Personas are configured from JSON defaults plus user config (for example: general, coder, debugger, tester, code_reviewer, researcher, web_scraper).\n\nSubagents use focused per-persona tool subsets from configuration for more deterministic behavior. NO TIMEOUT - runs until completion. Subagent provider and model are configured via config settings (subagent_provider and subagent_model).",
		Required: []string{"prompt", "persona"},
		Parameters: []ParameterDef{
			{Name: "prompt", Type: "string", Required: true, Description: "The prompt/task for the subagent to execute (required)"},
			{Name: "persona", Type: "string", Required: true, Description: "REQUIRED: Subagent persona ID or alias (see /persona list)"},
			{Name: "context", Type: "string", Description: "Context from previous subagent work (files created, summaries, etc.)"},
			{Name: "files", Type: "string", Description: "Comma-separated list of relevant file paths (e.g., 'models/user.go,pkg/auth/jwt.go')"},
			{Name: "working_dir", Type: "string", Description: "Optional: directory to use as the subagent's working directory (must be within $HOME). Use this to spawn subagents operating in a different project directory."},
		},
	}
}

func (h *runSubagentHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "prompt")
	if err != nil {
		return err
	}
	_, err = extractString(args, "persona")
	return err
}

func (h *runSubagentHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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
	// event bus, persona resolution, and subagent spawning. This is a thin
	// wrapper stub that will be completed when the *Agent refactoring is done.
	return ToolResult{
		Output:  "Subagent tools require full *Agent refactoring for complete functionality. This handler cannot execute run_subagent without access to the Agent's LLM client, config manager, and event bus. Please use the legacy interface or complete the migration.",
		IsError: true,
	}, fmt.Errorf("run_subagent requires full *Agent refactoring")
}
