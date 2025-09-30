package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// MCPServerConfig represents the configuration for an MCP server
type MCPServerConfig struct {
	Name        string            `json:"name"`
	Type        string            `json:"type,omitempty"`    // "stdio" or "http"
	Command     string            `json:"command,omitempty"` // For stdio servers
	Args        []string          `json:"args,omitempty"`    // For stdio servers
	URL         string            `json:"url,omitempty"`     // For HTTP servers
	Env         map[string]string `json:"env,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"` // For stdio servers
	Timeout     time.Duration     `json:"timeout,omitempty"`
	AutoStart   bool              `json:"auto_start"`
	MaxRestarts int               `json:"max_restarts"`
}

// UnmarshalJSON implements custom JSON unmarshaling for MCPServerConfig to handle timeout as string or duration
func (s *MCPServerConfig) UnmarshalJSON(data []byte) error {
	// Create an alias to avoid infinite recursion
	type MCPServerConfigAlias MCPServerConfig

	// First try to unmarshal as the normal struct
	aux := &struct {
		Timeout interface{} `json:"timeout"`
		*MCPServerConfigAlias
	}{
		MCPServerConfigAlias: (*MCPServerConfigAlias)(s),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	// Handle timeout field conversion
	if aux.Timeout != nil {
		switch v := aux.Timeout.(type) {
		case string:
			// Parse string duration (backward compatibility)
			if v != "" {
				duration, err := time.ParseDuration(v)
				if err != nil {
					return fmt.Errorf("invalid timeout duration: %w", err)
				}
				s.Timeout = duration
			} else {
				s.Timeout = 30 * time.Second // default
			}
		case float64:
			// Handle JSON number (nanoseconds)
			s.Timeout = time.Duration(v)
		default:
			s.Timeout = 30 * time.Second // default fallback
		}
	} else {
		s.Timeout = 30 * time.Second // default if not present
	}

	return nil
}

// MCPTool represents a tool available via MCP
type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
	ServerName  string                 `json:"server_name"`
}

// MCPResource represents a resource available via MCP
type MCPResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	ServerName  string `json:"server_name"`
}

// MCPPrompt represents a prompt available via MCP
type MCPPrompt struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Arguments   []MCPPromptArgument `json:"arguments,omitempty"`
	ServerName  string              `json:"server_name"`
}

// MCPPromptArgument represents a prompt argument
type MCPPromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

// MCPMessage represents an MCP protocol message
type MCPMessage struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method,omitempty"`
	Params  interface{} `json:"params,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPError represents an MCP protocol error
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCPToolCallRequest represents a tool call request
type MCPToolCallRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// MCPToolCallResult represents a tool call result
type MCPToolCallResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError"`
}

// MCPContent represents content in MCP responses
type MCPContent struct {
	Type        string                 `json:"type"`
	Text        string                 `json:"text,omitempty"`
	Data        string                 `json:"data,omitempty"`
	MimeType    string                 `json:"mimeType,omitempty"`
	Annotations map[string]interface{} `json:"annotations,omitempty"`
}

// MCPServer represents an MCP server interface
type MCPServer interface {
	// Start starts the MCP server process
	Start(ctx context.Context) error

	// Stop stops the MCP server process
	Stop(ctx context.Context) error

	// IsRunning checks if the server is running
	IsRunning() bool

	// GetName returns the server name
	GetName() string

	// GetConfig returns the server configuration
	GetConfig() MCPServerConfig

	// Initialize sends initialize request to server
	Initialize(ctx context.Context) error

	// ListTools lists available tools from the server
	ListTools(ctx context.Context) ([]MCPTool, error)

	// CallTool calls a tool on the server
	CallTool(ctx context.Context, request MCPToolCallRequest) (*MCPToolCallResult, error)

	// ListResources lists available resources from the server
	ListResources(ctx context.Context) ([]MCPResource, error)

	// ReadResource reads a resource from the server
	ReadResource(ctx context.Context, uri string) (*MCPContent, error)

	// ListPrompts lists available prompts from the server
	ListPrompts(ctx context.Context) ([]MCPPrompt, error)

	// GetPrompt gets a prompt from the server
	GetPrompt(ctx context.Context, name string, args map[string]interface{}) (*MCPContent, error)
}

// MCPManager manages multiple MCP servers
type MCPManager interface {
	// AddServer adds a new MCP server
	AddServer(config MCPServerConfig) error

	// RemoveServer removes an MCP server
	RemoveServer(name string) error

	// GetServer gets an MCP server by name
	GetServer(name string) (MCPServer, bool)

	// ListServers lists all registered servers
	ListServers() []MCPServer

	// StartAll starts all configured servers
	StartAll(ctx context.Context) error

	// StopAll stops all running servers
	StopAll(ctx context.Context) error

	// GetAllTools gets all tools from all running servers
	GetAllTools(ctx context.Context) ([]MCPTool, error)

	// CallTool calls a tool on the appropriate server
	CallTool(ctx context.Context, serverName, toolName string, args map[string]interface{}) (*MCPToolCallResult, error)
}

// MCPToolWrapper wraps an MCP tool to implement the standard Tool interface
type MCPToolWrapper struct {
	mcpTool   MCPTool
	manager   MCPManager
	category  string
	timeout   time.Duration
	available bool
}

// Standard error codes
const (
	ErrorCodeParse          = -32700
	ErrorCodeInvalidRequest = -32600
	ErrorCodeMethodNotFound = -32601
	ErrorCodeInvalidParams  = -32602
	ErrorCodeInternalError  = -32603
)
