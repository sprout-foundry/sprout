package tools

import (
	"context"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// RunAutomateFunc is a function pointer set by pkg/agent at startup.
// It bridges the new ToolHandler interface with the legacy handleRunAutomate
// implementation that requires *Agent access.
//
// The function signature matches the legacy handler:
//
//	handleRunAutomate(ctx, args) → JSON string
//
// The agent sets this pointer during initialization, capturing the *Agent
// reference in a closure so the handler doesn't need direct access.
//
// Phase 4 of SP-109 will migrate the execute logic into this package,
// eliminating the need for this indirection.
var RunAutomateFunc func(ctx context.Context, args map[string]any) (string, error)

// runAutomateHandler implements ToolHandler for the run_automate tool.
// It starts an automated workflow from the project's automate/ directory
// as a long-running background process.
//
// This is a THIN WRAPPER that delegates Execute to the function pointer
// RunAutomateFunc. All metadata (Name, Definition, Validate, Aliases,
// Timeout, etc.) lives here.
type runAutomateHandler struct{}

func (h *runAutomateHandler) Name() string { return "run_automate" }

func (h *runAutomateHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "run_automate",
		Description: "Start an automated workflow from the project's automate/ directory as a long-running background process. " +
			"Use list_automate_workflows first to discover what's available. " +
			"BEFORE calling this tool: (1) read the workflow JSON and its prompt_file/command_file (if any) so you understand what the workflow will actually do; (2) write a brief plain-language overview to the user describing what will happen (steps, providers/models, expected duration, side effects like commits); (3) ask the user to confirm starting. The first call in a session triggers an explicit user approval prompt; subsequent calls for the SAME workflow during the same chat session are auto-approved so you (the primary agent) can restart it after failure without re-asking. " +
			"After invocation, the tool returns immediately with a session_id. The workflow keeps running in the background. " +
			"When the workflow finishes, a self-contained completion message is queued and delivered at the start of your next turn. If auto-resume is enabled (wakeup config), the completion triggers an automatic turn. For progress updates, use shell_command(check_background=<session_id>, wait_seconds=600). " +
			"To monitor it efficiently while waiting, use shell_command(check_background=<session_id>, wait_seconds=600) — this blocks (up to 10 min) until the workflow exits or the wait elapses, returning the snapshot. " +
			"Cadence guidance: first check ~60–90s after start (catches early failures), then use wait_seconds=600 in a loop while status stays 'running'. Surface meaningful updates to the user between waits — never poll silently for hours. If the user asks for status mid-run, do an immediate check with wait_seconds=0. " +
			"When status is 'exited', read the captured output, decide if the run succeeded, and resume control to either report results, retry the workflow (no re-approval needed), or take corrective action. " +
			"Returns JSON with workflow, description, command, session_id, and status fields.",
		Required: []string{"workflow"},
		Parameters: []ParameterDef{
			{
				Name:        "workflow",
				Type:        "string",
				Required:    true,
				Description: "Workflow filename or name (with or without .json extension) from the automate/ directory",
			},
		},
	}
}

func (h *runAutomateHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "workflow")
	return err
}

func (h *runAutomateHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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

	if RunAutomateFunc == nil {
		hadError = true
		return ToolResult{
			Output:  "run_automate is not available: agent integration not initialized (RunAutomateFunc is nil)",
			IsError: true,
		}, nil
	}

	result, err := RunAutomateFunc(ctx, args)
	if err != nil {
		hadError = true
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *runAutomateHandler) Aliases() []string         { return nil }
func (h *runAutomateHandler) Timeout() time.Duration    { return 0 }
func (h *runAutomateHandler) MaxResultSize() int        { return 0 }
func (h *runAutomateHandler) SafeForParallel() bool     { return false }
func (h *runAutomateHandler) Interactive() bool         { return false }
