package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// Executor handles the execution of tools with proper error handling, timeouts, and security
type Executor struct {
	registry    Registry
	permissions PermissionChecker
	logger      *utils.Logger
	config      *config.Config
}

// PermissionChecker checks if operations are allowed
type PermissionChecker interface {
	// HasPermission checks if the given permissions are granted
	HasPermission(permissions []string) bool

	// CheckToolExecution checks if a tool can be executed
	CheckToolExecution(tool Tool, params Parameters) bool
}

// NewExecutor creates a new tool executor
func NewExecutor(registry Registry, permissions PermissionChecker, logger *utils.Logger, config *config.Config) *Executor {
	return &Executor{
		registry:    registry,
		permissions: permissions,
		logger:      logger,
		config:      config,
	}
}

// ExecuteTool executes a tool with the given parameters
func (e *Executor) ExecuteTool(ctx context.Context, tool Tool, params Parameters) (*Result, error) {
	if tool == nil {
		return nil, fmt.Errorf("cannot execute nil tool")
	}

	// Check if tool is available
	if !tool.IsAvailable() {
		return &Result{
			Success: false,
			Errors:  []string{fmt.Sprintf("tool %s is not available", tool.Name())},
		}, nil
	}

	// Check permissions
	if !e.permissions.CheckToolExecution(tool, params) {
		return &Result{
			Success: false,
			Errors:  []string{fmt.Sprintf("insufficient permissions to execute tool %s", tool.Name())},
		}, nil
	}

	// Check if tool can execute with current context
	if !tool.CanExecute(ctx, params) {
		return &Result{
			Success: false,
			Errors:  []string{fmt.Sprintf("tool %s cannot execute with current context", tool.Name())},
		}, nil
	}

	// Set up execution timeout
	execTimeout := tool.EstimatedDuration()
	if params.Timeout > 0 {
		execTimeout = params.Timeout
	}

	execCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	// Log tool execution start
	if e.logger != nil {
		e.logger.LogProcessStep(fmt.Sprintf("🔧 Executing tool: %s (%s)", tool.Name(), tool.Description()))
	}

	// Execute the tool
	startTime := time.Now()
	result, err := tool.Execute(execCtx, params)
	executionTime := time.Since(startTime)

	if result != nil {
		result.ExecutionTime = executionTime
	}

	// Log tool execution end
	if e.logger != nil {
		if err != nil {
			e.logger.LogProcessStep(fmt.Sprintf("❌ Tool %s failed after %v: %v", tool.Name(), executionTime, err))
		} else if result != nil && result.Success {
			e.logger.LogProcessStep(fmt.Sprintf("✅ Tool %s completed successfully in %v", tool.Name(), executionTime))
		} else {
			e.logger.LogProcessStep(fmt.Sprintf("⚠️ Tool %s completed with issues in %v", tool.Name(), executionTime))
		}
	}

	return result, err
}

// ExecuteToolByName executes a tool by name
func (e *Executor) ExecuteToolByName(ctx context.Context, toolName string, params Parameters) (*Result, error) {
	tool, exists := e.registry.GetTool(toolName)
	if !exists {
		return &Result{
			Success: false,
			Errors:  []string{fmt.Sprintf("tool %s not found in registry", toolName)},
		}, nil
	}

	return e.ExecuteTool(ctx, tool, params)
}

// ExecuteToolCall executes a tool call from an LLM response
func (e *Executor) ExecuteToolCall(ctx context.Context, toolCall types.ToolCall) (*Result, error) {
	// Parse the arguments from JSON string to map
	args, err := ParseToolCallArguments(toolCall.Function.Arguments)
	if err != nil {
		return &Result{
			Success: false,
			Errors:  []string{fmt.Sprintf("failed to parse tool arguments: %v", err)},
		}, nil
	}

	// Create tool parameters
	params := Parameters{
		Args:    nil, // Positional args not used in tool calls
		Kwargs:  args,
		Config:  e.config,
		Logger:  e.logger,
		Timeout: 0, // Use tool default
	}

	// Get the tool from registry
	tool, exists := e.registry.GetTool(toolCall.Function.Name)
	if !exists {
		// Fall back to built-in tools for backward compatibility
		return e.executeBuiltinTool(ctx, toolCall.Function.Name, args)
	}

	return e.ExecuteTool(ctx, tool, params)
}

// ListAvailableTools returns a list of all available tools
func (e *Executor) ListAvailableTools() []Tool {
	return e.registry.ListTools()
}

// GetTool retrieves a specific tool by name
func (e *Executor) GetTool(name string) (Tool, bool) {
	return e.registry.GetTool(name)
}

// SimplePermissionChecker is a basic implementation of PermissionChecker
type SimplePermissionChecker struct {
	allowedPermissions map[string]bool
}

// NewSimplePermissionChecker creates a simple permission checker
func NewSimplePermissionChecker(allowedPermissions []string) *SimplePermissionChecker {
	perms := make(map[string]bool)
	for _, perm := range allowedPermissions {
		perms[perm] = true
	}
	return &SimplePermissionChecker{allowedPermissions: perms}
}

// HasPermission checks if the given permissions are granted
func (p *SimplePermissionChecker) HasPermission(permissions []string) bool {
	for _, perm := range permissions {
		if !p.allowedPermissions[perm] {
			return false
		}
	}
	return true
}

// CheckToolExecution checks if a tool can be executed
func (p *SimplePermissionChecker) CheckToolExecution(tool Tool, params Parameters) bool {
	return p.HasPermission(tool.RequiredPermissions())
}
