package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
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
	tools map[string]ToolConfig
}

var defaultToolRegistry *ToolRegistry

// GetToolRegistry returns the default tool registry
func GetToolRegistry() *ToolRegistry {
	if defaultToolRegistry == nil {
		defaultToolRegistry = newDefaultToolRegistry()
	}
	return defaultToolRegistry
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
			{"file_path", "string", true, []string{"path"}, "Path to the file to read"},
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
			{"file_path", "string", true, []string{"path"}, "Path to the file to write"},
			{"content", "string", true, []string{}, "Content to write to the file"},
		},
		Handler: handleWriteFile,
	})

	// Register edit_file tool
	registry.RegisterTool(ToolConfig{
		Name:        "edit_file",
		Description: "Edit a file by replacing old string with new string",
		Parameters: []ParameterConfig{
			{"file_path", "string", true, []string{"path"}, "Path to the file to edit"},
			{"old_string", "string", true, []string{}, "String to replace"},
			{"new_string", "string", true, []string{}, "Replacement string"},
		},
		Handler: handleEditFile,
	})

	// Register todo tools
	registry.RegisterTool(ToolConfig{
		Name:        "add_todo",
		Description: "Add a new todo item",
		Parameters: []ParameterConfig{
			{"task", "string", true, []string{"content", "description"}, "The todo task description"},
		},
		Handler: handleAddTodo,
	})

	// Register add_todos (bulk add) tool
	registry.RegisterTool(ToolConfig{
		Name:        "add_todos",
		Description: "Create multiple todo items",
		Parameters: []ParameterConfig{
			{"todos", "array", true, []string{}, "Array of todos: {title, description?, priority?}"},
		},
		Handler: handleAddTodos,
	})

	registry.RegisterTool(ToolConfig{
		Name:        "update_todo_status",
		Description: "Update the status of a todo item",
		Parameters: []ParameterConfig{
			{"task_id", "string", true, []string{"id"}, "The ID of the todo task (e.g., 'todo_1')"},
			{"status", "string", true, []string{}, "New status: pending, in_progress, completed, cancelled"},
		},
		Handler: handleUpdateTodoStatus,
	})

	registry.RegisterTool(ToolConfig{
		Name:        "list_todos",
		Description: "List all todo items",
		Parameters:  []ParameterConfig{},
		Handler:     handleListTodos,
	})

	// Optional compact/maintenance todo tools (to avoid unknown tool calls)
	registry.RegisterTool(ToolConfig{
		Name:        "get_active_todos_compact",
		Description: "Get compact view of active todos",
		Parameters:  []ParameterConfig{},
		Handler: func(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
			return tools.GetActiveTodosCompact(), nil
		},
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

	// Register aliases: find and find_files map to same handler/parameters
	registry.RegisterTool(ToolConfig{
		Name:        "find",
		Description: "Find text pattern in files (alias of search_files)",
		Parameters: []ParameterConfig{
			{"pattern", "string", true, []string{"search_pattern"}, "Text pattern or regex to search for"},
			{"directory", "string", false, []string{"root"}, "Directory to search (default: .)"},
			{"file_glob", "string", false, []string{"file_pattern", "glob"}, "Glob to limit files (e.g., *.go)"},
			{"case_sensitive", "bool", false, []string{}, "Case sensitive search (default: false)"},
			{"max_results", "int", false, []string{}, "Maximum results to return (default: 50)"},
			{"max_bytes", "int", false, []string{}, "Maximum total bytes of matches to return (default: 20480)"},
		},
		Handler: handleSearchFiles,
	})
	registry.RegisterTool(ToolConfig{
		Name:        "find_files",
		Description: "Find text pattern in files (alias of search_files)",
		Parameters: []ParameterConfig{
			{"pattern", "string", true, []string{"search_pattern"}, "Text pattern or regex to search for"},
			{"directory", "string", false, []string{"root"}, "Directory to search (default: .)"},
			{"file_glob", "string", false, []string{"file_pattern", "glob"}, "Glob to limit files (e.g., *.go)"},
			{"case_sensitive", "bool", false, []string{}, "Case sensitive search (default: false)"},
			{"max_results", "int", false, []string{}, "Maximum results to return (default: 50)"},
			{"max_bytes", "int", false, []string{}, "Maximum total bytes of matches to return (default: 20480)"},
		},
		Handler: handleSearchFiles,
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

	// Validate and extract parameters
	validatedArgs, err := r.validateParameters(tool, args)
	if err != nil {
		return "", fmt.Errorf("parameter validation failed for tool '%s': %w", toolName, err)
	}

	// Execute the tool handler
	return tool.Handler(ctx, agent, validatedArgs)
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

// convertParameterType converts a parameter to the expected type
func (r *ToolRegistry) convertParameterType(value interface{}, expectedType string) (interface{}, error) {
	switch expectedType {
	case "string":
		if str, ok := value.(string); ok {
			return str, nil
		}
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

	a.ToolLog("writing file", filePath)
	a.debugLog("Writing file: %s\n", filePath)

	// Track the file write for change tracking
	if trackErr := a.TrackFileWrite(filePath, content); trackErr != nil {
		a.debugLog("Warning: Failed to track file write: %v\n", trackErr)
	}

	result, err := tools.WriteFile(ctx, filePath, content)
	a.debugLog("Write file result: %s, error: %v\n", result, err)
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

	a.ToolLog("editing file", fmt.Sprintf("%s (%s → %s)", filePath,
		truncateString(oldString, 30), truncateString(newString, 30)))
	a.debugLog("Editing file: %s\n", filePath)
	a.debugLog("Old string: %s\n", oldString)
	a.debugLog("New string: %s\n", newString)

	// Track the file edit for change tracking
	if trackErr := a.TrackFileEdit(filePath, oldString, newString); trackErr != nil {
		a.debugLog("Warning: Failed to track file edit: %v\n", trackErr)
	}

	result, err := tools.EditFile(ctx, filePath, oldString, newString)
	a.debugLog("Edit file result: %s, error: %v\n", result, err)

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
	a.ToolLog("adding todo", task)
	a.debugLog("Adding todo: %s\n", task)

	result := tools.AddTodo(task, "", "medium") // title, description, priority
	a.debugLog("Add todo result: %s\n", result)
	return result, nil
}

func handleUpdateTodoStatus(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Accept id as string or number for robustness
	var taskID string
	if idStr, ok := args["task_id"].(string); ok {
		taskID = idStr
	} else if idAlt, ok := args["id"].(string); ok {
		taskID = idAlt
	} else if idNum, ok := args["task_id"].(float64); ok {
		taskID = fmt.Sprintf("todo_%d", int(idNum))
	} else if idNumAlt, ok := args["id"].(float64); ok {
		taskID = fmt.Sprintf("todo_%d", int(idNumAlt))
	} else if idInt, ok := args["task_id"].(int); ok { // just in case
		taskID = fmt.Sprintf("todo_%d", idInt)
	} else if idIntAlt, ok := args["id"].(int); ok {
		taskID = fmt.Sprintf("todo_%d", idIntAlt)
	} else {
		return "", fmt.Errorf("invalid or missing task_id/id argument")
	}

	// Normalize string numeric IDs like "1" to internal format "todo_1"
	if !strings.HasPrefix(taskID, "todo_") {
		if _, err := strconv.Atoi(taskID); err == nil {
			taskID = fmt.Sprintf("todo_%s", taskID)
		}
	}

	status, ok := args["status"].(string)
	if !ok {
		return "", fmt.Errorf("invalid status argument")
	}

	a.ToolLog("updating todo", fmt.Sprintf("task %s to %s", taskID, status))
	a.debugLog("Updating todo %s to status: %s\n", taskID, status)

	result := tools.UpdateTodoStatus(taskID, status)
	if result == "Todo not found" && !strings.HasPrefix(taskID, "todo_") {
		if resolved, ok := tools.FindTodoIDByTitle(taskID); ok {
			a.debugLog("Resolved todo title '%s' to id %s\n", taskID, resolved)
			a.ToolLog("updating todo", fmt.Sprintf("resolved '%s' -> %s", taskID, resolved))
			taskID = resolved
			result = tools.UpdateTodoStatus(taskID, status)
		}
	}
	a.debugLog("Update todo result: %s\n", result)
	return result, nil
}

func handleAddTodos(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	todosRaw, ok := args["todos"]
	if !ok {
		return "", fmt.Errorf("missing todos argument")
	}

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

	// Log compact summary
	titles := make([]string, len(todos))
	for i, t := range todos {
		titles[i] = t.Title
	}
	if len(titles) == 1 {
		a.ToolLog("adding todo", titles[0])
	} else if len(titles) <= 3 {
		a.ToolLog("adding todos", strings.Join(titles, ", "))
	} else {
		a.ToolLog("adding todos", fmt.Sprintf("%s, %s, +%d more", titles[0], titles[1], len(titles)-2))
	}
	a.debugLog("Adding %d todos\n", len(todos))

	result := tools.AddBulkTodos(todos)
	fmt.Printf("%s", result)
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

// Helper function for string truncation
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func handleValidateBuild(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	a.ToolLog("running build validation", "")
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

	// Log the search action so users can see that the tool actually executed
	a.ToolLog("searching files", fmt.Sprintf("%q in %s (max %d)", pattern, root, maxResults))
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
	a.ToolLog("web search", query)
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
	a.ToolLog("fetching url", url)
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
	a.ToolLog("analyzing ui screenshot", imagePath)
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

	a.ToolLog("analyzing image", imagePath)
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

	a.ToolLog("viewing history", strings.Join(logParts, " "))
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

	logAction := "previewing rollback"
	if revisionID == "" {
		logAction = "listing revisions"
	} else if filePath != "" {
		if confirm {
			logAction = "rolling back file"
		} else {
			logAction = "previewing file rollback"
		}
	} else if confirm {
		logAction = "rolling back revision"
	}

	logDetails := []string{}
	if revisionID != "" {
		logDetails = append(logDetails, fmt.Sprintf("rev=%s", revisionID))
	}
	if filePath != "" {
		logDetails = append(logDetails, fmt.Sprintf("file=%s", filePath))
	}
	if confirm {
		logDetails = append(logDetails, "confirm")
	}

	a.ToolLog(logAction, strings.Join(logDetails, " "))
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
