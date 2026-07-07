package tools

import (
	"context"
	"time"
)

// RequestClarificationFunc is a function pointer set by pkg/agent at startup.
// It bridges the new ToolHandler interface with the legacy
// handleRequestClarification implementation that requires *Agent access.
//
// The function signature matches the legacy handler:
//
//	handleRequestClarification(ctx, args) → string, error
//
// The agent sets this pointer during initialization, capturing the *Agent
// reference in a closure so the handler doesn't need direct access.
//
// Phase 4 of SP-109 will migrate the execute logic into this package,
// eliminating the need for this indirection.
var RequestClarificationFunc func(ctx context.Context, args map[string]any) (string, error)

// requestClarificationHandler implements ToolHandler for the
// request_clarification tool. It is called by subagents when they need
// clarification from their parent agent.
//
// This is a THIN WRAPPER that delegates Execute to the function pointer
// RequestClarificationFunc. All metadata (Name, Definition, Validate,
// Aliases, Timeout, etc.) lives here.
type requestClarificationHandler struct{}

func (h *requestClarificationHandler) Name() string { return "request_clarification" }

func (h *requestClarificationHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "request_clarification",
		Description: "Request clarification from the parent agent when you encounter ambiguity " +
			"or need additional context during execution. The parent will receive your question " +
			"and can respond with guidance. This tool will block until a response is received " +
			"or a timeout expires.",
		Required: []string{"question"},
		Parameters: []ParameterDef{
			{
				Name:        "question",
				Type:        "string",
				Required:    true,
				Description: "What you need clarification on",
			},
		},
	}
}

func (h *requestClarificationHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "question")
	return err
}

func (h *requestClarificationHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	if RequestClarificationFunc == nil {
		return ToolResult{
			Output:  "request_clarification is not available: agent integration not initialized (RequestClarificationFunc is nil)",
			IsError: true,
		}, nil
	}

	result, err := RequestClarificationFunc(ctx, args)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *requestClarificationHandler) Aliases() []string      { return nil }
func (h *requestClarificationHandler) Timeout() time.Duration { return 65 * time.Second }
func (h *requestClarificationHandler) MaxResultSize() int     { return 0 }
func (h *requestClarificationHandler) SafeForParallel() bool  { return false }
func (h *requestClarificationHandler) Interactive() bool      { return true }
