package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// Executor handles the execution of tools with proper error handling, timeouts, and security
type Executor struct {
	registry       Registry
	permissions    PermissionChecker
	logger         *utils.Logger
	config         *config.Config
	sessionTracker *SessionTracker
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
		registry:       registry,
		permissions:    permissions,
		logger:         logger,
		config:         config,
		sessionTracker: GetGlobalSessionTracker(),
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

	// Normalize common argument aliases per tool to be resilient to LLM variations
	args = normalizeArgsForTool(toolCall.Function.Name, args)

	// Check for duplicate requests in the same session
	sessionID := e.getSessionID(ctx)
	if sessionID != "" {
		if isDuplicate, callInfo := e.sessionTracker.IsDuplicateRequest(sessionID, toolCall.Function.Name, args); isDuplicate {
			return e.handleDuplicateRequest(toolCall.Function.Name, args, callInfo)
		}
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
		result, err := e.executeBuiltinTool(ctx, toolCall.Function.Name, args)

		// Record the tool call in session tracker if successful
		if sessionID != "" && err == nil && result != nil && result.Success {
			responseStr := ""
			if output, ok := result.Output.(string); ok {
				responseStr = output
			}
			e.sessionTracker.RecordToolCall(sessionID, toolCall.Function.Name, args, responseStr)
		}

		return result, err
	}

	result, err := e.ExecuteTool(ctx, tool, params)

	// Record the tool call in session tracker if successful
	if sessionID != "" && err == nil && result != nil && result.Success {
		responseStr := ""
		if output, ok := result.Output.(string); ok {
			responseStr = output
		}
		e.sessionTracker.RecordToolCall(sessionID, toolCall.Function.Name, args, responseStr)
	}

	return result, err
}

// normalizeArgsForTool maps common alias parameter names to expected names per tool
func normalizeArgsForTool(toolName string, args map[string]interface{}) map[string]interface{} {
	if args == nil {
		return args
	}
	switch toolName {
	case "read_file":
		if _, ok := args["file_path"]; !ok {
			if v, ok := args["target_file"]; ok {
				args["file_path"] = v
			}
			if v, ok := args["path"]; ok {
				args["file_path"] = v
			}
			if v, ok := args["filename"]; ok {
				args["file_path"] = v
			}
		}
	case "run_shell_command":
		if _, ok := args["command"]; !ok {
			if v, ok := args["cmd"]; ok {
				args["command"] = v
			}
		}
	case "edit_file_section":
		if _, ok := args["file_path"]; !ok {
			if v, ok := args["target_file"]; ok {
				args["file_path"] = v
			}
			if v, ok := args["filename"]; ok {
				args["file_path"] = v
			}
		}
		if _, ok := args["old_text"]; !ok {
			if v, ok := args["old_string"]; ok {
				args["old_text"] = v
			}
			if v, ok := args["from"]; ok {
				args["old_text"] = v
			}
		}
		if _, ok := args["new_text"]; !ok {
			if v, ok := args["new_string"]; ok {
				args["new_text"] = v
			}
			if v, ok := args["to"]; ok {
				args["new_text"] = v
			}
		}
	case "workspace_context":
		// Ensure action/query presence if provided via aliases
		if _, ok := args["action"]; !ok {
			if v, ok := args["op"]; ok {
				args["action"] = v
			}
		}
		if v, ok := args["action"].(string); ok {
			if strings.EqualFold(v, "search") {
				args["action"] = "search"
			}
		}
		if _, ok := args["query"]; !ok {
			if v, ok := args["keywords"]; ok {
				args["query"] = v
			}
		}
	}
	return args
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

// getSessionID extracts session ID from context
func (e *Executor) getSessionID(ctx context.Context) string {
	if sessionID, ok := ctx.Value("session_id").(string); ok {
		return sessionID
	}
	return ""
}

// handleDuplicateRequest handles duplicate tool call requests
func (e *Executor) handleDuplicateRequest(toolName string, args map[string]interface{}, callInfo *ToolCallInfo) (*Result, error) {
	if toolName == "read_file" {
		// For read_file duplicates, return a special response indicating the file was already requested
		filePath, _ := args["file_path"].(string)
		response := fmt.Sprintf("⚡ Duplicate request detected: File '%s' has already been read %d times in this session (first read: %s, last read: %s). You already have this file content available.",
			filePath, callInfo.CallCount, callInfo.FirstCall.Format("15:04:05"), callInfo.LastCall.Format("15:04:05"))

		if e.logger != nil {
			e.logger.LogProcessStep(fmt.Sprintf("Duplicate read_file request blocked: %s", filePath))
		}

		return &Result{
			Success: true,
			Output:  response,
			Metadata: map[string]interface{}{
				"duplicate_request": true,
				"tool_name":         toolName,
				"file_path":         filePath,
				"call_count":        callInfo.CallCount,
				"first_call":        callInfo.FirstCall,
				"last_call":         callInfo.LastCall,
			},
		}, nil
	}

	// For other tools, return a generic duplicate response
	response := fmt.Sprintf("⚡ Duplicate request detected: This %s call has been made %d times in this session. Please avoid redundant requests.",
		toolName, callInfo.CallCount)

	if e.logger != nil {
		e.logger.LogProcessStep(fmt.Sprintf("Duplicate %s request blocked", toolName))
	}

	return &Result{
		Success: true,
		Output:  response,
		Metadata: map[string]interface{}{
			"duplicate_request": true,
			"tool_name":         toolName,
			"call_count":        callInfo.CallCount,
		},
	}, nil
}

// StartSession starts a new session for tracking tool calls
func (e *Executor) StartSession() string {
	return e.sessionTracker.StartSession()
}

// EndSession ends a session and cleans up tracking data
func (e *Executor) EndSession(sessionID string) {
	e.sessionTracker.EndSession(sessionID)
}

// GetSessionStats returns statistics about a session
func (e *Executor) GetSessionStats(sessionID string) map[string]interface{} {
	return e.sessionTracker.GetSessionStats(sessionID)
}
