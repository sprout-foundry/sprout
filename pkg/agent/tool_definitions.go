package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/filesystem"
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
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	Parameters    []ParameterConfig     `json:"parameters"`
	Handler       ToolHandler           `json:"-"` // Function reference, not serialized
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
	tools map[string]ToolConfig
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

	// Register commit tool - dedicated commit tool that works without user interaction
	// Uses the automated commit flow with message generation
	registry.RegisterTool(ToolConfig{
		Name:        "commit",
		Description: "Commit staged changes with an auto-generated commit message. Use this tool instead of running 'git commit' directly. This tool uses the commit message generation and validation system. For read-only operations like status, log, diff, use shell_command instead.",
		Parameters: []ParameterConfig{
			{"message", "string", false, []string{"msg"}, "Commit message (optional). If not provided, a message will be auto-generated based on the staged changes."},
		},
		Handler: handleCommitTool,
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
		Description: "Open a URL in a headless browser. Use this directly for localhost app debugging, JS-rendered scraping, and web UI verification when you need rendered state or when Playwright/MCP is unavailable. Supports screenshots, rendered DOM/text capture, persistent browser sessions across tool calls, navigation and interaction steps, assertions, selector inspection, browser console/error capture, network request summaries including CORS signals, cookies/storage snapshots, and responsive testing via custom viewport sizes.",
		Parameters: []ParameterConfig{
			{"url", "string", true, []string{}, "URL to browse — works with localhost URLs for testing local apps"},
			{"action", "string", false, []string{}, "What to do: 'screenshot' (save PNG), 'dom' (return rendered HTML), 'text' (return visible text, default), or 'inspect' (return structured JSON with page state and diagnostics)"},
			{"screenshot_path", "string", false, []string{}, "File path to save screenshot (required when action=screenshot, e.g. /tmp/ledit_examples/screenshot.png)"},
			{"session_id", "string", false, []string{}, "Reuse a persistent built-in browser session across multiple browse_url calls for iterative debugging"},
			{"persist_session", "boolean", false, []string{}, "Keep the browser page alive after this call and return a session_id in inspect output"},
			{"close_session", "boolean", false, []string{}, "Close the referenced persistent session after this call completes"},
			{"viewport_width", "int", false, []string{}, "Browser viewport width in pixels (default: 1280)"},
			{"viewport_height", "int", false, []string{}, "Browser viewport height in pixels (default: 720)"},
			{"user_agent", "string", false, []string{}, "Override the browser User-Agent string"},
			{"wait_for_selector", "string", false, []string{}, "Optional CSS selector to wait for before capturing output or running steps"},
			{"wait_timeout_ms", "int", false, []string{}, "Optional selector wait timeout in milliseconds (default: 10000)"},
			{"steps", "array", false, []string{}, "Optional interaction steps. Each step object supports action=wait_for|wait_for_text|assert_selector|assert_text|assert_title|assert_url|click|hover|type|fill|press|sleep|scroll_to|navigate|reload|back|forward|eval plus selector/value/key/millis/script/expect fields as needed"},
			{"capture_selectors", "array", false, []string{}, "Optional list of CSS selectors to capture after interactions (text/html/value/basic attrs)"},
			{"capture_dom", "boolean", false, []string{}, "Include rendered DOM in inspect output"},
			{"capture_text", "boolean", false, []string{}, "Include visible text in inspect output"},
			{"include_console", "boolean", false, []string{}, "Include browser console messages and page errors in inspect output"},
			{"capture_network", "boolean", false, []string{}, "Include fetch/XHR network request summaries in inspect output"},
			{"capture_storage", "boolean", false, []string{}, "Include localStorage and sessionStorage snapshots in inspect output"},
			{"capture_cookies", "boolean", false, []string{}, "Include document.cookie-visible cookies in inspect output"},
			{"response_max_chars", "int", false, []string{}, "Optional per-field truncation limit for inspect output"},
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

	// Register memory tools
	registry.RegisterTool(ToolConfig{
		Name:        "add_memory",
		Description: "Save a memory to persist across all future conversations. Use this to remember user preferences, learned patterns, project-specific conventions, or anything useful for future sessions. Memories are stored as markdown files in ~/.ledit/memories/ and loaded into your system prompt automatically.",
		Parameters: []ParameterConfig{
			{"name", "string", true, []string{"title"}, "Short descriptive name for the memory (e.g., 'git-safety', 'test-conventions')"},
			{"content", "string", true, []string{}, "Markdown content to store in the memory file"},
		},
		Handler: handleAddMemory,
	})

	registry.RegisterTool(ToolConfig{
		Name:        "read_memory",
		Description: "Read a specific memory by name. Returns the full markdown content of the memory file.",
		Parameters: []ParameterConfig{
			{"name", "string", true, []string{}, "Name of the memory to read (without .md extension, e.g., 'git-safety')"},
		},
		Handler: handleReadMemory,
	})

	registry.RegisterTool(ToolConfig{
		Name:        "list_memories",
		Description: "List all saved memories. Returns memory names and their first lines (titles). Memories persist across all conversations.",
		Parameters:  []ParameterConfig{},
		Handler:     handleListMemories,
	})

	registry.RegisterTool(ToolConfig{
		Name:        "delete_memory",
		Description: "Delete a memory by name. Permanently removes the memory file from ~/.ledit/memories/.",
		Parameters: []ParameterConfig{
			{"name", "string", true, []string{}, "Name of the memory to delete (e.g., 'git-safety')"},
		},
		Handler: handleDeleteMemory,
	})

	return registry
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
				agent.debugLog("[NO] Blocked subagent tool '%s' - nested subagents are not allowed\n", toolName)
			}
			return nil, "", errors.New(errMsg)
		}
	}

	// Security validation — classify and block/prompt dangerous operations
	if secResult := tools.ClassifyToolCall(toolName, args); secResult.ShouldBlock || secResult.ShouldPrompt {
		if agent != nil && agent.GetUnsafeMode() {
			// Unsafe mode: bypass all security checks
			if agent.debug {
				agent.debugLog("[UNLOCK] Unsafe mode: bypassing security validation for %s (risk: %s)\n", toolName, secResult.Risk)
			}
		} else if agent != nil {
			// Check if we're running as a subagent — subagents cannot prompt
			isSubagent := os.Getenv("LEDIT_FROM_AGENT") == "1" || os.Getenv("LEDIT_SUBAGENT") == "1"

			// Determine if we can prompt the user interactively
			agentConfig := agent.GetConfig()
			logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)
			canPrompt := logger != nil && logger.IsInteractive() && !isSubagent

			if canPrompt {
				// INTERACTIVE: prompt user with detailed risk information (CLI mode only)
				prompt := buildSecurityPrompt(toolName, args, secResult)
				if !logger.AskForConfirmation(prompt, false, false) {
					return nil, "", fmt.Errorf("security rejected: user rejected %s — %s", toolName, secResult.Reasoning)
				}
			} else if mgr := agent.GetSecurityApprovalMgr(); mgr != nil && agent.GetEventBus() != nil && !isSubagent {
				// NON-INTERACTIVE or WEBUI: request approval via webui event bus
				// This path is used when running in webui mode, when interactive prompt is not available,
				// or when eventBus is available (e.g., in webui where stdin is /dev/null)
				if agent.debug {
					agent.debugLog("[APPROVAL] Requesting security approval via webui for %s (risk: %s)\n", toolName, secResult.Risk)
				}
				// Build extras with context the webui dialog needs (command, target, risk type)
				extras := map[string]string{}
				if secResult.RiskType != "" {
					extras["risk_type"] = formatRiskType(secResult.RiskType)
				}
				switch toolName {
				case "shell_command":
					if cmd, ok := args["command"].(string); ok && cmd != "" {
						extras["command"] = cmd
					}
				case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
					if path, ok := args["path"].(string); ok && path != "" {
						extras["target"] = path
					}
				case "git":
					if op, ok := args["operation"].(string); ok && op != "" {
						extras["target"] = fmt.Sprintf("git %s", op)
					}
				}
				if !mgr.RequestApproval(agent.GetEventBus(), agent.GetEventClientID(), toolName, secResult.Risk.String(), secResult.Reasoning, extras) {
					return nil, "", fmt.Errorf("security rejected: user rejected %s — %s", toolName, secResult.Reasoning)
				}
			} else if secResult.ShouldBlock {
				// NON-INTERACTIVE + DANGEROUS, no approval mechanism: always block (subagents or no terminal/webui)
				return nil, "", fmt.Errorf("security block: %s — %s", toolName, secResult.Reasoning)
			}
			// NON-INTERACTIVE + CAUTION, no approval mechanism: auto-allow
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
	if err != nil {
		return nil, result, fmt.Errorf("execute tool %q: %w", toolName, err)
	}
	return nil, result, nil
}

// buildSecurityPrompt constructs a detailed security approval prompt for the user
func buildSecurityPrompt(toolName string, args map[string]interface{}, secResult tools.SecurityResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("⚠  Security Warning — %s\n\n", secResult.Risk))

	// Show the actual command/operation
	switch toolName {
	case "shell_command":
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			sb.WriteString(fmt.Sprintf("Command:\n  %s\n\n", cmd))
		}
	case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		if path, ok := args["path"].(string); ok && path != "" {
			sb.WriteString(fmt.Sprintf("Target: %s\n\n", path))
		}
	case "git":
		if op, ok := args["operation"].(string); ok && op != "" {
			sb.WriteString(fmt.Sprintf("Operation: git %s\n\n", op))
		}
	}

	if secResult.RiskType != "" {
		sb.WriteString(fmt.Sprintf("Risk category: %s\n\n", formatRiskType(secResult.RiskType)))
	}

	sb.WriteString(fmt.Sprintf("Reasoning: %s\n\n", secResult.Reasoning))
	sb.WriteString("Do you want to proceed? (yes/no): ")

	return sb.String()
}

// formatRiskType returns a human-readable description for a risk type
func formatRiskType(riskType string) string {
	switch riskType {
	case "mass_deletion":
		return "Mass deletion — may delete all files in current directory or home"
	case "source_code_destruction":
		return "Source code destruction — may delete project source files"
	case "privilege_escalation":
		return "Privilege escalation — running with elevated permissions"
	case "remote_code_execution":
		return "Remote code execution — downloading and executing untrusted code"
	case "arbitrary_code_execution":
		return "Arbitrary code execution — executing arbitrary shell commands"
	case "destructive_git_operation":
		return "Destructive git operation — may rewrite published history"
	case "disk_destruction":
		return "Disk destruction — may destroy disk data or partition tables"
	case "critical_system_operation":
		return "Critical system operation — may cause irreversible system damage"
	case "system_instability":
		return "System instability — may crash the system or kill all processes"
	case "insecure_permissions":
		return "Insecure permissions — setting overly permissive file access"
	case "system_integrity":
		return "System integrity — writing to critical system files"
	default:
		return riskType
	}
}

// handleFileSecurityError checks if an error is due to filesystem security and prompts the user
// Returns a context with security bypass enabled if user approves, original context otherwise
func handleFileSecurityError(ctx context.Context, agent *Agent, toolName, filePath string, err error) context.Context {
	// Check if this is a filesystem security error
	if errors.Is(err, filesystem.ErrOutsideWorkingDirectory) || errors.Is(err, filesystem.ErrWriteOutsideWorkingDirectory) {
		// Unsafe mode bypasses filesystem security checks automatically
		if agent.GetUnsafeMode() {
			agent.debugLog("[UNLOCK] Unsafe mode: automatically allowing file access outside working directory: %s\n", filePath)
			return filesystem.WithSecurityBypass(ctx)
		}

		// If user already approved filesystem access this session, skip re-prompting
		if agent.IsSecurityBypassApproved() {
			agent.debugLog("[UNLOCK] Session-level security bypass: allowing file access outside working directory: %s\n", filePath)
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

		// Check if we're running as a subagent — subagents cannot prompt.
		// Note: LEDIT_FROM_AGENT is already checked above (returns early), but we also
		// check LEDIT_SUBAGENT here for completeness.
		isSubagent := os.Getenv("LEDIT_FROM_AGENT") == "1" || os.Getenv("LEDIT_SUBAGENT") == "1"

		// Determine if we can prompt the user interactively
		agentConfig := agent.GetConfig()
		logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)
		canPrompt := logger != nil && logger.IsInteractive() && !isSubagent

		if canPrompt {
			// INTERACTIVE: prompt user with CLI prompt
			prompt := fmt.Sprintf("[WARN] Filesystem Security Warning\n\nThe tool '%s' is attempting to access a file outside the working directory:\n  %s\n\nDo you want to allow this? (yes/no): ", toolName, filePath)
			if logger.AskForConfirmation(prompt, false, false) {
				// User approved - enable security bypass for this operation and remember for the session
				agent.debugLog("User approved file access outside working directory: %s\n", filePath)
				agent.SetSecurityBypassApproved()
				return filesystem.WithSecurityBypass(ctx)
			} else {
				// User rejected
				agent.debugLog("User rejected file access outside working directory: %s\n", filePath)
			}
		} else if mgr := agent.GetSecurityApprovalMgr(); mgr != nil && agent.GetEventBus() != nil && !isSubagent {
			// WEBUI/Event path: request approval via event bus
			prompt := fmt.Sprintf("The tool '%s' is attempting to access a file outside the working directory: %s", toolName, filePath)
			extras := map[string]string{
				"risk_type": "Filesystem Security",
				"target":    filePath,
			}
			if mgr.RequestApproval(agent.GetEventBus(), agent.GetEventClientID(), toolName, "CAUTION", prompt, extras) {
				agent.debugLog("User approved file access outside working directory: %s\n", filePath)
				agent.SetSecurityBypassApproved()
				return filesystem.WithSecurityBypass(ctx)
			} else {
				agent.debugLog("User rejected file access outside working directory: %s\n", filePath)
			}
		} else {
			// No prompting available (subagent or no mechanism), return original context (error will propagate)
			if agent.debug {
				agent.debugLog("Cannot prompt for filesystem security approval (subagent or no mechanism): %s\n", filePath)
			}
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
