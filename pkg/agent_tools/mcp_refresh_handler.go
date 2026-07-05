package tools

import (
	"context"
	"time"
)

// MCPRefreshFunc is a function pointer set by pkg/agent at startup.
// It bridges the new ToolHandler interface with the legacy handleMCPRefresh
// implementation that requires *Agent access.
//
// The function signature matches the legacy handler:
//
//	handleMCPRefresh(ctx, args) → JSON string
//
// The agent sets this pointer during initialization, capturing the *Agent
// reference in a closure so the handler doesn't need direct access.
//
// Phase 4 of SP-109 will migrate the execute logic into this package,
// eliminating the need for this indirection.
var MCPRefreshFunc func(ctx context.Context, args map[string]any) (string, error)

// mcpRefreshHandler implements ToolHandler for the mcp_refresh tool.
// It manages MCP (Model Context Protocol) servers at runtime with
// list, refresh, add, and remove operations.
//
// This is a THIN WRAPPER that delegates Execute to the function pointer
// MCPRefreshFunc. All metadata (Name, Definition, Validate, Aliases,
// Timeout, etc.) lives here.
type mcpRefreshHandler struct{}

func (h *mcpRefreshHandler) Name() string { return "mcp_refresh" }

func (h *mcpRefreshHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "mcp_refresh",
		Description: "Reload MCP server config and reconcile servers after config changes. " +
			"Operations: list, refresh, add, remove.",
		Required: []string{"operation"},
		Parameters: []ParameterDef{
			{
				Name:        "operation",
				Type:        "string",
				Required:    true,
				Description: "Operation: list, refresh, add, or remove",
			},
			{
				Name:        "name",
				Type:        "string",
				Description: "Server name (required for add/remove)",
			},
			{
				Name:        "type",
				Type:        "string",
				Description: "Server type: 'stdio' or 'http' (required for add)",
			},
			{
				Name:        "command",
				Type:        "string",
				Description: "Server command (required for add)",
			},
			{
				Name:        "args",
				Type:        "array",
				Description: "Command arguments (optional, for add)",
			},
			{
				Name:        "env",
				Type:        "object",
				Description: "Environment variables (optional, for add)",
			},
			{
				Name:        "url",
				Type:        "string",
				Description: "Server URL (required for HTTP servers)",
			},
			{
				Name:        "working_dir",
				Type:        "string",
				Description: "Working directory (optional, for add)",
			},
		},
	}
}

func (h *mcpRefreshHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "operation")
	return err
}

func (h *mcpRefreshHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	if MCPRefreshFunc == nil {
		return ToolResult{
			Output:  "mcp_refresh is not available: agent integration not initialized (MCPRefreshFunc is nil)",
			IsError: true,
		}, nil
	}

	result, err := MCPRefreshFunc(ctx, args)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *mcpRefreshHandler) Aliases() []string      { return nil }
func (h *mcpRefreshHandler) Timeout() time.Duration { return 30 * time.Second }
func (h *mcpRefreshHandler) MaxResultSize() int     { return 0 }
func (h *mcpRefreshHandler) SafeForParallel() bool  { return false }
func (h *mcpRefreshHandler) Interactive() bool      { return false }
