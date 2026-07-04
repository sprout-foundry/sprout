package tools

import (
	"context"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// RunSubagentFunc is a function pointer set by pkg/agent at startup.
// It bridges the new ToolHandler interface with the legacy handleRunSubagent
// implementation that requires *Agent access.
//
// The function signature matches the legacy handler:
//
//	handleRunSubagent(ctx, args) → JSON string
//
// The agent sets this pointer during initialization, capturing the *Agent
// reference in a closure so the handler doesn't need direct access.
//
// Phase 4 of SP-109 will migrate the execute logic into this package,
// eliminating the need for this indirection.
var RunSubagentFunc func(ctx context.Context, args map[string]any) (string, error)

// runSubagentHandler implements ToolHandler for the run_subagent tool.
// It delegates a single implementation task to a subagent, running an
// in-process agent with a focused task and waiting for completion.
//
// This is a THIN WRAPPER that delegates Execute to the function pointer
// RunSubagentFunc. All metadata (Name, Definition, Validate, Aliases,
// Timeout, etc.) lives here.
type runSubagentHandler struct{}

func (h *runSubagentHandler) Name() string { return "run_subagent" }

func (h *runSubagentHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "run_subagent",
		Description: "Delegate a SINGLE implementation task to a subagent. " +
			"Runs an in-process agent with a focused task, waits for completion, and returns all output. " +
			"Use this when: (1) Tasks must be done SEQUENTIALLY with dependencies between them, " +
			"(2) You need to review results before deciding next steps, " +
			"(3) Working on a single focused feature. " +
			"For MULTIPLE INDEPENDENT tasks, use run_parallel_subagents instead for faster completion.\n\n" +
			"**REQUIRED**: You MUST specify a persona parameter. " +
			"Personas are configured from JSON defaults plus user config " +
			"(for example: general, coder, refactor, debugger, tester, reviewer, researcher, web_scraper).\n\n" +
			"Subagents use focused per-persona tool subsets from configuration for more deterministic behavior. " +
			"NO TIMEOUT - runs until completion. " +
			"Subagent provider and model are configured via config settings (subagent_provider and subagent_model).\n\n" +
			"**IMPORTANT — interpreting the result**: The subagent's response is a JSON envelope. " +
			"The `files_modified` array (also mirrored as a `[subagent files modified] … [/subagent files modified]` " +
			"block at the top of `stdout`) is the AUTHORITATIVE list of files this subagent edited. " +
			"Trust it: if a file does not appear in that list, the subagent did not change it. " +
			"Do NOT revert, undo, or treat as out-of-scope any file in the working tree unless you have " +
			"independently confirmed it is unrelated to the subagent's reported changes AND unrelated to " +
			"your own prior edits in this session. When in doubt, ask the user before reverting.",
		Required: []string{"prompt", "persona"},
		Parameters: []ParameterDef{
			{
				Name:        "prompt",
				Type:        "string",
				Required:    true,
				Description: "The prompt/task for the subagent to execute (required)",
			},
			{
				Name:        "persona",
				Type:        "string",
				Required:    true,
				Description: "REQUIRED: Subagent persona ID or alias (see /persona list)",
			},
			{
				Name:        "context",
				Type:        "string",
				Description: "Context from previous subagent work (files created, summaries, etc.)",
			},
			{
				Name:        "files",
				Type:        "string",
				Description: "Comma-separated list of relevant file paths (e.g., 'models/user.go,pkg/auth/jwt.go')",
			},
			{
				Name:        "working_dir",
				Type:        "string",
				Description: "Optional: directory to use as the subagent's working directory (must be within $HOME). Use this to spawn subagents operating in a different project directory.",
			},
		},
	}
}

func (h *runSubagentHandler) Validate(args map[string]any) error {
	if _, err := extractString(args, "prompt"); err != nil {
		return err
	}
	if _, err := extractString(args, "persona"); err != nil {
		return err
	}
	return nil
}

func (h *runSubagentHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	var hadError bool
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": hadError,
			})
		}()
	}

	if RunSubagentFunc == nil {
		hadError = true
		return ToolResult{
			Output:  "subagent support not configured: agent integration not initialized (RunSubagentFunc is nil)",
			IsError: true,
		}, nil
	}

	result, err := RunSubagentFunc(ctx, args)
	if err != nil {
		hadError = true
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *runSubagentHandler) Aliases() []string         { return nil }
func (h *runSubagentHandler) Timeout() time.Duration    { return 30 * time.Minute }
func (h *runSubagentHandler) MaxResultSize() int        { return 0 }
func (h *runSubagentHandler) SafeForParallel() bool     { return false }
func (h *runSubagentHandler) Interactive() bool         { return false }
