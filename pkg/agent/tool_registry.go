package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/security_validator"
	"github.com/alantheprice/ledit/pkg/utils"
)

const (
	MAX_SUBAGENT_OUTPUT_SIZE    = 10 * 1024 * 1024 // 10MB
	MAX_SUBAGENT_CONTEXT_SIZE   = 1024 * 1024      // 1MB
	MAX_PARALLEL_SUBAGENTS      = 5
)

// ToolHandler represents a function that can handle a tool execution
type ToolHandler func(ctx context.Context, a *Agent, args map[string]interface{}) (string, error)

// ParameterConfig defines parameter validation rules for a tool
type ParameterConfig struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"` // "string", "int", "float64", "bool"
	Required     bool     `json:"required"`
	Alternatives []string `json:"alternatives"` // Alternative parameter names for backward compatibility
	Description  string   `json:"description"`
}

// ToolConfig holds configuration for a tool
type ToolConfig struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Parameters  []ParameterConfig `json:"parameters"`
	Handler     ToolHandler       `json:"-"` // Function reference, not serialized
}

// ToolRegistry manages tool configurations in a data-driven way
type ToolRegistry struct {
	tools     map[string]ToolConfig
	validator *security_validator.Validator
}

var defaultToolRegistry *ToolRegistry
var registryOnce sync.Once

// GetToolRegistry returns the default tool registry, initializing it lazily if needed (thread-safe)
func GetToolRegistry() *ToolRegistry {
	registryOnce.Do(func() {
		defaultToolRegistry = newDefaultToolRegistry()
	})
	return defaultToolRegistry
}

// InitializeToolRegistry pre-creates the tool registry to avoid first-use overhead
// This should be called during agent initialization for better performance
func InitializeToolRegistry() {
	registryOnce.Do(func() {
		defaultToolRegistry = newDefaultToolRegistry()
	})
}

// newDefaultToolRegistry creates the registry with all tool configurations
func newDefaultToolRegistry() *ToolRegistry {
	registry := &ToolRegistry{
		tools: make(map[string]ToolConfig),
	}

	// Register shell_command tool
	registry.RegisterTool(ToolConfig{
		Name:        "shell_command",
		Description: "Execute a shell command",
		Parameters: []ParameterConfig{
			{"command", "string", true, []string{"cmd"}, "The shell command to execute"},
		},
		Handler: handleShellCommand,
	})

	// Register read_file tool
	registry.RegisterTool(ToolConfig{
		Name:        "read_file",
		Description: "Read contents of a file",
		Parameters: []ParameterConfig{
			{"file_path", "string", true, []string{}, "Path to the file to read"},
			{"start_line", "int", false, []string{}, "Starting line number (optional)"},
			{"end_line", "int", false, []string{}, "Ending line number (optional)"},
		},
		Handler: handleReadFile,
	})

	// Register write_file tool
	registry.RegisterTool(ToolConfig{
		Name:        "write_file",
		Description: "Write content to a file",
		Parameters: []ParameterConfig{
			{"file_path", "string", true, []string{}, "Path to the file to write"},
			{"content", "string", true, []string{}, "Content to write to the file"},
		},
		Handler: handleWriteFile,
	})

	// Register edit_file tool
	registry.RegisterTool(ToolConfig{
		Name:        "edit_file",
		Description: "Edit a file by replacing old string with new string",
		Parameters: []ParameterConfig{
			{"file_path", "string", true, []string{}, "Path to the file to edit"},
			{"old_string", "string", true, []string{}, "String to replace"},
			{"new_string", "string", true, []string{}, "Replacement string"},
		},
		Handler: handleEditFile,
	})

	// Register todo tools
	// Register add_todos (handles both single and bulk)
	registry.RegisterTool(ToolConfig{
		Name:        "add_todos",
		Description: "Add multiple todo items at once. Use single-todo add only for rare cases. Returns todo IDs for reference.",
		Parameters: []ParameterConfig{
			{"todos", "array", true, []string{}, "Array of todos: [{title, description?, priority?}]"},
		},
		Handler: handleAddTodos,
	})

	registry.RegisterTool(ToolConfig{
		Name:        "update_todo_status",
		Description: "Update the status of a single todo. Accepts id as 'todo_1', '1', or the todo title. For multiple updates, use update_todo_status_bulk.",
		Parameters: []ParameterConfig{
			{"id", "string", true, []string{}, "Todo identifier (format: 'todo_1', numeric '1', or todo title)"},
			{"status", "string", true, []string{}, "New status (one of: pending, in_progress, completed, cancelled)"},
		},
		Handler: handleUpdateTodoStatus,
	})

	registry.RegisterTool(ToolConfig{
		Name:        "update_todo_status_bulk",
		Description: "Update the status of multiple todo items efficiently. Accepts IDs as 'todo_1', numeric '1', or titles.",
		Parameters: []ParameterConfig{
			{"updates", "array", true, []string{}, "Array of updates: [{id, status}, ...] where status is one of: pending, in_progress, completed, cancelled"},
		},
		Handler: handleUpdateTodoStatusBulk,
	})

	registry.RegisterTool(ToolConfig{
		Name:        "list_todos",
		Description: "List all todo items",
		Parameters:  []ParameterConfig{},
		Handler:     handleListTodos,
	})

	registry.RegisterTool(ToolConfig{
		Name:        "archive_completed",
		Description: "Archive completed/cancelled todos",
		Parameters:  []ParameterConfig{},
		Handler: func(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
			return tools.ArchiveCompleted(), nil
		},
	})

	// Register build validation tool
	registry.RegisterTool(ToolConfig{
		Name:        "validate_build",
		Description: "Validate project build after file operations",
		Parameters:  []ParameterConfig{},
		Handler:     handleValidateBuild,
	})

	// Register run_subagent tool - for multi-agent collaboration
	registry.RegisterTool(ToolConfig{
		Name:        "run_subagent",
		Description: "CRITICAL - Use this to delegate implementation tasks to subagents. Spawns an agent subprocess with a focused task, waits for completion, and returns all output. This is the PRIMARY way to implement features during execution phase. Use for: creating files, feature implementations, multi-file changes, complex logic. The subagent has full access to all tools (read, write, edit, search) and will complete the scoped task. NO TIMEOUT - runs until completion. After each subagent completes, review its output (stdout/stderr) to verify success before proceeding to the next task. If a subagent fails, you can spawn another to fix issues. Designed specifically for planning workflow: delegate, review, adjust. Subagent provider and model are configured via config settings (subagent_provider and subagent_model).",
		Parameters: []ParameterConfig{
			{"prompt", "string", true, []string{}, "The prompt/task for the subagent to execute (required)"},
			{"context", "string", false, []string{}, "Context from previous subagent work (files created, summaries, etc.)"},
			{"files", "string", false, []string{}, "Comma-separated list of relevant file paths (e.g., 'models/user.go,pkg/auth/jwt.go')"},
		},
		Handler: handleRunSubagent,
	})

	// Register run_parallel_subagents tool - for concurrent multi-agent execution
	registry.RegisterTool(ToolConfig{
		Name:        "run_parallel_subagents",
		Description: "Execute multiple subagent tasks concurrently in parallel. Useful for independent tasks that can be done simultaneously (e.g., writing production code and test cases concurrently). Waits for all tasks to complete and returns results for each task by ID. Each task has its own ID, prompt, optional model, and optional provider. Results include stdout, stderr, exit_code, completed status, and timed_out status for each task ID.",
		Parameters: []ParameterConfig{
			{"tasks", "array", true, []string{}, "Array of subagent tasks: [{id, prompt, model?, provider?}] where id is a unique identifier for the task"},
		},
		Handler: handleRunParallelSubagents,
	})

	// Register search_files tool (cross-platform file content search)
	registry.RegisterTool(ToolConfig{
		Name:        "search_files",
		Description: "Search text pattern in files (cross-platform, ignores .git, node_modules, .ledit by default)",
		Parameters: []ParameterConfig{
			{"search_pattern", "string", true, []string{"pattern"}, "Text pattern or regex to search for"},
			{"directory", "string", false, []string{"root"}, "Directory to search (default: .)"},
			{"file_glob", "string", false, []string{"file_pattern", "glob"}, "Glob to limit files (e.g., *.go)"},
			{"case_sensitive", "bool", false, []string{}, "Case sensitive search (default: false)"},
			{"max_results", "int", false, []string{}, "Maximum results to return (default: 50)"},
			{"max_bytes", "int", false, []string{}, "Maximum total bytes of matches to return (default: 20480)"},
		},
		Handler: handleSearchFiles,
	})

	// Register web_search tool
	registry.RegisterTool(ToolConfig{
		Name:        "web_search",
		Description: "Search web for relevant URLs",
		Parameters: []ParameterConfig{
			{"query", "string", true, []string{}, "Search query to find relevant web content"},
		},
		Handler: handleWebSearch,
	})

	// Register fetch_url tool
	registry.RegisterTool(ToolConfig{
		Name:        "fetch_url",
		Description: "Fetch and extract content from a URL",
		Parameters: []ParameterConfig{
			{"url", "string", true, []string{}, "URL to fetch content from"},
		},
		Handler: handleFetchURL,
	})

	// Register vision analysis tools
	registry.RegisterTool(ToolConfig{
		Name:        "analyze_ui_screenshot",
		Description: "Analyze UI screenshots or mockups for implementation guidance",
		Parameters: []ParameterConfig{
			{"image_path", "string", true, []string{}, "Path or URL to the UI screenshot"},
		},
		Handler: handleAnalyzeUIScreenshot,
	})

	registry.RegisterTool(ToolConfig{
		Name:        "analyze_image_content",
		Description: "Analyze images for text/code extraction or general insights",
		Parameters: []ParameterConfig{
			{"image_path", "string", true, []string{}, "Path or URL to the image to analyze"},
			{"analysis_prompt", "string", false, []string{}, "Optional custom vision prompt"},
			{"analysis_mode", "string", false, []string{}, "Optional analysis mode override"},
		},
		Handler: handleAnalyzeImageContent,
	})

	// Register history tools
	registry.RegisterTool(ToolConfig{
		Name:        "view_history",
		Description: "View recent change history tracked by the agent",
		Parameters: []ParameterConfig{
			{"limit", "int", false, []string{}, "Maximum number of entries to return (default 10)"},
			{"file_filter", "string", false, []string{"filename"}, "Filter by filename (partial match)"},
			{"since", "string", false, []string{}, "Only include changes after this ISO 8601 timestamp"},
			{"show_content", "bool", false, []string{}, "Include content summaries for each change"},
		},
		Handler: handleViewHistory,
	})

	registry.RegisterTool(ToolConfig{
		Name:        "rollback_changes",
		Description: "Preview or perform a rollback of tracked revisions",
		Parameters: []ParameterConfig{
			{"revision_id", "string", false, []string{}, "Revision ID to rollback (leave blank to list revisions)"},
			{"file_path", "string", false, []string{"filename"}, "Rollback only this file from the revision"},
			{"confirm", "bool", false, []string{}, "Set to true to execute the rollback"},
		},
		Handler: handleRollbackChanges,
	})

	return registry
}

// GetValidator returns the security validator (used for fast path optimization)
func (r *ToolRegistry) GetValidator() *security_validator.Validator {
	return r.validator
}

// RegisterTool adds a tool to the registry
func (r *ToolRegistry) RegisterTool(config ToolConfig) {
	r.tools[config.Name] = config
}

// ExecuteTool executes a tool with standardized parameter validation and error handling
func (r *ToolRegistry) ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}, agent *Agent) (string, error) {
	tool, exists := r.tools[toolName]
	if !exists {
		return "", fmt.Errorf("unknown tool '%s'", toolName)
	}

	// Security validation (if enabled and not bypassed)
	if agent != nil && !filesystem.SecurityBypassEnabled(ctx) {
		if validationErr := r.validateToolSecurity(ctx, toolName, args, agent); validationErr != nil {
			return "", validationErr
		}
	}

	// Validate and extract parameters
	validatedArgs, err := r.validateParameters(tool, args)
	if err != nil {
		return "", fmt.Errorf("parameter validation failed for tool '%s': %w", toolName, err)
	}

	// Execute the tool handler
	return tool.Handler(ctx, agent, validatedArgs)
}

// validateToolSecurity performs LLM-based security validation if enabled
func (r *ToolRegistry) validateToolSecurity(ctx context.Context, toolName string, args map[string]interface{}, agent *Agent) error {
	// Get config from agent
	if agent.configManager == nil {
		return nil // No config manager, skip validation
	}

	// Check for critical system operations FIRST - these are ALWAYS blocked, even in unsafe mode
	if security_validator.IsCriticalSystemOperation(toolName, args) {
		return fmt.Errorf("CRITICAL: operation blocked - this would permanently damage the system: %s", toolName)
	}

	// Unsafe mode bypasses most security checks
	if agent.GetUnsafeMode() {
		agent.debugLog("🔓 Unsafe mode enabled: skipping security validation for %s\n", toolName)
		return nil
	}

	config := agent.configManager.GetConfig()
	if config == nil || config.SecurityValidation == nil || !config.SecurityValidation.Enabled {
		return nil // Security validation disabled
	}

	// Create or reuse validator
	if r.validator == nil {
		// Get logger and interactive mode from agent
		agentConfig := agent.GetConfig()
		isNonInteractive := agentConfig != nil && agentConfig.SkipPrompt
		logger := utils.GetLogger(isNonInteractive)
		interactive := !isNonInteractive

		validator, err := security_validator.NewValidator(config.SecurityValidation, logger, interactive)
		if err != nil {
			agent.debugLog("Failed to create security validator: %v\n", err)
			return nil // Fail open - don't block operations if validator fails to init
		}
		r.validator = validator
		agent.debugLog("✓ Security validator initialized successfully\n")
	}

	// Perform validation
	result, err := r.validator.ValidateToolCall(ctx, toolName, args)
	if err != nil {
		agent.debugLog("Security validation error: %v\n", err)
		return nil // Fail open on errors
	}

	// Log the validation result
	agent.debugLog("🔒 Security validation: %s (%s) - IsSoftBlock: %v, ShouldBlock: %v, ShouldConfirm: %v\n",
		toolName, result.RiskLevel, result.IsSoftBlock, result.ShouldBlock, result.ShouldConfirm)

	// Handle blocks (user rejected in interactive mode or hard block)
	if result.ShouldBlock {
		return fmt.Errorf("operation blocked by security validation: %s (risk level: %s)\nReasoning: %s",
			toolName, result.RiskLevel, result.Reasoning)
	}

	// Handle confirmation needed (for non-interactive mode, use second LLM validation)
	if result.ShouldConfirm {
		agentConfig := agent.GetConfig()
		isNonInteractive := agentConfig != nil && agentConfig.SkipPrompt

		if isNonInteractive {
			// For non-interactive mode, make a second LLM validation call
			// Ask the LLM to confirm if this operation should proceed
			agent.PrintLine(fmt.Sprintf("⚠️  Security Block: %s (risk level: %s)", toolName, result.RiskLevel))
			agent.PrintLine(fmt.Sprintf("   Reasoning: %s", result.Reasoning))
			agent.PrintLine("   Requesting second LLM validation...")

			// Build a prompt for the second validation
			argsJSON, _ := json.Marshal(args)
			confirmPrompt := fmt.Sprintf(`The following operation was flagged as needing user confirmation:

Tool: %s
Arguments: %s
Risk Level: %s
Reasoning: %s

As an automated validation system, you need to decide if this operation should proceed.

CRITICAL: Only approve operations that are:
1. Clearly safe and necessary for the task
2. Not destructive or irreversible
3. Not accessing/modifying critical system files
4. Part of normal development workflows

HARD BLOCK these operations immediately:
- Filesystem destruction (mkfs, fdisk, dd to devices)
- Deleting system directories (rm -rf /usr, /bin, /etc, /lib, etc.)
- Modifying critical system config (/etc/shadow, /etc/passwd, sudoers)
- System-ruining commands (fork bombs, killall -9, chmod 000 /)

Respond with JSON:
{"approved": true/false, "reasoning": "brief explanation"}

Only return valid JSON, nothing else.`, toolName, string(argsJSON), result.RiskLevel, result.Reasoning)

			// Call the LLM for second validation
			secondResult, err := r.validator.CallLLMForConfirmation(ctx, confirmPrompt)
			if err != nil {
				// If second validation fails, block by default
				return fmt.Errorf("operation blocked: second LLM validation failed: %v\nOriginal reasoning: %s", err, result.Reasoning)
			}

			if !secondResult.Approved {
				return fmt.Errorf("operation blocked by second LLM validation: %s\nReasoning: %s", toolName, secondResult.Reasoning)
			}

			agent.PrintLine("   ✓ Second validation approved the operation")
		}
		// In interactive mode, ShouldConfirm should have been handled by ValidateToolCall
		// If we reach here with ShouldConfirm=true in interactive mode, it's unexpected
	}

	return nil
}

// handleFileSecurityError checks if an error is due to filesystem security and prompts the user
// Returns a context with security bypass enabled if user approves, original context otherwise
func handleFileSecurityError(ctx context.Context, agent *Agent, toolName, filePath string, err error) context.Context {
	// Check if this is a filesystem security error
	if errors.Is(err, filesystem.ErrOutsideWorkingDirectory) || errors.Is(err, filesystem.ErrWriteOutsideWorkingDirectory) {
		// Unsafe mode bypasses filesystem security checks automatically
		if agent.GetUnsafeMode() {
			agent.debugLog("🔓 Unsafe mode: automatically allowing file access outside working directory: %s\n", filePath)
			return filesystem.WithSecurityBypass(ctx)
		}

		// Prompt user for confirmation
		agentConfig := agent.GetConfig()
		logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)

		prompt := fmt.Sprintf("⚠️  Filesystem Security Warning\n\nThe tool '%s' is attempting to access a file outside the working directory:\n  %s\n\nDo you want to allow this? (yes/no): ", toolName, filePath)

		if logger.AskForConfirmation(prompt, false, false) {
			// User approved - enable security bypass for this operation
			agent.debugLog("User approved file access outside working directory: %s\n", filePath)
			return filesystem.WithSecurityBypass(ctx)
		} else {
			// User rejected - error will be returned as-is
			agent.debugLog("User rejected file access outside working directory: %s\n", filePath)
		}
	}
	return ctx
}

// validateParameters validates and extracts parameters according to tool configuration
func (r *ToolRegistry) validateParameters(tool ToolConfig, args map[string]interface{}) (map[string]interface{}, error) {
	validated := make(map[string]interface{})

	for _, param := range tool.Parameters {
		value, found := r.extractParameter(param, args)

		if !found && param.Required {
			return nil, fmt.Errorf("required parameter '%s' missing", param.Name)
		}

		if found {
			// Type validation and conversion
			convertedValue, err := r.convertParameterType(value, param.Type)
			if err != nil {
				return nil, fmt.Errorf("parameter '%s': %w", param.Name, err)
			}
			validated[param.Name] = convertedValue
		}
	}

	return validated, nil
}

// extractParameter extracts a parameter value, checking alternatives for backward compatibility
func (r *ToolRegistry) extractParameter(param ParameterConfig, args map[string]interface{}) (interface{}, bool) {
	// Try primary name first
	if value, exists := args[param.Name]; exists {
		return value, true
	}

	// Try alternative names for backward compatibility
	for _, alt := range param.Alternatives {
		if value, exists := args[alt]; exists {
			return value, true
		}
	}

	return nil, false
}

// getMapKeys returns all keys from a map as a slice
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// mapToJSONString converts a map to a pretty-printed JSON string
func (r *ToolRegistry) mapToJSONString(m map[string]interface{}) (string, error) {
	jsonBytes, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal map to JSON: %w", err)
	}
	return string(jsonBytes), nil
}

// convertParameterType converts a parameter to the expected type
func (r *ToolRegistry) convertParameterType(value interface{}, expectedType string) (interface{}, error) {
	switch expectedType {
	case "string":
		if str, ok := value.(string); ok {
			return str, nil
		}

		// Handle case where content is passed as a map instead of string
		if mapVal, ok := value.(map[string]interface{}); ok {
			// Try to convert the map to JSON string
			jsonStr, err := r.mapToJSONString(mapVal)
			if err != nil {
				fmt.Printf("🚨 DEBUG: Expected string, got map[string]interface {}. Failed to convert to JSON: %v\n", err)
				fmt.Printf("🚨 DEBUG: Content as map keys: %v\n", getMapKeys(mapVal))
				return "", fmt.Errorf("expected string, got %T (failed to convert map to JSON: %w)", value, err)
			}

			fmt.Printf("🚨 DEBUG: Converted map to JSON string. Length: %d\n", len(jsonStr))
			return jsonStr, nil
		}

		// Debug logging for other type conversion failures
		fmt.Printf("🚨 DEBUG: Expected string, got %T. Value: %+v\n", value, value)

		return "", fmt.Errorf("expected string, got %T", value)

	case "int":
		if i, ok := value.(int); ok {
			return i, nil
		}
		if f, ok := value.(float64); ok {
			return int(f), nil
		}
		return 0, fmt.Errorf("expected int, got %T", value)

	case "float64":
		if f, ok := value.(float64); ok {
			return f, nil
		}
		if i, ok := value.(int); ok {
			return float64(i), nil
		}
		return 0.0, fmt.Errorf("expected float64, got %T", value)

	case "bool":
		if b, ok := value.(bool); ok {
			return b, nil
		}
		return false, fmt.Errorf("expected bool, got %T", value)

	default:
		return value, nil // No conversion needed for unknown types
	}
}

// GetAvailableTools returns a list of all registered tool names
func (r *ToolRegistry) GetAvailableTools() []string {
	tools := make([]string, 0, len(r.tools))
	for toolName := range r.tools {
		tools = append(tools, toolName)
	}
	return tools
}

// Tool handler implementations

func handleShellCommand(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	command := args["command"].(string)
	a.ToolLog("executing command", command)
	return a.executeShellCommandWithTruncation(ctx, command)
}

func handleReadFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Convert arguments to proper types with type checking
	filePath, err := convertToString(args["file_path"], "file_path")
	if err != nil {
		return "", err
	}

	// Check for optional line range parameters
	var startLine int
	var hasStart bool
	if startLineParam, exists := args["start_line"]; exists {
		switch v := startLineParam.(type) {
		case int:
			startLine = v
			hasStart = true
		case float64: // JSON numbers are often unmarshaled as float64
			startLine = int(v)
			hasStart = true
		default:
			return "", fmt.Errorf("parameter 'start_line' has invalid type %T, expected integer", startLineParam)
		}
	}

	var endLine int
	var hasEnd bool
	if endLineParam, exists := args["end_line"]; exists {
		switch v := endLineParam.(type) {
		case int:
			endLine = v
			hasEnd = true
		case float64: // JSON numbers are often unmarshaled as float64
			endLine = int(v)
			hasEnd = true
		default:
			return "", fmt.Errorf("parameter 'end_line' has invalid type %T, expected integer", endLineParam)
		}
	}

	// Log the operation
	if hasStart || hasEnd {
		a.ToolLog("reading file", fmt.Sprintf("%s (lines %d-%d)", filePath, startLine, endLine))
		a.debugLog("Reading file: %s (lines %d-%d)\n", filePath, startLine, endLine)
		result, err := tools.ReadFileWithRange(ctx, filePath, startLine, endLine)

			// Handle filesystem security errors - prompt user for confirmation
		if err != nil {
			ctx2 := handleFileSecurityError(ctx, a, "read_file", filePath, err)
			if ctx2 != ctx {
				// User approved, retry with bypass
				result, err = tools.ReadFileWithRange(ctx2, filePath, startLine, endLine)
			}
		}

		a.debugLog("Read file result: %s, error: %v\n", result, err)

		// Record as a task action for conversation summary
		if err == nil {
			a.AddTaskAction("file_read", fmt.Sprintf("Read file: %s (lines %d-%d)", filePath, startLine, endLine), filePath)
		}

		return result, err
	} else {
		a.ToolLog("reading file", filePath)
		a.debugLog("Reading file: %s\n", filePath)
		result, err := tools.ReadFile(ctx, filePath)

		// Handle filesystem security errors - prompt user for confirmation
		if err != nil {
			ctx2 := handleFileSecurityError(ctx, a, "read_file", filePath, err)
			if ctx2 != ctx {
				// User approved, retry with bypass
				result, err = tools.ReadFile(ctx2, filePath)
			}
		}

		a.debugLog("Read file result: %s, error: %v\n", result, err)

		// Record as a task action for conversation summary
		if err == nil {
			a.AddTaskAction("file_read", fmt.Sprintf("Read file: %s", filePath), filePath)
		}

		return result, err
	}
}

// convertToString safely converts a parameter to string with proper error handling
func convertToString(param interface{}, paramName string) (string, error) {
	switch v := param.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case int, int32, int64, float32, float64:
		return fmt.Sprintf("%v", v), nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	case map[string]interface{}:
		// If it's a map, try to convert to JSON string
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("parameter '%s' is an object that cannot be converted to string: %w", paramName, err)
		}
		return string(jsonBytes), nil
	case nil:
		return "", fmt.Errorf("parameter '%s' is missing or null", paramName)
	default:
		return "", fmt.Errorf("parameter '%s' has invalid type %T, expected string", paramName, param)
	}
}

func handleWriteFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Convert arguments to strings with proper type checking
	filePath, err := convertToString(args["file_path"], "file_path")
	if err != nil {
		return "", err
	}

	content, err := convertToString(args["content"], "content")
	if err != nil {
		return "", err
	}

	a.ToolLog("writing file", fmt.Sprintf("%s (%d bytes)", filePath, len(content)))
	a.debugLog("Writing file: %s\n", filePath)

	// Track the file write for change tracking
	if trackErr := a.TrackFileWrite(filePath, content); trackErr != nil {
		a.debugLog("Warning: Failed to track file write: %v\n", trackErr)
	}

	result, err := tools.WriteFile(ctx, filePath, content)

	// Handle filesystem security errors - prompt user for confirmation
	if err != nil {
		ctx2 := handleFileSecurityError(ctx, a, "write_file", filePath, err)
		if ctx2 != ctx {
			// User approved, retry with bypass
			result, err = tools.WriteFile(ctx2, filePath, content)
		}
	}

	a.debugLog("Write file result: %s, error: %v\n", result, err)

	// Publish file change event for web UI auto-sync
	if err == nil && a.eventBus != nil {
		a.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(filePath, "write", content))
		a.debugLog("Published file_changed event: %s (write)\n", filePath)
	}

	return result, err
}

func handleEditFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Convert arguments to strings with proper type checking
	filePath, err := convertToString(args["file_path"], "file_path")
	if err != nil {
		return "", err
	}

	oldString, err := convertToString(args["old_string"], "old_string")
	if err != nil {
		return "", err
	}

	newString, err := convertToString(args["new_string"], "new_string")
	if err != nil {
		return "", err
	}

	// Read the original content for diff display
	originalContent, err := tools.ReadFile(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read original file for diff: %w", err)
	}

	a.ToolLog("editing file", fmt.Sprintf("%s (replacing %d bytes → %d bytes)", filePath, len(oldString), len(newString)))
	a.debugLog("Editing file: %s\n", filePath)
	a.debugLog("Old string: %s\n", oldString)
	a.debugLog("New string: %s\n", newString)

	// Track the file edit for change tracking
	if trackErr := a.TrackFileEdit(filePath, oldString, newString); trackErr != nil {
		a.debugLog("Warning: Failed to track file edit: %v\n", trackErr)
	}

	result, err := tools.EditFile(ctx, filePath, oldString, newString)

	// Handle filesystem security errors - prompt user for confirmation
	if err != nil {
		ctx2 := handleFileSecurityError(ctx, a, "edit_file", filePath, err)
		if ctx2 != ctx {
			// User approved, retry with bypass
			// Re-read original content with bypass
			originalContent, err = tools.ReadFile(ctx2, filePath)
			if err != nil {
				return "", fmt.Errorf("failed to read original file for diff: %w", err)
			}
			result, err = tools.EditFile(ctx2, filePath, oldString, newString)
		}
	}

	a.debugLog("Edit file result: %s, error: %v\n", result, err)

	// Publish file change event for web UI auto-sync
	if err == nil && a.eventBus != nil {
		// Read new content to include in event
		var eventContent string
		if eventContent, err = tools.ReadFile(ctx, filePath); err == nil {
			a.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(filePath, "edit", eventContent))
			a.debugLog("Published file_changed event: %s (edit)\n", filePath)
		} else {
			// Still publish event even if we can't read file (just with empty content)
			a.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(filePath, "edit", ""))
			a.debugLog("Published file_changed event: %s (edit, no content)\n", filePath)
		}
	}

	// Display diff if successful
	if err == nil {
		newContent, readErr := tools.ReadFile(ctx, filePath)
		if readErr == nil {
			a.ShowColoredDiff(originalContent, newContent, 50)
		}
	}

	return result, err
}

func handleAddTodo(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	task := args["task"].(string)
	a.ToolLog("adding todo", fmt.Sprintf("task=%q", truncateString(task, 40)))
	a.debugLog("Adding todo: %s\n", task)

	result := tools.AddTodo(task, "", "medium") // title, description, priority
	a.debugLog("Add todo result: %s\n", result)
	return result, nil
}

func handleUpdateTodoStatus(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Normalize ID using shared utility function
	taskID := tools.NormalizeTodoID(args["id"])
	if taskID == "" {
		return "", fmt.Errorf("invalid or missing id argument")
	}

	status, ok := args["status"].(string)
	if !ok {
		return "", fmt.Errorf("invalid status argument")
	}

	// Validate status using shared utility function
	if !tools.IsValidStatus(status) {
		return "", fmt.Errorf("%s", tools.FormatTodoStatusError(status))
	}

	a.ToolLog("updating todo", fmt.Sprintf("id=%s status=%s", taskID, status))
	a.debugLog("Updating todo %s to status: %s\n", taskID, status)

	result := tools.UpdateTodoStatus(taskID, status)
	if result == "Todo not found" && !strings.HasPrefix(taskID, "todo_") {
		if resolved, ok := tools.FindTodoIDByTitle(taskID); ok {
			a.debugLog("Resolved todo title '%s' to id %s\n", taskID, resolved)
			taskID = resolved
			result = tools.UpdateTodoStatus(taskID, status)
		}
	}
	a.debugLog("Update todo result: %s\n", result)
	return result, nil
}

func handleUpdateTodoStatusBulk(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	updatesRaw, ok := args["updates"]
	if !ok {
		return "", fmt.Errorf("missing updates argument")
	}

	// Parse the updates array
	updatesSlice, ok := updatesRaw.([]interface{})
	if !ok {
		return "", fmt.Errorf("updates must be an array")
	}

	var updates []struct {
		ID     string
		Status string
	}

	for _, updateRaw := range updatesSlice {
		updateMap, ok := updateRaw.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("each update must be an object")
		}

		update := struct {
			ID     string
			Status string
		}{}

		// Normalize ID using shared utility function
		update.ID = tools.NormalizeTodoID(updateMap["id"])

		if status, ok := updateMap["status"].(string); ok {
			// Validate status using shared utility function
			if !tools.IsValidStatus(status) {
				return "", fmt.Errorf("%s", tools.FormatTodoStatusError(status))
			}
			update.Status = status
		}

		if update.ID == "" {
			return "", fmt.Errorf("each update requires an id")
		}
		if update.Status == "" {
			return "", fmt.Errorf("each update requires a status")
		}
		updates = append(updates, update)
	}

	a.ToolLog("bulk updating todos", fmt.Sprintf("%d items", len(updates)))
	a.debugLog("Bulk updating %d todos\n", len(updates))

	result := tools.UpdateTodoStatusBulk(updates)
	a.debugLog("Bulk update result: %s\n", result)
	return result, nil
}

func handleAddTodos(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	todosRaw, ok := args["todos"]
	if !ok {
		return "", fmt.Errorf("missing todos argument")
	}

	// Count todos for logging
	todoCount := 0
	if todosSlice, ok := todosRaw.([]interface{}); ok {
		todoCount = len(todosSlice)
	}
	a.ToolLog("adding todos", fmt.Sprintf("%d items", todoCount))

	// Parse the todos array
	todosSlice, ok := todosRaw.([]interface{})
	if !ok {
		return "", fmt.Errorf("todos must be an array")
	}

	var todos []struct {
		Title       string
		Description string
		Priority    string
	}

	for _, todoRaw := range todosSlice {
		todoMap, ok := todoRaw.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("each todo must be an object")
		}

		todo := struct {
			Title       string
			Description string
			Priority    string
		}{}

		if title, ok := todoMap["title"].(string); ok {
			todo.Title = title
		}
		if desc, ok := todoMap["description"].(string); ok {
			todo.Description = desc
		}
		if prio, ok := todoMap["priority"].(string); ok {
			todo.Priority = prio
		} else {
			todo.Priority = "medium"
		}

		if todo.Title == "" {
			return "", fmt.Errorf("each todo requires a title")
		}
		todos = append(todos, todo)
	}

	a.debugLog("Adding %d todos\n", len(todos))
	result := tools.AddBulkTodos(todos)
	a.debugLog("Add todos result: %s\n", result)
	return result, nil
}

func handleListTodos(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	a.ToolLog("listing todos", "")
	a.debugLog("Listing todos\n")

	result := tools.ListTodos()
	a.debugLog("List todos result: %s\n", result)
	return result, nil
}

// extractSubagentSummary parses stdout from a subagent execution to extract key information
func extractSubagentSummary(stdout string) map[string]string {
	summary := make(map[string]string)
	lines := strings.Split(stdout, "\n")

	var fileChanges []string
	var buildStatus string
	var testStatus string
	var errors []string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Extract file operations
		switch {
		case strings.HasPrefix(line, "Created:") || strings.HasPrefix(line, "Wrote"):
			file := strings.TrimSpace(strings.TrimPrefix(line, "Created:"))
			file = strings.TrimSpace(strings.TrimPrefix(file, "Wrote"))
			fileChanges = append(fileChanges, "Created: "+file)
		case strings.HasPrefix(line, "Modified:"):
			file := strings.TrimSpace(strings.TrimPrefix(line, "Modified:"))
			fileChanges = append(fileChanges, "Modified: "+file)
		case strings.HasPrefix(line, "Deleted:"):
			file := strings.TrimSpace(strings.TrimPrefix(line, "Deleted:"))
			fileChanges = append(fileChanges, "Deleted: "+file)
		case strings.HasPrefix(line, "Updated:"):
			file := strings.TrimSpace(strings.TrimPrefix(line, "Updated:"))
			fileChanges = append(fileChanges, "Updated: "+file)
		}

		// Extract build status
		if strings.Contains(line, "Build:") {
			if strings.Contains(line, "✅ Passed") {
				buildStatus = "passed"
			} else if strings.Contains(line, "✅ Failed") || strings.Contains(line, "❌ Failed") {
				buildStatus = "failed"
			}
		}

		// Extract test status
		if strings.Contains(line, "Test:") || strings.Contains(line, "Tests:") {
			if strings.Contains(line, "✅ Passed") {
				testStatus = "passed"
			} else if strings.Contains(line, "✅ Failed") || strings.Contains(line, "❌ Failed") {
				testStatus = "failed"
			}
		}

		// Extract errors
		if strings.HasPrefix(line, "Error:") || strings.HasPrefix(line, "error:") {
			errors = append(errors, line)
		}
	}

	if len(fileChanges) > 0 {
		summary["files"] = strings.Join(fileChanges, "; ")
	}
	if buildStatus != "" {
		summary["build_status"] = buildStatus
	}
	if testStatus != "" {
		summary["test_status"] = testStatus
	}
	if len(errors) > 0 {
		summary["errors"] = strings.Join(errors, "; ")
	}

	return summary
}

func handleRunSubagent(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	prompt, err := convertToString(args["prompt"], "prompt")
	if err != nil {
		return "", err
	}

	// Parse optional context parameter
	var context string
	if ctxVal, ok := args["context"]; ok && ctxVal != nil {
		if ctxStr, ok := ctxVal.(string); ok && ctxStr != "" {
			context = ctxStr
			a.debugLog("Subagent context provided: %s\n", truncateString(context, 100))
		}
	}

	// Parse optional files parameter (comma-separated list)
	var files []string
	var filesStr string
	if filesVal, ok := args["files"]; ok && filesVal != nil {
		if filesRaw, ok := filesVal.(string); ok && filesRaw != "" {
			// Split by comma and trim spaces
			rawFiles := strings.Split(filesRaw, ",")
			for _, f := range rawFiles {
				if f = strings.TrimSpace(f); f != "" {
					files = append(files, f)
				}
			}
			filesStr = strings.Join(files, ",")
			a.debugLog("Subagent files provided: %s\n", filesStr)

			// Validate each file path before proceeding
			for _, filePath := range files {
				// Clean the path to eliminate any . or redundant separators
				cleanedPath := filepath.Clean(filePath)
				absPath, err := filepath.Abs(cleanedPath)
				if err != nil {
					return "", fmt.Errorf("failed to resolve absolute path for %s: %w", filePath, err)
				}

				// Get workspace directory (current working directory)
				workspaceDir, err := os.Getwd()
				if err != nil {
					return "", fmt.Errorf("failed to get workspace directory: %w", err)
				}
				absWorkspaceDir, err := filepath.Abs(workspaceDir)
				if err != nil {
					return "", fmt.Errorf("failed to resolve absolute workspace path: %w", err)
				}

				// Verify the file is within the workspace
				if !strings.HasPrefix(absPath, absWorkspaceDir+string(filepath.Separator)) && absPath != absWorkspaceDir {
					return "", fmt.Errorf("file path is outside workspace: %s (workspace: %s)", filePath, absWorkspaceDir)
				}

				// Verify the file exists (missing is OK - subagent can create it)
				if _, err := os.Stat(absPath); err != nil && !os.IsNotExist(err) {
					return "", fmt.Errorf("failed to access file %s: %w", filePath, err)
				}

				a.debugLog("Validated file path: %s -> %s\n", filePath, absPath)
			}
		}
	}

	// Build enhanced prompt with context and files
	enhancedPrompt := new(strings.Builder)

	// Add previous work context section if provided
	if context != "" {
		enhancedPrompt.WriteString("# Previous Work Context\n\n")
		enhancedPrompt.WriteString(context)
		enhancedPrompt.WriteString("\n\n---\n\n")
	}

	// Add relevant files section if provided
	if len(files) > 0 {
		enhancedPrompt.WriteString("# Relevant Files\n\n")
		for _, filePath := range files {
			enhancedPrompt.WriteString(fmt.Sprintf("## File: %s\n\n", filePath))
			content, err := tools.ReadFile(ctx, filePath)
			if err != nil {
				enhancedPrompt.WriteString(fmt.Sprintf("[Error reading file: %v]\n\n", err))
				a.debugLog("Failed to read file %s for subagent context: %v\n", filePath, err)
			} else {
				enhancedPrompt.WriteString(content)
				enhancedPrompt.WriteString("\n\n")
			}
		}
		enhancedPrompt.WriteString("---\n\n")
	}

	// Add task section
	enhancedPrompt.WriteString("# Your Task\n\n")
	enhancedPrompt.WriteString(prompt)

	a.debugLog("Spawning subagent with enhanced prompt (length: %d)\n", enhancedPrompt.Len())

	// Validate enhanced prompt size
	if enhancedPrompt.Len() > MAX_SUBAGENT_CONTEXT_SIZE {
		return "", fmt.Errorf("enhanced prompt exceeds maximum size of %d bytes", MAX_SUBAGENT_CONTEXT_SIZE)
	}

	// Get subagent provider and model from configuration
	var provider string
	var model string

	if a.configManager != nil {
		config := a.configManager.GetConfig()
		provider = config.GetSubagentProvider()
		model = config.GetSubagentModel()
		a.debugLog("Using subagent provider=%s model=%s from config\n", provider, model)
	} else {
		a.debugLog("Warning: No config manager available, using defaults\n")
		provider = "" // Will use system default
		model = ""    // Will use system default
	}

	resultMap, err := tools.RunSubagent(enhancedPrompt.String(), model, provider)
	if err != nil {
		a.debugLog("Subagent spawn error: %v\n", err)
		return "", err
	}

	// Truncate output if it exceeds size limit
	if stdout, ok := resultMap["stdout"]; ok {
		if len(stdout) > MAX_SUBAGENT_OUTPUT_SIZE {
			resultMap["stdout"] = stdout[:MAX_SUBAGENT_OUTPUT_SIZE] + "... (truncated, too large)"
		}
	}
	if stderr, ok := resultMap["stderr"]; ok {
		if len(stderr) > MAX_SUBAGENT_OUTPUT_SIZE {
			resultMap["stderr"] = stderr[:MAX_SUBAGENT_OUTPUT_SIZE] + "... (truncated, too large)"
		}
	}

	// Extract summary from stdout
	if stdout, ok := resultMap["stdout"]; ok {
		summary := extractSubagentSummary(stdout)
		summaryJSON, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			a.debugLog("Failed to marshal summary: %v\n", err)
			resultMap["summary"] = fmt.Sprintf("Error creating summary: %v", err)
		} else {
			resultMap["summary"] = string(summaryJSON)
			a.debugLog("Extracted subagent summary: %s\n", string(summaryJSON))
		}
	}

	// Add context_used field
	if context != "" {
		resultMap["context_used"] = "true"
	} else {
		resultMap["context_used"] = "false"
	}

	// Add files_used field
	if filesStr != "" {
		resultMap["files_used"] = filesStr
	} else {
		resultMap["files_used"] = ""
	}

	// Convert map result to JSON for return
	jsonBytes, jsonErr := json.MarshalIndent(resultMap, "", "  ")
	if jsonErr != nil {
		return "", fmt.Errorf("failed to marshal subagent result: %w", jsonErr)
	}

	a.debugLog("Subagent spawn result: %s\n", string(jsonBytes))
	return string(jsonBytes), nil
}

func handleRunParallelSubagents(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	tasksRaw, ok := args["tasks"]
	if !ok {
		return "", fmt.Errorf("missing tasks argument")
	}

	// Parse the tasks array
	tasksSlice, ok := tasksRaw.([]interface{})
	if !ok {
		return "", fmt.Errorf("tasks must be an array")
	}

	var parallelTasks []tools.ParallelSubagentTask
	for _, taskRaw := range tasksSlice {
		taskMap, ok := taskRaw.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("each task must be an object")
		}

		task := tools.ParallelSubagentTask{}

		if id, ok := taskMap["id"].(string); ok {
			task.ID = id
		}

		prompt, err := convertToString(taskMap["prompt"], "prompt")
		if err != nil {
			return "", err
		}
		task.Prompt = prompt

		if model, ok := taskMap["model"].(string); ok {
			task.Model = model
		}

		if provider, ok := taskMap["provider"].(string); ok {
			task.Provider = provider
		}

		if task.ID == "" {
			return "", fmt.Errorf("each task requires an id")
		}

		parallelTasks = append(parallelTasks, task)
	}

	// Validate number of parallel tasks
	if len(parallelTasks) > MAX_PARALLEL_SUBAGENTS {
		return "", fmt.Errorf("too many parallel tasks: %d exceeds max of %d", len(parallelTasks), MAX_PARALLEL_SUBAGENTS)
	}

	a.debugLog("Spawning %d parallel subagents\n", len(parallelTasks))

	resultMap, err := tools.RunParallelSubagents(parallelTasks, false)
	if err != nil {
		a.debugLog("Parallel subagents spawn error: %v\n", err)
		return "", err
	}

	// Convert map result to JSON for return
	jsonBytes, jsonErr := json.MarshalIndent(resultMap, "", "  ")
	if jsonErr != nil {
		return "", fmt.Errorf("failed to marshal parallel subagents result: %w", jsonErr)
	}

	a.debugLog("Parallel subagents spawn result: %s\n", string(jsonBytes))
	return string(jsonBytes), nil
}

// Helper function for string truncation
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func handleValidateBuild(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	a.debugLog("Running build validation\n")

	result, err := tools.ValidateBuild()
	a.debugLog("Build validation result: %s, error: %v\n", result, err)
	return result, err
}

// handleSearchFiles implements a cross-platform content search with sensible defaults and ignores
const (
	defaultSearchMaxResults = 50
	defaultSearchMaxBytes   = 20 * 1024
	defaultSearchLineLength = 240
)

func normalizePositiveInt(value any) int {
	const maxInt = int(^uint(0) >> 1)
	switch v := value.(type) {
	case int:
		if v > 0 {
			return v
		}
	case int8:
		if v > 0 {
			return int(v)
		}
	case int16:
		if v > 0 {
			return int(v)
		}
	case int32:
		if v > 0 {
			return int(v)
		}
	case int64:
		if v > 0 && v <= int64(maxInt) {
			return int(v)
		}
	case uint:
		if v64 := uint64(v); v64 > 0 && v64 <= uint64(maxInt) {
			return int(v)
		}
	case uint8:
		if v > 0 {
			return int(v)
		}
	case uint16:
		if v > 0 {
			return int(v)
		}
	case uint32:
		if v64 := uint64(v); v64 > 0 && v64 <= uint64(maxInt) {
			return int(v)
		}
	case uint64:
		if v > 0 && v <= uint64(maxInt) {
			return int(v)
		}
	case float32:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return normalizePositiveInt(i)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return normalizePositiveInt(i)
		}
	}
	return 0
}

func handleSearchFiles(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	var pattern string
	if p, ok := args["search_pattern"].(string); ok {
		pattern = p
	} else if p, ok := args["pattern"].(string); ok {
		pattern = p
	} else {
		return "", fmt.Errorf("missing required parameter 'search_pattern'")
	}

	root := "."
	if v, ok := args["directory"].(string); ok && strings.TrimSpace(v) != "" {
		root = v
	}

	glob := ""
	if v, ok := args["file_glob"].(string); ok {
		glob = v
	} else if v, ok := args["file_pattern"].(string); ok {
		glob = v
	}

	caseSensitive := false
	if v, ok := args["case_sensitive"].(bool); ok {
		caseSensitive = v
	}

	maxResults := defaultSearchMaxResults
	if v, ok := args["max_results"]; ok {
		if normalized := normalizePositiveInt(v); normalized > 0 {
			maxResults = normalized
		}
	}

	maxBytes := defaultSearchMaxBytes
	if v, ok := args["max_bytes"]; ok {
		if normalized := normalizePositiveInt(v); normalized > 0 {
			maxBytes = normalized
		}
	}

	a.ToolLog("searching files", fmt.Sprintf("pattern=%q in %s", pattern, root))
	a.debugLog("Searching files: pattern=%q, root=%s, max_results=%d\n", pattern, root, maxResults)

	// Prepare matcher: try regex first, then fallback to substring
	var re *regexp.Regexp
	var err error
	if caseSensitive {
		re, err = regexp.Compile(pattern)
	} else {
		re, err = regexp.Compile("(?i)" + pattern)
	}
	useRegex := err == nil

	// Default excluded directories
	excluded := map[string]bool{
		".git":         true,
		"node_modules": true,
		".ledit":       true,
		".venv":        true,
		"dist":         true,
		"build":        true,
		".cache":       true,
	}

	matched := 0
	var b strings.Builder
	searchCapped := false

	// Limit per-file read to avoid huge files (in bytes)
	const maxFileSize = 2 * 1024 * 1024 // 2MB

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if searchCapped {
			return io.EOF
		}
		if err != nil {
			return nil // skip on error
		}
		name := d.Name()
		if d.IsDir() {
			if excluded[name] {
				return filepath.SkipDir
			}
			// Skip hidden dirs unless explicitly included via pattern/glob (keep simple)
			if strings.HasPrefix(name, ".") && !strings.HasPrefix(name, ".env") {
				if name != "." && name != ".." {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Glob filter
		if glob != "" {
			// Use base name for typical patterns
			if ok, _ := filepath.Match(glob, name); !ok {
				return nil
			}
		}

		// Basic binary guard by extension
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tiff", ".webp",
			".pdf", ".zip", ".tar", ".gz", ".rar", ".7z",
			".mp3", ".wav", ".ogg", ".flac", ".aac",
			".mp4", ".avi", ".mov", ".wmv", ".mkv",
			".exe", ".dll", ".so", ".dylib", ".bin",
			".db", ".sqlite", ".ico", ".woff", ".woff2", ".ttf":
			return nil
		}

		// Open file and scan
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		// Size cap
		if info, err := f.Stat(); err == nil && info.Size() > maxFileSize {
			// Read only first maxFileSize bytes
			r := io.LimitReader(f, maxFileSize)
			buf := make([]byte, maxFileSize)
			n, _ := io.ReadFull(r, buf)
			buf = buf[:n]
			// naive binary check: look for NUL
			if bytesIndexByte(buf, 0) >= 0 {
				return nil
			}
			// search within this chunk by lines
			if searchBufferLines(&b, path, string(buf), re, pattern, caseSensitive, useRegex, &matched, maxResults, maxBytes) {
				searchCapped = true
				return io.EOF // stop walking by returning non-nil? better: track and stop later
			}
			return nil
		}

		content, err := io.ReadAll(f)
		if err != nil {
			return nil
		}
		// binary check
		if bytesIndexByte(content, 0) >= 0 {
			return nil
		}
		if searchBufferLines(&b, path, string(content), re, pattern, caseSensitive, useRegex, &matched, maxResults, maxBytes) {
			searchCapped = true
			return io.EOF
		}
		return nil
	})

	if walkErr != nil && walkErr != io.EOF {
		return "", fmt.Errorf("search failed: %v", walkErr)
	}

	if matched == 0 {
		return fmt.Sprintf("No matches found for pattern '%s' in %s", pattern, root), nil
	}
	return b.String(), nil
}

func handleWebSearch(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent context is required for web_search tool")
	}

	query := args["query"].(string)
	a.ToolLog("searching web", fmt.Sprintf("query=%q", truncateString(query, 50)))
	a.debugLog("Performing web search: %s\n", query)

	if a.configManager == nil {
		return "", fmt.Errorf("configuration manager not initialized for web search")
	}

	result, err := tools.WebSearch(query, a.configManager)
	a.debugLog("Web search error: %v\n", err)
	return result, err
}

func handleFetchURL(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent context is required for fetch_url tool")
	}

	url := args["url"].(string)
	a.ToolLog("fetching URL", fmt.Sprintf("url=%q", truncateString(url, 50)))
	a.debugLog("Fetching URL: %s\n", url)

	if a.configManager == nil {
		return "", fmt.Errorf("configuration manager not initialized for URL fetch")
	}

	result, err := tools.FetchURL(url, a.configManager)
	a.debugLog("Fetch URL error: %v\n", err)
	return result, err
}

func handleAnalyzeUIScreenshot(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent context is required for analyze_ui_screenshot tool")
	}

	imagePath := args["image_path"].(string)
	a.debugLog("Analyzing UI screenshot: %s\n", imagePath)

	result, err := tools.AnalyzeImage(imagePath, "", "frontend")
	a.debugLog("Analyze UI screenshot error: %v\n", err)
	return result, err
}

func handleAnalyzeImageContent(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent context is required for analyze_image_content tool")
	}

	imagePath := args["image_path"].(string)
	analysisPrompt := ""
	if v, ok := args["analysis_prompt"].(string); ok {
		analysisPrompt = v
	}
	analysisMode := "general"
	if v, ok := args["analysis_mode"].(string); ok && strings.TrimSpace(v) != "" {
		analysisMode = v
	}

	a.debugLog("Analyzing image: %s (mode=%s)\n", imagePath, analysisMode)

	result, err := tools.AnalyzeImage(imagePath, analysisPrompt, analysisMode)
	a.debugLog("Analyze image content error: %v\n", err)
	return result, err
}

func handleViewHistory(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	limit := 10
	if v, ok := args["limit"].(int); ok {
		limit = v
	} else if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}

	fileFilter := ""
	if v, ok := args["file_filter"].(string); ok {
		fileFilter = strings.TrimSpace(v)
	}

	var sincePtr *time.Time
	sinceDisplay := ""
	if raw, ok := args["since"].(string); ok && strings.TrimSpace(raw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
		if err != nil {
			return "", fmt.Errorf("invalid time format for 'since': %s. Use ISO 8601 format like '2024-01-01T10:00:00Z'", raw)
		}
		sincePtr = &parsed
		sinceDisplay = parsed.Format(time.RFC3339)
	}

	showContent := false
	if v, ok := args["show_content"].(bool); ok {
		showContent = v
	}

	logParts := []string{fmt.Sprintf("limit=%d", limit)}
	if fileFilter != "" {
		logParts = append(logParts, fmt.Sprintf("file~%s", fileFilter))
	}
	if sincePtr != nil {
		logParts = append(logParts, fmt.Sprintf("since=%s", sinceDisplay))
	}
	if showContent {
		logParts = append(logParts, "with_content")
	}

	a.debugLog("Executing view_history with limit=%d, file_filter=%q, since=%s, show_content=%v\n", limit, fileFilter, sinceDisplay, showContent)

	res, err := tools.ViewHistory(limit, fileFilter, sincePtr, showContent)
	if err != nil {
		return "", err
	}

	a.debugLog("view_history metadata: %+v\n", res.Metadata)
	return res.Output, nil
}

func handleRollbackChanges(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	revisionID := ""
	if v, ok := args["revision_id"].(string); ok {
		revisionID = strings.TrimSpace(v)
	}

	filePath := ""
	if v, ok := args["file_path"].(string); ok {
		filePath = strings.TrimSpace(v)
	}

	confirm := false
	if v, ok := args["confirm"].(bool); ok {
		confirm = v
	}

	a.debugLog("Executing rollback_changes with revision_id=%q, file_path=%q, confirm=%v\n", revisionID, filePath, confirm)

	res, err := tools.RollbackChanges(revisionID, filePath, confirm)
	if err != nil {
		return "", err
	}

	a.debugLog("rollback_changes success=%v metadata=%+v\n", res.Success, res.Metadata)
	return res.Output, nil
}

// bytesIndexByte is a small helper to avoid importing bytes for one call
func bytesIndexByte(b []byte, c byte) int {
	for i := 0; i < len(b); i++ {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// searchBufferLines scans lines of content and appends matches; returns true if max reached
func searchBufferLines(b *strings.Builder, path, content string, re *regexp.Regexp, pattern string, caseSensitive, useRegex bool, matched *int, max int, maxBytes int) bool {
	// Normalize to forward slashes for readability
	norm := filepath.ToSlash(path)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if maxBytes > 0 && b.Len() >= maxBytes {
			return true
		}
		if *matched >= max {
			return true
		}
		ok := false
		if useRegex {
			ok = re.FindStringIndex(line) != nil
		} else {
			if caseSensitive {
				ok = strings.Contains(line, pattern)
			} else {
				ok = strings.Contains(strings.ToLower(line), strings.ToLower(pattern))
			}
		}
		if ok {
			lineOut := line
			if defaultSearchLineLength > 0 && len(lineOut) > defaultSearchLineLength {
				lineOut = truncateString(lineOut, defaultSearchLineLength)
			}
			// Format similar to grep: path:line:content
			fmt.Fprintf(b, "%s:%d:%s\n", norm, i+1, lineOut)
			*matched++
			if maxBytes > 0 && b.Len() >= maxBytes {
				return true
			}
		}
	}
	return false
}
