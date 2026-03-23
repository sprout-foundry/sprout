package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/security_validator"
	"github.com/alantheprice/ledit/pkg/utils"
)

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
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	Parameters    []ParameterConfig  `json:"parameters"`
	Handler       ToolHandler        `json:"-"` // Function reference, not serialized
	HandlerImages ToolHandlerWithImages `json:"-"` // Optional image-returning handler (takes precedence over Handler when set)
}

// ToolHandler represents a function that can handle a tool execution
type ToolHandler func(ctx context.Context, a *Agent, args map[string]interface{}) (string, error)

// ToolHandlerWithImages is like ToolHandler but can also return image data
// for multimodal (vision-capable) models. The []api.ImageData slice should be
// nil when no images are produced; the string is always the text result.
type ToolHandlerWithImages func(ctx context.Context, a *Agent, args map[string]interface{}) ([]api.ImageData, string, error)

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

	// Register git tool - handles write operations that require approval
	registry.RegisterTool(ToolConfig{
		Name:        "git",
		Description: "Execute git write operations that modify the repository. All operations require user approval. Commit operations should use the /commit slash command for the interactive commit flow. For read-only operations (status, log, diff, etc.), use the shell_command tool instead.",
		Parameters: []ParameterConfig{
			{"operation", "string", true, []string{"op"}, "Git operation type: commit, push, add, rm, mv, reset, rebase, merge, checkout, branch_delete, tag, clean, stash, am, apply, cherry_pick, revert"},
			{"args", "string", false, []string{}, "Arguments to pass to the git command (optional)"},
		},
		Handler: handleGitOperation,
	})

	// Register read_file tool
	registry.RegisterTool(ToolConfig{
		Name:        "read_file",
		Description: "Read contents of a file",
		Parameters: []ParameterConfig{
			{"path", "string", true, []string{"file_path"}, "Path to the file to read"},
			{"view_range", "array", false, []string{}, "Line range as [start, end] array (1-based)"},
		},
		Handler:       handleReadFile,
		HandlerImages: handleReadFileWithImages,
	})

	// Register write_file tool
	registry.RegisterTool(ToolConfig{
		Name:        "write_file",
		Description: "Write content to a file",
		Parameters: []ParameterConfig{
			{"path", "string", true, []string{"file_path"}, "Path to the file to write"},
			{"content", "string", true, []string{}, "Content to write to the file"},
		},
		Handler: handleWriteFile,
	})

	// Register edit_file tool
	registry.RegisterTool(ToolConfig{
		Name:        "edit_file",
		Description: "Edit a file by replacing old string with new string",
		Parameters: []ParameterConfig{
			{"path", "string", true, []string{"file_path"}, "Path to the file to edit"},
			{"old_str", "string", true, []string{"old_string"}, "String to replace"},
			{"new_str", "string", true, []string{"new_string"}, "Replacement string"},
		},
		Handler: handleEditFile,
	})

	// Register write_structured_file tool
	registry.RegisterTool(ToolConfig{
		Name:        "write_structured_file",
		Description: "Write schema-validated structured data to JSON/YAML with deterministic formatting",
		Parameters: []ParameterConfig{
			{"path", "string", true, []string{"file_path"}, "Path to the structured file to write"},
			{"format", "string", false, []string{}, "Optional format override: json or yaml (otherwise inferred from extension)"},
			{"data", "object", true, []string{}, "Structured data object/array to serialize"},
			{"schema", "object", false, []string{}, "Optional JSON Schema subset used to validate data before writing"},
		},
		Handler: handleWriteStructuredFile,
	})

	// Register patch_structured_file tool
	registry.RegisterTool(ToolConfig{
		Name:        "patch_structured_file",
		Description: "Apply JSON Patch operations (add/replace/remove/test) to existing JSON/YAML then write it back",
		Parameters: []ParameterConfig{
			{"path", "string", true, []string{"file_path"}, "Path to the structured file to patch"},
			{"format", "string", false, []string{}, "Optional format override: json or yaml (otherwise inferred from extension)"},
			{"patch_ops", "array", false, []string{"ops", "operations"}, "JSON Patch operations array"},
			{"schema", "object", false, []string{}, "Optional JSON Schema subset used to validate document after patch"},
			{"data", "object", false, []string{}, "Optional full-document structured payload; if provided without patch_ops, this call is treated as write_structured_file"},
		},
		Handler: handlePatchStructuredFile,
	})

	// TodoWrite - Creates and manages a structured task list
	registry.RegisterTool(ToolConfig{
		Name:        "TodoWrite",
		Description: "Use this tool to create and manage a structured task list for your current coding session.",
		Parameters: []ParameterConfig{
			{"todos", "array", true, []string{}, "Array of todo items: [{content, status, activeForm?, priority?, id?}]"},
		},
		Handler: handleTodoWrite,
	})

	// TodoRead - Returns the current todo list (no parameters)
	registry.RegisterTool(ToolConfig{
		Name:        "TodoRead",
		Description: "Use this tool to read the current to-do list for the session.",
		Parameters:  []ParameterConfig{},
		Handler:     handleTodoRead,
	})

	// Register run_subagent tool - for multi-agent collaboration
	registry.RegisterTool(ToolConfig{
		Name:        "run_subagent",
		Description: "Delegate a SINGLE implementation task to a subagent. Spawns an agent subprocess with a focused task, waits for completion, and returns all output. Use this when: (1) Tasks must be done SEQUENTIALLY with dependencies between them, (2) You need to review results before deciding next steps, (3) Working on a single focused feature. For MULTIPLE INDEPENDENT tasks, use run_parallel_subagents instead for faster completion.\n\n**REQUIRED**: You MUST specify a persona parameter. Personas are configured from JSON defaults plus user config (for example: general, coder, debugger, tester, code_reviewer, researcher, web_scraper).\n\nSubagents use focused per-persona tool subsets from configuration for more deterministic behavior. NO TIMEOUT - runs until completion. Subagent provider and model are configured via config settings (subagent_provider and subagent_model).",
		Parameters: []ParameterConfig{
			{"prompt", "string", true, []string{}, "The prompt/task for the subagent to execute (required)"},
			{"persona", "string", true, []string{}, "REQUIRED: Subagent persona ID or alias (see /persona list)"},
			{"context", "string", false, []string{}, "Context from previous subagent work (files created, summaries, etc.)"},
			{"files", "string", false, []string{}, "Comma-separated list of relevant file paths (e.g., 'models/user.go,pkg/auth/jwt.go')"},
			{"auto_files", "bool", false, []string{}, "Automatically extract file paths mentioned in the prompt and include them in the context (default: true)"},
		},
		Handler: handleRunSubagent,
	})

	// Register run_parallel_subagents tool - for concurrent multi-agent execution
	registry.RegisterTool(ToolConfig{
		Name:        "run_parallel_subagents",
		Description: "Execute MULTIPLE INDEPENDENT subagent tasks CONCURRENTLY in parallel. Use this when you have 2+ tasks that can be done SIMULTANEOUSLY without dependencies (e.g., researching different code areas, writing code + tests concurrently, analyzing multiple files). This is MUCH FASTER than running tasks sequentially. Waits for ALL tasks to complete and returns results for each task by ID. Results include stdout, stderr, exit_code, completed status, and timed_out status for each task ID. Prefer this over run_subagent when tasks are independent.\n\nAccepts simple array of strings: [\"task 1 description\", \"task 2 description\", \"task 3\"]. IDs will be auto-generated (task-1, task-2, etc.).\n\nNote: Personas are only supported for single subagent execution via run_subagent. Parallel subagents use the default subagent configuration.\n\nSubagent provider and model are configured via config settings (subagent_provider and subagent_model).",
		Parameters: []ParameterConfig{
			{"subagents", "array", true, []string{}, "Array of task descriptions as strings: [\"task 1\", \"task 2\", \"task 3\"]. Auto-generates IDs like task-1, task-2, etc. Example: [\"Research X\", \"Implement Y\", \"Write tests for Z\"]"},
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
			{"max_bytes", "int", false, []string{}, "Maximum total bytes of matches to return (default: 102400)"},
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
		Description: "Fetch and extract content from a URL. For HTML/text content, extracts readable text. For images and PDFs (when the model supports vision), returns visual content directly.",
		Parameters: []ParameterConfig{
			{"url", "string", true, []string{}, "URL to fetch content from"},
		},
		Handler:       handleFetchURL,
		HandlerImages: handleFetchURLWithImages,
	})

	// Register browse_url tool
	registry.RegisterTool(ToolConfig{
		Name:        "browse_url",
		Description: "Open a URL in a headless browser. Use for: (1) taking screenshots of web pages including localhost apps, (2) capturing rendered DOM output after JavaScript execution, (3) extracting visible text from JS-rendered pages. Supports custom viewport sizes and user-agents for responsive testing.",
		Parameters: []ParameterConfig{
			{"url", "string", true, []string{}, "URL to browse — works with localhost URLs for testing local apps"},
			{"action", "string", false, []string{}, "What to do: 'screenshot' (save PNG), 'dom' (return rendered HTML), 'text' (return visible text, default)"},
			{"screenshot_path", "string", false, []string{}, "File path to save screenshot (required when action=screenshot, e.g. /tmp/ledit_examples/screenshot.png)"},
			{"viewport_width", "int", false, []string{}, "Browser viewport width in pixels (default: 1280)"},
			{"viewport_height", "int", false, []string{}, "Browser viewport height in pixels (default: 720)"},
			{"user_agent", "string", false, []string{}, "Override the browser User-Agent string"},
		},
		Handler: handleBrowseURL,
	})

	// Register vision analysis tools
	registry.RegisterTool(ToolConfig{
		Name:        "analyze_ui_screenshot",
		Description: "Analyze UI screenshots, mockups, or live HTML pages for implementation feedback. Accepts image files (PNG/JPG/WebP), remote image URLs, and local HTML files which are automatically rendered via a headless browser before analysis. Ideal for quick visual testing of dev builds and design reviews.",
		Parameters: []ParameterConfig{
			{"image_path", "string", true, []string{}, "Path or URL to the UI screenshot or HTML file"},
			{"analysis_prompt", "string", false, []string{}, "Optional custom vision prompt for analysis"},
			{"viewport_width", "int", false, []string{}, "Browser viewport width in pixels for HTML files (default: 1280)"},
			{"viewport_height", "int", false, []string{}, "Browser viewport height in pixels for HTML files (default: 720)"},
		},
		Handler: handleAnalyzeUIScreenshot,
	})

	registry.RegisterTool(ToolConfig{
		Name:        "analyze_image_content",
		Description: "Analyze images/PDFs for text/code extraction or general insights. Supports local file paths and remote HTTP(S) URLs.",
		Parameters: []ParameterConfig{
			{"image_path", "string", true, []string{}, "Path or URL to an image or PDF to analyze (local path or HTTP(S) URL)"},
			{"analysis_prompt", "string", false, []string{}, "Optional custom vision prompt"},
			{"analysis_mode", "string", false, []string{}, "Optional analysis mode override"},
		},
		Handler:       handleAnalyzeImageContent,
		HandlerImages: handleAnalyzeImageContentWithImages,
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

	// Register self-review tool for canonical spec validation
	registry.RegisterTool(ToolConfig{
		Name:        "self_review",
		Description: "Review the agent's own work against a canonical specification extracted from the conversation to detect scope creep and ensure alignment with user requirements",
		Parameters: []ParameterConfig{
			{
				Name:         "revision_id",
				Type:         "string",
				Required:     false,
				Alternatives: []string{},
				Description:  "Optional revision ID to review (defaults to current/most recent revision)",
			},
		},
		Handler: handleSelfReview,
	})

	// Register list_skills tool
	registry.RegisterTool(ToolConfig{
		Name:        "list_skills",
		Description: "List all available skills that can be activated. Skills are instruction bundles that can be loaded into context to provide domain expertise.",
		Parameters:  []ParameterConfig{},
		Handler:     handleListSkills,
	})

	// Register activate_skill tool
	registry.RegisterTool(ToolConfig{
		Name:        "activate_skill",
		Description: "Activate a skill to load its instructions into your context. Use this when you need the skill's expertise for the current task.",
		Parameters: []ParameterConfig{
			{"skill_id", "string", true, []string{"skill", "id"}, "The ID of the skill to activate (e.g., 'go-conventions', 'test-writing')"},
		},
		Handler: handleActivateSkill,
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

// GetAvailableTools returns a list of all registered tool names
func (r *ToolRegistry) GetAvailableTools() []string {
	tools := make([]string, 0, len(r.tools))
	for toolName := range r.tools {
		tools = append(tools, toolName)
	}
	return tools
}

// ExecuteTool executes a tool with standardized parameter validation and error handling
func (r *ToolRegistry) ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}, agent *Agent) ([]api.ImageData, string, error) {
	tool, exists := r.tools[toolName]
	if !exists {
		return nil, "", fmt.Errorf("unknown tool '%s'", toolName)
	}

	// CRITICAL: Prevent subagents from creating nested subagents
	// This check ensures that subagents (identified by LEDIT_SUBAGENT env var)
	// cannot spawn further subagents, preventing runaway agent chains
	if os.Getenv("LEDIT_SUBAGENT") == "1" {
		if toolName == "run_subagent" || toolName == "run_parallel_subagents" {
			const errMsg = "SUBAGENT_RESTRICTION: Subagents are not allowed to spawn nested subagents. " +
				"This restriction prevents runaway agent chains and ensures proper task delegation. " +
				"If you need additional work done, please complete your current task and return " +
				"your results to the primary agent for further delegation."
			if agent != nil && agent.debug {
				agent.debugLog("🚫 Blocked subagent tool '%s' - nested subagents are not allowed\n", toolName)
			}
			return nil, "", fmt.Errorf("%s", errMsg)
		}
	}

	// Security validation (if enabled and not bypassed)
	if agent != nil && !filesystem.SecurityBypassEnabled(ctx) {
		if validationErr := r.validateToolSecurity(ctx, toolName, args, agent); validationErr != nil {
			return nil, "", validationErr
		}
	}

	// Validate and extract parameters
	validatedArgs, err := r.validateParameters(tool, args, agent)
	if err != nil {
		return nil, "", fmt.Errorf("parameter validation failed for tool '%s': %w", toolName, err)
	}

	// Execute the tool handler — prefer the image-capable handler when set
	if tool.HandlerImages != nil {
		return tool.HandlerImages(ctx, agent, validatedArgs)
	}
	result, err := tool.Handler(ctx, agent, validatedArgs)
	return nil, result, err
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

	// ShouldConfirm is already handled by ValidateToolCall in interactive mode
	// (the user is prompted there). In non-interactive mode, the validator's
	// applyThreshold sets ShouldBlock=true for dangerous operations, so
	// CAUTION is auto-allowed with a log warning.
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

		// If user already approved filesystem access this session, skip re-prompting
		if agent.IsSecurityBypassApproved() {
			agent.debugLog("🔓 Session-level security bypass: allowing file access outside working directory: %s\n", filePath)
			return filesystem.WithSecurityBypass(ctx)
		}

		// CRITICAL: When running as a subagent, we CANNOT prompt for user confirmation
		// because stdin is /dev/null. Instead, we must return the error and let the primary
		// agent handle the security decision.
		if os.Getenv("LEDIT_FROM_AGENT") == "1" {
			agent.debugLog("Subagent encountered filesystem security error for %s, delegating to primary agent\n", filePath)
			// Return the original context (without bypass) so the error is propagated
			return ctx
		}

		// Prompt user for confirmation (primary agent only)
		agentConfig := agent.GetConfig()
		logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)

		prompt := fmt.Sprintf("⚠️  Filesystem Security Warning\n\nThe tool '%s' is attempting to access a file outside the working directory:\n  %s\n\nDo you want to allow this? (yes/no): ", toolName, filePath)

		if logger.AskForConfirmation(prompt, false, false) {
			// User approved - enable security bypass for this operation and remember for the session
			agent.debugLog("User approved file access outside working directory: %s\n", filePath)
			agent.SetSecurityBypassApproved()
			return filesystem.WithSecurityBypass(ctx)
		} else {
			// User rejected - error will be returned as-is
			agent.debugLog("User rejected file access outside working directory: %s\n", filePath)
		}
	}
	return ctx
}

// validateParameters validates and extracts parameters according to tool configuration
func (r *ToolRegistry) validateParameters(tool ToolConfig, args map[string]interface{}, agent *Agent) (map[string]interface{}, error) {
	validated := make(map[string]interface{})

	for _, param := range tool.Parameters {
		value, found := r.extractParameter(param, args)

		if !found && param.Required {
			return nil, fmt.Errorf("required parameter '%s' missing", param.Name)
		}

		if found {
			// Type validation and conversion
			convertedValue, err := r.convertParameterType(value, param.Type, agent)
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
func (r *ToolRegistry) convertParameterType(value interface{}, expectedType string, agent *Agent) (interface{}, error) {
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
				if agent != nil && agent.debug {
					agent.debugLog("Expected string, got map[string]interface {}. Failed to convert to JSON: %v\n", err)
					agent.debugLog("Content as map keys: %v\n", getMapKeys(mapVal))
				}
				return "", fmt.Errorf("expected string, got %T (failed to convert map to JSON: %w)", value, err)
			}

			if agent != nil && agent.debug {
				agent.debugLog("Converted map to JSON string. Length: %d\n", len(jsonStr))
			}
			return jsonStr, nil
		}

		// Debug logging for other type conversion failures
		if agent != nil && agent.debug {
			agent.debugLog("Expected string, got %T. Value: %+v\n", value, value)
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

	case "array":
		if arr, ok := value.([]interface{}); ok {
			return arr, nil
		}
		return nil, fmt.Errorf("expected array, got %T", value)

	case "object":
		switch typed := value.(type) {
		case map[string]interface{}:
			return typed, nil
		case []interface{}:
			// Allow top-level arrays for structured content payloads.
			return typed, nil
		default:
			return nil, fmt.Errorf("expected object, got %T", value)
		}

	default:
		return value, nil // No conversion needed for unknown types
	}
}
