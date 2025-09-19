package mcp

import (
	"context"
	"fmt"
	"time"
)

// Local constants to avoid importing tools package and creating cycle
const (
	CategoryWeb            = "web"
	PermissionNetworkAccess = "network_access"
)

// Local type definitions to avoid import cycle
type Parameters struct {
	Args   []string
	Kwargs map[string]interface{}
}

type Result struct {
	Success       bool                   `json:"success"`
	Output        interface{}            `json:"output"`
	Errors        []string               `json:"errors"`
	Metadata      map[string]interface{} `json:"metadata"`
	ExecutionTime time.Duration          `json:"execution_time"`
}

// NewMCPToolWrapper creates a new wrapper for an MCP tool to implement the standard Tool interface
func NewMCPToolWrapper(mcpTool MCPTool, manager MCPManager) *MCPToolWrapper {
	return &MCPToolWrapper{
		mcpTool:   mcpTool,
		manager:   manager,
		category:  CategoryWeb, // Default category for MCP tools
		timeout:   30 * time.Second,  // Default timeout
		available: true,
	}
}

// Name returns the unique name of the tool
func (w *MCPToolWrapper) Name() string {
	return fmt.Sprintf("mcp_%s_%s", w.mcpTool.ServerName, w.mcpTool.Name)
}

// Description returns a human-readable description of what the tool does
func (w *MCPToolWrapper) Description() string {
	desc := w.mcpTool.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %s from %s server", w.mcpTool.Name, w.mcpTool.ServerName)
	}
	return fmt.Sprintf("[MCP:%s] %s", w.mcpTool.ServerName, desc)
}

// Category returns the category this tool belongs to
func (w *MCPToolWrapper) Category() string {
	return w.category
}

// Execute runs the tool with the given context and parameters
func (w *MCPToolWrapper) Execute(ctx context.Context, params Parameters) (*Result, error) {
	startTime := time.Now()

	// Extract arguments from parameters
	args := params.Kwargs
	if args == nil {
		args = make(map[string]interface{})
	}

	// Call the MCP tool
	result, err := w.manager.CallTool(ctx, w.mcpTool.ServerName, w.mcpTool.Name, args)
	if err != nil {
		return &Result{
			Success:       false,
			Errors:        []string{err.Error()},
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	// Convert MCP result to standard tool result
	if result.IsError {
		var errors []string
		for _, content := range result.Content {
			if content.Type == "text" {
				errors = append(errors, content.Text)
			}
		}
		return &Result{
			Success:       false,
			Errors:        errors,
			ExecutionTime: time.Since(startTime),
		}, nil
	}

	// Combine content into output
	var output interface{}
	if len(result.Content) == 1 {
		content := result.Content[0]
		switch content.Type {
		case "text":
			output = content.Text
		case "image":
			output = map[string]interface{}{
				"type":     content.Type,
				"data":     content.Data,
				"mimeType": content.MimeType,
			}
		case "resource":
			output = map[string]interface{}{
				"type":     content.Type,
				"text":     content.Text,
				"data":     content.Data,
				"mimeType": content.MimeType,
			}
		default:
			output = content.Text
		}
	} else if len(result.Content) > 1 {
		// Multiple content pieces
		outputs := make([]interface{}, len(result.Content))
		for i, content := range result.Content {
			switch content.Type {
			case "text":
				outputs[i] = content.Text
			case "image", "resource":
				outputs[i] = map[string]interface{}{
					"type":     content.Type,
					"text":     content.Text,
					"data":     content.Data,
					"mimeType": content.MimeType,
				}
			default:
				outputs[i] = content.Text
			}
		}
		output = outputs
	} else {
		output = ""
	}

	metadata := map[string]interface{}{
		"server_name":   w.mcpTool.ServerName,
		"tool_name":     w.mcpTool.Name,
		"content_count": len(result.Content),
		"mcp_source":    true,
	}

	// Add annotations if present
	for _, content := range result.Content {
		if content.Annotations != nil && len(content.Annotations) > 0 {
			metadata["annotations"] = content.Annotations
			break // Just take the first one with annotations
		}
	}

	return &Result{
		Success:       true,
		Output:        output,
		Metadata:      metadata,
		ExecutionTime: time.Since(startTime),
	}, nil
}

// CanExecute checks if the tool can be executed with the current context
func (w *MCPToolWrapper) CanExecute(ctx context.Context, params Parameters) bool {
	// Check if the server is available
	server, exists := w.manager.GetServer(w.mcpTool.ServerName)
	if !exists || !server.IsRunning() {
		return false
	}

	// TODO: Could add schema validation here based on w.mcpTool.InputSchema
	return true
}

// RequiredPermissions returns the permissions needed to execute this tool
func (w *MCPToolWrapper) RequiredPermissions() []string {
	// MCP tools typically need network access to communicate with servers
	permissions := []string{PermissionNetworkAccess}

	// Add permissions based on tool category or server type
	if w.mcpTool.ServerName == "github" {
		permissions = append(permissions, "mcp_github_access")
	}

	// Could analyze the tool's input schema to determine additional permissions
	return permissions
}

// EstimatedDuration returns an estimate of how long the tool will take to execute
func (w *MCPToolWrapper) EstimatedDuration() time.Duration {
	return w.timeout
}

// IsAvailable checks if the tool is available in the current environment
func (w *MCPToolWrapper) IsAvailable() bool {
	if !w.available {
		return false
	}

	// Check if the server is running
	server, exists := w.manager.GetServer(w.mcpTool.ServerName)
	return exists && server.IsRunning()
}

// SetCategory allows customizing the tool category
func (w *MCPToolWrapper) SetCategory(category string) {
	w.category = category
}

// SetTimeout allows customizing the tool timeout
func (w *MCPToolWrapper) SetTimeout(timeout time.Duration) {
	w.timeout = timeout
}

// SetAvailable allows enabling/disabling the tool
func (w *MCPToolWrapper) SetAvailable(available bool) {
	w.available = available
}

// GetMCPTool returns the underlying MCP tool
func (w *MCPToolWrapper) GetMCPTool() MCPTool {
	return w.mcpTool
}

// GetServerName returns the server name
func (w *MCPToolWrapper) GetServerName() string {
	return w.mcpTool.ServerName
}

// GetToolName returns the original tool name (without MCP prefix)
func (w *MCPToolWrapper) GetToolName() string {
	return w.mcpTool.Name
}

// ValidateArgs validates arguments against the tool's input schema
func (w *MCPToolWrapper) ValidateArgs(args map[string]interface{}) error {
	// TODO: Implement JSON schema validation based on w.mcpTool.InputSchema
	// For now, just return nil (no validation)
	return nil
}

// AgentTool represents the agent's tool format for compatibility
type AgentTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Parameters  interface{} `json:"parameters"`
	} `json:"function"`
}

// ToAgentTool converts the MCP tool to the agent's Tool format
func (w *MCPToolWrapper) ToAgentTool() AgentTool {
	return AgentTool{
		Type: "function",
		Function: struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Parameters  interface{} `json:"parameters"`
		}{
			Name:        w.Name(),
			Description: w.Description(),
			Parameters:  w.mcpTool.InputSchema,
		},
	}
}
