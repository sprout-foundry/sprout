package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/utils"
)

// DefaultToolExecutor is the default implementation of ToolExecutor
type DefaultToolExecutor struct {
	registry    ToolRegistry
	permissions PermissionChecker
	logger      *utils.Logger
	config      *config.Config
}

// PermissionChecker checks if operations are allowed
type PermissionChecker interface {
	// HasPermission checks if the given permissions are granted
	HasPermission(permissions []string) bool

	// CheckToolExecution checks if a tool can be executed
	CheckToolExecution(tool Tool, params ToolParameters) bool
}

// NewToolExecutor creates a new tool executor
func NewToolExecutor(registry ToolRegistry, permissions PermissionChecker, logger *utils.Logger, config *config.Config) ToolExecutor {
	return &DefaultToolExecutor{
		registry:    registry,
		permissions: permissions,
		logger:      logger,
		config:      config,
	}
}

// ExecuteTool executes a tool with the given parameters
func (e *DefaultToolExecutor) ExecuteTool(ctx context.Context, tool Tool, params ToolParameters) (*ToolResult, error) {
	if tool == nil {
		return nil, fmt.Errorf("cannot execute nil tool")
	}

	// Check if tool is available
	if !tool.IsAvailable() {
		return &ToolResult{
			Success: false,
			Errors:  []string{fmt.Sprintf("tool %s is not available", tool.Name())},
		}, nil
	}

	// Check permissions
	if !e.permissions.CheckToolExecution(tool, params) {
		return &ToolResult{
			Success: false,
			Errors:  []string{fmt.Sprintf("insufficient permissions to execute tool %s", tool.Name())},
		}, nil
	}

	// Check if tool can execute with current context
	if !tool.CanExecute(ctx, params) {
		return &ToolResult{
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
func (e *DefaultToolExecutor) ExecuteToolByName(ctx context.Context, name string, params ToolParameters) (*ToolResult, error) {
	tool, exists := e.registry.Get(name)
	if !exists {
		return &ToolResult{
			Success: false,
			Errors:  []string{fmt.Sprintf("tool %s not found", name)},
		}, fmt.Errorf("tool %s not found", name)
	}

	return e.ExecuteTool(ctx, tool, params)
}

// CanExecute checks if a tool can be executed
func (e *DefaultToolExecutor) CanExecute(ctx context.Context, tool Tool, params ToolParameters) bool {
	if tool == nil {
		return false
	}

	if !tool.IsAvailable() {
		return false
	}

	if !e.permissions.CheckToolExecution(tool, params) {
		return false
	}

	return tool.CanExecute(ctx, params)
}

// GetToolRegistry returns the tool registry
func (e *DefaultToolExecutor) GetToolRegistry() ToolRegistry {
	return e.registry
}

// SimplePermissionChecker is a basic implementation of PermissionChecker
type SimplePermissionChecker struct {
	allowedPermissions []string
	allowAll           bool
}

// NewSimplePermissionChecker creates a new simple permission checker
func NewSimplePermissionChecker(allowedPermissions []string, allowAll bool) PermissionChecker {
	return &SimplePermissionChecker{
		allowedPermissions: allowedPermissions,
		allowAll:           allowAll,
	}
}

// HasPermission checks if the given permissions are granted
func (p *SimplePermissionChecker) HasPermission(permissions []string) bool {
	if p.allowAll {
		return true
	}

	for _, required := range permissions {
		found := false
		for _, allowed := range p.allowedPermissions {
			if required == allowed {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// CheckToolExecution checks if a tool can be executed
func (p *SimplePermissionChecker) CheckToolExecution(tool Tool, params ToolParameters) bool {
	return p.HasPermission(tool.RequiredPermissions())
}
