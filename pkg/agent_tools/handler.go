// Tool handler interface and execution context types.
//
// ToolHandler defines the contract each tool implementation must satisfy.
// ToolEnv carries explicit dependencies (no *Agent). ToolResult captures
// the output of tool execution.
package tools

import (
	"context"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// ToolHandler is the interface that all tool implementations must satisfy.
// Each tool provides its name, its LLM-facing definition, argument validation,
// and execution logic. ToolEnv carries explicit dependencies — no *Agent dependency.
type ToolHandler interface {
	Name() string
	Definition() api.Tool
	Validate(args map[string]any) error
	Execute(ctx context.Context, env *ToolEnv, args map[string]any) (*ToolResult, error)
}

// ToolEnv carries explicit dependencies for tool execution.
// Fields are added as needed. This struct must not depend on *Agent to avoid
// circular imports and to keep tools independently testable.
type ToolEnv struct {
	WorkingDir      string
	ClientID        string
	ConfigManager   *configuration.Manager
	EventBus        *events.EventBus
}

// ToolResult is the output of a tool execution.
type ToolResult struct {
	Output        string `json:"output"`
	StructuredOut any    `json:"structured_out,omitempty"`
	TokenUsage    int    `json:"token_usage"`
	Error         error  `json:"-"`              // Typed error for Go callers; excluded from JSON
	ErrorMessage  string `json:"error,omitempty"` // Human-readable message for JSON consumers
}
