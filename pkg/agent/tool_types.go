package agent

import (
	"context"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// Tool represents a pluggable agent tool that can be executed
type Tool interface {
	// Name returns the unique name of the tool
	Name() string

	// Description returns a human-readable description of what the tool does
	Description() string

	// Category returns the category this tool belongs to (e.g., "workspace", "search", "edit")
	Category() string

	// Execute runs the tool with the given context and parameters
	Execute(ctx context.Context, params ToolParameters) (*ToolResult, error)

	// CanExecute checks if the tool can be executed with the current context
	CanExecute(ctx context.Context, params ToolParameters) bool

	// RequiredPermissions returns the permissions needed to execute this tool
	RequiredPermissions() []string

	// EstimatedDuration returns an estimate of how long the tool will take to execute
	EstimatedDuration() time.Duration

	// IsAvailable checks if the tool is available in the current environment
	IsAvailable() bool
}

// ToolParameters contains the parameters passed to a tool
type ToolParameters struct {
	// Args contains positional arguments
	Args []string

	// Kwargs contains keyword arguments
	Kwargs map[string]interface{}

	// Context provides access to the agent context
	Context *AgentContext

	// Config provides access to configuration
	Config *config.Config

	// Logger for tool execution logging
	Logger *utils.Logger

	// Timeout for tool execution
	Timeout time.Duration
}

// ToolResult contains the result of a tool execution
type ToolResult struct {
	// Success indicates if the tool execution was successful
	Success bool

	// Output contains the main output of the tool
	Output string

	// Data contains structured data output
	Data map[string]interface{}

	// Files contains any files that were affected
	Files []string

	// Errors contains any errors that occurred
	Errors []string

	// Warnings contains any warnings generated
	Warnings []string

	// Metadata contains additional metadata about the execution
	Metadata map[string]interface{}

	// ExecutionTime contains the actual execution time
	ExecutionTime time.Duration

	// TokenUsage contains token usage information
	TokenUsage *types.AgentTokenUsage
}

// ToolRegistry manages the registration and discovery of tools
type ToolRegistry interface {
	// Register registers a new tool
	Register(tool Tool) error

	// Get retrieves a tool by name
	Get(name string) (Tool, bool)

	// List returns all registered tools
	List() []Tool

	// ListByCategory returns tools filtered by category
	ListByCategory(category string) []Tool

	// IsRegistered checks if a tool is registered
	IsRegistered(name string) bool

	// Unregister removes a tool from the registry
	Unregister(name string) error
}

// ToolExecutor handles the execution of tools with proper error handling and timeouts
type ToolExecutor interface {
	// ExecuteTool executes a tool with the given parameters
	ExecuteTool(ctx context.Context, tool Tool, params ToolParameters) (*ToolResult, error)

	// ExecuteToolByName executes a tool by name
	ExecuteToolByName(ctx context.Context, name string, params ToolParameters) (*ToolResult, error)

	// CanExecute checks if a tool can be executed
	CanExecute(ctx context.Context, tool Tool, params ToolParameters) bool

	// GetToolRegistry returns the tool registry
	GetToolRegistry() ToolRegistry
}

// ToolChain represents a chain of tools that can be executed sequentially
type ToolChain struct {
	// Name is the name of the tool chain
	Name string

	// Description describes what this chain does
	Description string

	// Tools contains the sequence of tools to execute
	Tools []ToolChainStep

	// StopOnError determines if the chain should stop on the first error
	StopOnError bool

	// MaxExecutionTime is the maximum time for the entire chain
	MaxExecutionTime time.Duration
}

// ToolChainStep represents a single step in a tool chain
type ToolChainStep struct {
	// Tool is the tool to execute
	Tool Tool

	// Parameters are the parameters to pass to the tool
	Parameters ToolParameters

	// ContinueOnError determines if the chain should continue if this step fails
	ContinueOnError bool

	// Condition is a function that determines if this step should be executed
	Condition func(ctx context.Context, params ToolParameters) bool
}

// ToolChainResult contains the result of executing a tool chain
type ToolChainResult struct {
	// Success indicates if the entire chain was successful
	Success bool

	// StepResults contains the results of each step
	StepResults []*ToolResult

	// ExecutionTime contains the total execution time
	ExecutionTime time.Duration

	// Error contains any error that stopped the chain
	Error error
}

// ToolChainExecutor executes tool chains
type ToolChainExecutor interface {
	// ExecuteChain executes a tool chain
	ExecuteChain(ctx context.Context, chain *ToolChain) (*ToolChainResult, error)

	// ValidateChain validates that a tool chain is properly configured
	ValidateChain(chain *ToolChain) error
}

// BaseTool provides a basic implementation of the Tool interface
type BaseTool struct {
	name              string
	description       string
	category          string
	requiredPerms     []string
	estimatedDuration time.Duration
	availabilityCheck func() bool
}

// NewBaseTool creates a new BaseTool
func NewBaseTool(name, description, category string, perms []string, duration time.Duration) *BaseTool {
	return &BaseTool{
		name:              name,
		description:       description,
		category:          category,
		requiredPerms:     perms,
		estimatedDuration: duration,
		availabilityCheck: func() bool { return true },
	}
}

// Name returns the tool name
func (t *BaseTool) Name() string {
	return t.name
}

// Description returns the tool description
func (t *BaseTool) Description() string {
	return t.description
}

// Category returns the tool category
func (t *BaseTool) Category() string {
	return t.category
}

// RequiredPermissions returns the required permissions
func (t *BaseTool) RequiredPermissions() []string {
	return t.requiredPerms
}

// EstimatedDuration returns the estimated execution duration
func (t *BaseTool) EstimatedDuration() time.Duration {
	return t.estimatedDuration
}

// IsAvailable checks if the tool is available
func (t *BaseTool) IsAvailable() bool {
	if t.availabilityCheck != nil {
		return t.availabilityCheck()
	}
	return true
}

// SetAvailabilityCheck sets the availability check function
func (t *BaseTool) SetAvailabilityCheck(check func() bool) {
	t.availabilityCheck = check
}
