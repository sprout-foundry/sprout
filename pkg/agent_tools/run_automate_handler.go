package tools

import (
	"context"
	"time"
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
		Description: "Start an automated workflow from automate/ as a background process. " +
			"BEFORE calling: read the workflow JSON and its prompt_file, write a brief overview for the user, and ask them to confirm. " +
			"Returns immediately with session_id. For monitoring patterns, activate the 'workflow-automation' skill.",
		Required: []string{"workflow"},
		Parameters: []ParameterDef{
			{
				Name:        "workflow",
				Type:        "string",
				Required:    true,
				Description: "Workflow filename or name from automate/ directory",
			},
		},
	}
}

func (h *runAutomateHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "workflow")
	return err
}

func (h *runAutomateHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	ToolFuncMu.RLock()
	fn := RunAutomateFunc
	ToolFuncMu.RUnlock()
	if fn == nil {
		return ToolResult{
			Output:  "run_automate is not available: agent integration not initialized (RunAutomateFunc is nil)",
			IsError: true,
		}, nil
	}

	result, err := fn(ctx, args)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *runAutomateHandler) Aliases() []string      { return nil }
func (h *runAutomateHandler) Timeout() time.Duration { return 0 }
func (h *runAutomateHandler) MaxResultSize() int     { return 0 }
func (h *runAutomateHandler) SafeForParallel() bool  { return false }
func (h *runAutomateHandler) Interactive() bool      { return false }
