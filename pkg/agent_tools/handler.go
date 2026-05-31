// Package tools provides the interface-based tool system for the Sprout AI agent.
//
// Tools are capabilities the LLM can invoke — reading files, executing shell commands,
// searching code, delegating to subagents, automating browsers, and more. Each tool
// implements the ToolHandler interface and is registered with the ToolRegistry.
//
//
// ToolHandler interface
//
// The ToolHandler interface replaced the legacy `type ToolHandler func(ctx, args,
// agent) (images, output, error)` func type. The old func-based system tightly coupled
// every tool to the *Agent type, making tools hard to test in isolation and difficult
// to share across different execution contexts. The new interface-based system provides
// explicit dependencies through ToolEnv, enabling clean separation of concerns:
//
//	type ToolHandler interface {
//	    Name() string
//	    Definition() ToolDefinition
//	    Validate(args map[string]any) error
//	    Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error)
//	}
//
//
// Adding a new tool
//
// 1. Create a new file in this package (e.g., `my_tool_handler.go`).
// 2. Define a struct and implement the four ToolHandler methods.
// 3. Register it in `AllTools()` in `all.go`.
//
// See AGENTS.md for tool documentation and conventions.
//
//
// Migration from legacy func-style handlers
//
// The legacy tool system used function types directly coupled to *Agent. The new
// interface-based system decouples tools from the agent via ToolEnv, which provides
// explicit dependencies (EventBus, WorkspaceRoot, OutputWriter, etc.).
//
// During the migration period, a dual-dispatch shim in pkg/agent/tool_definitions.go
// bridges both systems: when ExecuteTool() is called, it first checks the new registry
// via tools.GetNewToolRegistry().Lookup(name). If a handler is found there, it builds
// a ToolEnv from the agent context and dispatches through the new interface. If no
// handler exists in the new registry, it falls back to the legacy func-style handlers.
// This allows incremental migration without breaking existing functionality.
//
// Some current tools (e.g., browseURLHandler, runSubagentHandler) are thin wrappers
// around legacy agent methods, pending full refactoring. These are marked with comments
// in all.go.
package tools

import (
	"context"
	"io"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
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
	// EmbeddingMgr is the agent's long-lived embedding manager. When set, tools
	// must reuse it instead of constructing their own — the manager holds the
	// loaded ONNX model and an open HNSW handle, so per-call construction is
	// both slow and unsafe under concurrent writes.
	EmbeddingMgr *embedding.EmbeddingManager
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