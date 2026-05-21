package tools

import (
	"context"
	"io"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// ToolHandler defines the interface for a tool that can be invoked by the agent.
type ToolHandler interface {
	// Name returns the unique tool identifier (e.g., "read_file").
	Name() string
	// Definition returns the JSON schema definition for the LLM to understand the tool.
	Definition() ToolDefinition
	// Validate checks arguments before execution. Returns error if invalid.
	Validate(args map[string]any) error
	// Execute runs the tool with the given context, environment, and arguments.
	Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error)
}

// ToolDefinition describes a tool's schema for LLM consumption.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  []ParameterDef `json:"parameters"`
	Required    []string       `json:"required,omitempty"` // Required parameter names
}

// ParameterDef defines a single tool parameter's schema.
type ParameterDef struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

// ToolEnv provides the execution context for a tool without coupling to *Agent.
type ToolEnv struct {
	// EventBus for publishing events (tool_start, tool_end, etc.)
	EventBus *events.EventBus
	// WorkspaceRoot is the working directory root for path resolution
	WorkspaceRoot string
	// OutputWriter for writing tool output (stdout, logs, etc.)
	OutputWriter io.Writer
	// ApprovalManager for security approvals; nil if approvals are not supported
	ApprovalManager ApprovalManager
	// MaxTokensFunc returns the current token budget limit
	MaxTokensFunc func() int
	// ConfigManager provides configuration access for tools that need it (e.g., API keys for web fetching)
	ConfigManager *configuration.Manager
}

// ApprovalResult contains the outcome of an approval request.
type ApprovalResult struct {
	Approved    bool   `json:"approved"`
	Reason      string `json:"reason,omitempty"`     // "rejected", "timed_out", "cancelled"
	UserComment string `json:"user_comment,omitempty"` // Optional feedback from user
}

// ApprovalManager handles security approval requests for tool execution.
type ApprovalManager interface {
	// RequestApproval asks the user to approve a tool execution.
	// Returns an ApprovalResult with the outcome and optional context.
	RequestApproval(requestID, toolName, riskLevel, prompt string, extras map[string]string) ApprovalResult
}

// ImageData represents an image returned by a vision-capable tool.
type ImageData struct {
	// URI is the path or data URI of the image
	URI string `json:"uri"`
	// MIMEType is the image MIME type (e.g., "image/png")
	MIMEType string `json:"mime_type"`
}

// ToolResult is the return value from a tool's Execute method.
type ToolResult struct {
	// Output is the primary text result of the tool execution.
	Output string `json:"output"`
	// StructuredOut holds optional structured data (maps, slices, etc.)
	StructuredOut any `json:"structured_out,omitempty"`
	// Images contains optional image data for vision-capable tools.
	Images []ImageData `json:"images,omitempty"`
	// TokenUsage tracks tokens consumed during execution.
	TokenUsage int64 `json:"token_usage"`
	// IsError indicates whether this result represents an error state.
	IsError bool `json:"is_error"`
}
