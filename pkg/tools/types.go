package tools

import (
	"context"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/utils"
)

// Tool represents a pluggable tool that can be executed
type Tool interface {
	// Name returns the unique name of the tool
	Name() string

	// Description returns a human-readable description of what the tool does
	Description() string

	// Category returns the category this tool belongs to (e.g., "workspace", "search", "edit")
	Category() string

	// Execute runs the tool with the given context and parameters
	Execute(ctx context.Context, params Parameters) (*Result, error)

	// CanExecute checks if the tool can be executed with the current context
	CanExecute(ctx context.Context, params Parameters) bool

	// RequiredPermissions returns the permissions needed to execute this tool
	RequiredPermissions() []string

	// EstimatedDuration returns an estimate of how long the tool will take to execute
	EstimatedDuration() time.Duration

	// IsAvailable checks if the tool is available in the current environment
	IsAvailable() bool
}

// Parameters contains the parameters passed to a tool
type Parameters struct {
	// Args contains positional arguments
	Args []string

	// Kwargs contains keyword arguments
	Kwargs map[string]interface{}

	// Config provides access to configuration
	Config *config.Config

	// Logger for tool execution logging
	Logger *utils.Logger

	// Timeout for tool execution
	Timeout time.Duration
}

// Result represents the outcome of a tool execution
type Result struct {
	Success       bool                   `json:"success"`
	Output        interface{}            `json:"output"`
	Errors        []string               `json:"errors"`
	Metadata      map[string]interface{} `json:"metadata"`
	ExecutionTime time.Duration          `json:"execution_time"`
}

// Registry manages available tools
type Registry interface {
	// RegisterTool registers a new tool
	RegisterTool(tool Tool) error

	// GetTool retrieves a tool by name
	GetTool(name string) (Tool, bool)

	// UnregisterTool removes a tool from the registry
	UnregisterTool(name string) error

	// ListTools returns all registered tools
	ListTools() []Tool

	// ListToolsByCategory returns tools in a specific category
	ListToolsByCategory(category string) []Tool
}

// Category constants for organizing tools
const (
	CategoryWorkspace = "workspace"
	CategoryFile      = "file"
	CategorySearch    = "search"
	CategoryEdit      = "edit"
	CategoryShell     = "shell"
	CategoryUser      = "user"
	CategoryWeb       = "web"
	CategoryAnalysis  = "analysis"
)

// Permission constants for tool security
const (
	PermissionReadFile       = "read_file"
	PermissionWriteFile      = "write_file"
	PermissionExecuteShell   = "execute_shell"
	PermissionNetworkAccess  = "network_access"
	PermissionUserPrompt     = "user_prompt"
	PermissionWorkspaceRead  = "workspace_read"
	PermissionWorkspaceWrite = "workspace_write"
)
