package tools

import (
	"context"
	"time"
)

// RespondClarificationFunc is a function pointer set by pkg/agent at startup.
// It bridges the new ToolHandler interface with the legacy
// handleRespondClarification implementation that requires *Agent access.
//
// The function signature matches the legacy handler:
//
//	handleRespondClarification(ctx, args) → string, error
//
// The agent sets this pointer during initialization, capturing the *Agent
// reference in a closure so the handler doesn't need direct access.
//
// Phase 4 of SP-109 will migrate the execute logic into this package,
// eliminating the need for this indirection.
var RespondClarificationFunc func(ctx context.Context, args map[string]any) (string, error)

// respondClarificationHandler implements ToolHandler for the
// respond_clarification tool. It is called by parent agents to respond to
// a subagent's clarification request.
//
// This is a THIN WRAPPER that delegates Execute to the function pointer
// RespondClarificationFunc. All metadata (Name, Definition, Validate,
// Aliases, Timeout, etc.) lives here.
type respondClarificationHandler struct{}

func (h *respondClarificationHandler) Name() string { return "respond_clarification" }

func (h *respondClarificationHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "respond_clarification",
		Description: "Respond to a clarification request from a delegate agent. " +
			"Provide the request_id and your response to give the delegate additional " +
			"context or guidance.",
		Required: []string{"request_id", "response"},
		Parameters: []ParameterDef{
			{
				Name:        "request_id",
				Type:        "string",
				Required:    true,
				Description: "The ID of the clarification request to respond to",
			},
			{
				Name:        "response",
				Type:        "string",
				Required:    true,
				Description: "Your clarification response",
			},
		},
	}
}

func (h *respondClarificationHandler) Validate(args map[string]any) error {
	if _, err := extractString(args, "request_id"); err != nil {
		return err
	}
	_, err := extractString(args, "response")
	return err
}

func (h *respondClarificationHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	ToolFuncMu.RLock()
	fn := RespondClarificationFunc
	ToolFuncMu.RUnlock()
	if fn == nil {
		return ToolResult{
			Output:  "respond_clarification is not available: agent integration not initialized (RespondClarificationFunc is nil)",
			IsError: true,
		}, nil
	}

	result, err := fn(ctx, args)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *respondClarificationHandler) Aliases() []string      { return nil }
func (h *respondClarificationHandler) Timeout() time.Duration { return 0 }
func (h *respondClarificationHandler) MaxResultSize() int     { return 0 }
func (h *respondClarificationHandler) SafeForParallel() bool  { return false }
func (h *respondClarificationHandler) Interactive() bool      { return false }
