package agent

import (
    "fmt"
    "strings"

    "github.com/alantheprice/ledit/pkg/agent_tools"
)

// ToolHandler represents a function that can handle a tool execution
type ToolHandler func(a *Agent, args map[string]interface{}) (string, error)

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

	// Register build validation tool
	registry.RegisterTool(ToolConfig{
		Name:        "validate_build",
		Description: "Validate project build after file operations",
		Parameters:  []ParameterConfig{},
		Handler:     handleValidateBuild,
	})

	return registry
}

// RegisterTool adds a tool to the registry
func (r *ToolRegistry) RegisterTool(config ToolConfig) {
	r.tools[config.Name] = config
}

// ExecuteTool executes a tool with standardized parameter validation and error handling
func (r *ToolRegistry) ExecuteTool(toolName string, args map[string]interface{}, agent *Agent) (string, error) {
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
	return tool.Handler(agent, validatedArgs)
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

func handleShellCommand(a *Agent, args map[string]interface{}) (string, error) {
	command := args["command"].(string)
	return a.executeShellCommandWithTruncation(command)
}

func handleReadFile(a *Agent, args map[string]interface{}) (string, error) {
	filePath := args["file_path"].(string)

	// Check for optional line range parameters
	startLine, hasStart := args["start_line"].(int)
	endLine, hasEnd := args["end_line"].(int)

	// Log the operation
	if hasStart || hasEnd {
		a.ToolLog("reading file", fmt.Sprintf("%s (lines %d-%d)", filePath, startLine, endLine))
		a.debugLog("Reading file: %s (lines %d-%d)\n", filePath, startLine, endLine)
		result, err := tools.ReadFileWithRange(filePath, startLine, endLine)
		a.debugLog("Read file result: %s, error: %v\n", result, err)
		return result, err
	} else {
		a.ToolLog("reading file", filePath)
		a.debugLog("Reading file: %s\n", filePath)
		result, err := tools.ReadFile(filePath)
		a.debugLog("Read file result: %s, error: %v\n", result, err)
		return result, err
	}
}

func handleWriteFile(a *Agent, args map[string]interface{}) (string, error) {
	filePath := args["file_path"].(string)
	content := args["content"].(string)

	a.ToolLog("writing file", filePath)
	a.debugLog("Writing file: %s\n", filePath)

	// Track the file write for change tracking
	if trackErr := a.TrackFileWrite(filePath, content); trackErr != nil {
		a.debugLog("Warning: Failed to track file write: %v\n", trackErr)
	}

	result, err := tools.WriteFile(filePath, content)
	a.debugLog("Write file result: %s, error: %v\n", result, err)
	return result, err
}

func handleEditFile(a *Agent, args map[string]interface{}) (string, error) {
	filePath := args["file_path"].(string)
	oldString := args["old_string"].(string)
	newString := args["new_string"].(string)

	// Read the original content for diff display
	originalContent, err := tools.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read original file for diff: %w", err)
	}

	// TODO: Implement circuit breaker
	// if blocked, warning := a.CheckCircuitBreaker("edit_file", filePath, 3); blocked {
	// 	return warning, fmt.Errorf("circuit breaker triggered - too many edit attempts on same file")
	// }

	a.ToolLog("editing file", fmt.Sprintf("%s (%s → %s)", filePath,
		truncateString(oldString, 30), truncateString(newString, 30)))
	a.debugLog("Editing file: %s\n", filePath)
	a.debugLog("Old string: %s\n", oldString)
	a.debugLog("New string: %s\n", newString)

	// Track the file edit for change tracking
	if trackErr := a.TrackFileEdit(filePath, oldString, newString); trackErr != nil {
		a.debugLog("Warning: Failed to track file edit: %v\n", trackErr)
	}

	result, err := tools.EditFile(filePath, oldString, newString)
	a.debugLog("Edit file result: %s, error: %v\n", result, err)

	// Display diff if successful
	if err == nil {
		newContent, readErr := tools.ReadFile(filePath)
		if readErr == nil {
			a.ShowColoredDiff(originalContent, newContent, 50)
		}
	}

	return result, err
}

func handleAddTodo(a *Agent, args map[string]interface{}) (string, error) {
    task := args["task"].(string)
    a.ToolLog("adding todo", task)
    a.debugLog("Adding todo: %s\n", task)

	result := tools.AddTodo(task, "", "medium") // title, description, priority
	a.debugLog("Add todo result: %s\n", result)
	return result, nil
}

func handleUpdateTodoStatus(a *Agent, args map[string]interface{}) (string, error) {
    taskID := args["task_id"].(string)
    status := args["status"].(string)

    a.ToolLog("updating todo", fmt.Sprintf("task %s to %s", taskID, status))
    a.debugLog("Updating todo %s to status: %s\n", taskID, status)

    result := tools.UpdateTodoStatus(taskID, status)
    a.debugLog("Update todo result: %s\n", result)
    return result, nil
}

func handleAddTodos(a *Agent, args map[string]interface{}) (string, error) {
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
    a.debugLog("Add todos result: %s\n", result)
    return result, nil
}

func handleListTodos(a *Agent, args map[string]interface{}) (string, error) {
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

func handleValidateBuild(a *Agent, args map[string]interface{}) (string, error) {
	a.ToolLog("running build validation", "")
	a.debugLog("Running build validation\n")
	
	result, err := tools.ValidateBuild()
	a.debugLog("Build validation result: %s, error: %v\n", result, err)
	return result, err
}
