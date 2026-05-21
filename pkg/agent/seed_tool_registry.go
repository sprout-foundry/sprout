package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	core "github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// NewSeedToolRegistry creates a seed core.ToolRegistry with all 30 sprout tools
// registered. The registry implements core.ToolExecutor directly, so it can be
// used as the Executor in core.Options.
//
// Seed's ToolRegistry handles: channel suffix stripping, alias resolution,
// argument parsing/repair, type coercion, required parameter validation,
// per-tool timeouts, result truncation, circuit breakers, parallel execution
// for SafeForParallel tools, and event publishing.
//
// Sprout-specific concerns are wired through:
//   - PreExecuteHook: security classification + subagent nesting prevention
//   - Handler closures: capture agent for sprout's (ctx, agent, args) signature
//     and apply all post-processing (constraints, truncation, secret redaction,
//     duplicate embedding check, TodoWrite events, error sanitization).
func NewSeedToolRegistry(agent *Agent) *core.ToolRegistry {
	var ep core.EventPublisher
	if agent != nil && agent.GetEventBus() != nil {
		ep = newRichEventPublisher(agent.GetEventBus(), agent)
	}

	return newSeedToolRegistryWithPublisher(agent, ep)
}

// newSeedToolRegistryWithPublisher creates a seed ToolRegistry using the
// provided EventPublisher. This is used by processQueryWithSeed which creates
// one shared publisher for both the registry and the seed core agent so that
// all events carry the same client_id/chat_id/user_id metadata.
func newSeedToolRegistryWithPublisher(agent *Agent, ep core.EventPublisher) *core.ToolRegistry {
	registry := core.NewToolRegistry(core.ToolRegistryOptions{
		DefaultTimeout:  5 * time.Minute,
		MaxResultSize:   50 * 1024,
		EventPublisher:  ep,
		PreExecuteHook:  newPreExecuteHook(agent),
	})

	// ---------------------------------------------------------------
	// Register all 31 tools
	// ---------------------------------------------------------------

	// 1. shell_command
	registry.Register(core.ToolConfig{
		Name:        "shell_command",
		Description: "Execute a shell command. Supports background execution (background=true) and checking accumulated output of a background session (check_background=session_id) and stopping a background session (stop_background=session_id)",
		Parameters: []core.ParameterConfig{
			{Name: "command", Type: "string", Alternatives: []string{"cmd"}, Description: "The shell command to execute (required unless check_background or stop_background is provided)"},
			{Name: "background", Type: "boolean", Description: "Run command in background and return immediately with session_id (default: false)"},
			{Name: "check_background", Type: "string", Description: "Session ID of a background session to check (returns accumulated output)"},
			{Name: "stop_background", Type: "string", Description: "Session ID of a background session to stop/terminate"},
		},
		Timeout: 2 * time.Minute,
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "shell_command")
			result, err := handleShellCommand(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "shell_command")
			}
			return postProcessResult(ctx, agent, "shell_command", args, result), nil
		},
	})

	// 2. git
	registry.Register(core.ToolConfig{
		Name:        "git",
		Description: "Execute git operations that modify repository state or require network access. All destructive operations require user approval. Commit operations should use the /commit slash command for the interactive commit flow. For read-only operations (status, log, diff, branch, show), use shell_command instead.",
		Parameters: []core.ParameterConfig{
			{Name: "operation", Type: "string", Required: true, Alternatives: []string{"op"}, Description: "Git operation type: commit, push, pull, fetch, add, rm, mv, reset, rebase, merge, checkout, branch_delete, tag, clean, stash, am, apply, cherry_pick, revert, restore"},
			{Name: "args", Type: "string", Description: "Arguments to pass to the git command (optional). For pull: --rebase, --ff-only, remote/branch. For fetch: --all, --prune, remote. For restore: --staged, pathspec."},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "git")
			result, err := handleGitOperation(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "git")
			}
			return postProcessResult(ctx, agent, "git", args, result), nil
		},
	})

	// 3. commit
	registry.Register(core.ToolConfig{
		Name:        "commit",
		Description: "Commit staged changes with an auto-generated commit message. Use this tool instead of running 'git commit' directly. This tool uses the commit message generation and validation system. For read-only operations like status, log, diff, use shell_command instead.",
		Parameters: []core.ParameterConfig{
			{Name: "message", Type: "string", Alternatives: []string{"msg"}, Description: "Commit message (optional). If not provided, a message will be auto-generated based on the staged changes."},
			{Name: "notes", Type: "string", Alternatives: []string{"context", "extra_context"}, Description: "Optional notes/context to integrate into the auto-generated commit message. Use this to provide context about why the changes were made, what task they relate to, or any other information that should be captured in the commit. These notes are combined with the diff analysis to produce a better commit message. Ignored if 'message' parameter is provided."},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "commit")
			result, err := handleCommitTool(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "commit")
			}
			return postProcessResult(ctx, agent, "commit", args, result), nil
		},
	})

	// 4. read_file (SafeForParallel, has HandlerWithImages)
	registry.Register(core.ToolConfig{
		Name:        "read_file",
		Description: "Read contents of a file",
		Parameters: []core.ParameterConfig{
			{Name: "path", Type: "string", Required: true, Alternatives: []string{"file_path"}, Description: "Path to the file to read"},
			{Name: "view_range", Type: "array", Description: "Line range as [start, end] array (1-based)"},
		},
		SafeForParallel: true,
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "read_file")
			result, err := handleReadFile(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "read_file")
			}
			return postProcessResult(ctx, agent, "read_file", args, result), nil
		},
		HandlerWithImages: func(ctx context.Context, args map[string]interface{}) ([]core.ImageData, string, error) {
			logToolExecution(agent, "read_file")
			imgs, result, err := handleReadFileWithImages(ctx, agent, args)
			if err != nil {
				msg, _ := handleToolError(agent, err, "read_file")
				return imgs, msg, err
			}
			return imgs, postProcessResult(ctx, agent, "read_file", args, result), nil
		},
	})

	// 5. write_file
	registry.Register(core.ToolConfig{
		Name:        "write_file",
		Description: "Write content to a file",
		Parameters: []core.ParameterConfig{
			{Name: "path", Type: "string", Required: true, Alternatives: []string{"file_path"}, Description: "Path to the file to write"},
			{Name: "content", Type: "string", Required: true, Description: "Content to write to the file"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "write_file")
			result, err := handleWriteFile(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "write_file")
			}
			return postProcessResult(ctx, agent, "write_file", args, result), nil
		},
	})

	// 6. edit_file
	registry.Register(core.ToolConfig{
		Name:        "edit_file",
		Description: "Edit a file by replacing old string with new string",
		Parameters: []core.ParameterConfig{
			{Name: "path", Type: "string", Required: true, Alternatives: []string{"file_path"}, Description: "Path to the file to edit"},
			{Name: "old_str", Type: "string", Required: true, Alternatives: []string{"old_string"}, Description: "String to replace"},
			{Name: "new_str", Type: "string", Required: true, Alternatives: []string{"new_string"}, Description: "Replacement string"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "edit_file")
			result, err := handleEditFile(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "edit_file")
			}
			return postProcessResult(ctx, agent, "edit_file", args, result), nil
		},
	})

	// 7. write_structured_file
	registry.Register(core.ToolConfig{
		Name:        "write_structured_file",
		Description: "Write schema-validated structured data to JSON/YAML with deterministic formatting",
		Parameters: []core.ParameterConfig{
			{Name: "path", Type: "string", Required: true, Alternatives: []string{"file_path"}, Description: "Path to the structured file to write"},
			{Name: "format", Type: "string", Description: "Optional format override: json or yaml (otherwise inferred from extension)"},
			{Name: "data", Type: "object", Required: true, Description: "Structured data object/array to serialize"},
			{Name: "schema", Type: "object", Description: "Optional JSON Schema subset used to validate data before writing"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "write_structured_file")
			result, err := handleWriteStructuredFile(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "write_structured_file")
			}
			return postProcessResult(ctx, agent, "write_structured_file", args, result), nil
		},
	})

	// 8. patch_structured_file
	registry.Register(core.ToolConfig{
		Name:        "patch_structured_file",
		Description: "Apply JSON Patch operations (add/replace/remove/test) to existing JSON/YAML then write it back",
		Parameters: []core.ParameterConfig{
			{Name: "path", Type: "string", Required: true, Alternatives: []string{"file_path"}, Description: "Path to the structured file to patch"},
			{Name: "format", Type: "string", Description: "Optional format override: json or yaml (otherwise inferred from extension)"},
			{Name: "patch_ops", Type: "array", Alternatives: []string{"ops", "operations"}, Description: "JSON Patch operations array"},
			{Name: "schema", Type: "object", Description: "Optional JSON Schema subset used to validate document after patch"},
			{Name: "data", Type: "object", Description: "Optional full-document structured payload; if provided without patch_ops, this call is treated as write_structured_file"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "patch_structured_file")
			result, err := handlePatchStructuredFile(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "patch_structured_file")
			}
			return postProcessResult(ctx, agent, "patch_structured_file", args, result), nil
		},
	})

	// 9. TodoWrite
	registry.Register(core.ToolConfig{
		Name:        "TodoWrite",
		Description: "Use this tool to create and manage a structured task list for your current coding session.",
		Parameters: []core.ParameterConfig{
			{Name: "todos", Type: "array", Required: true, Description: "Array of todo items: [{content, status, activeForm?, priority?, id?}]"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "TodoWrite")
			result, err := handleTodoWrite(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "TodoWrite")
			}
			return postProcessResult(ctx, agent, "TodoWrite", args, result), nil
		},
	})

	// 10. TodoRead
	registry.Register(core.ToolConfig{
		Name:        "TodoRead",
		Description: "Use this tool to read the current to-do list for the session.",
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "TodoRead")
			result, err := handleTodoRead(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "TodoRead")
			}
			return postProcessResult(ctx, agent, "TodoRead", args, result), nil
		},
	})

	// 11. ask_user
	registry.Register(core.ToolConfig{
		Name:        "ask_user",
		Description: "Ask the user a question and wait for their response. Use this when you need clarification, user input, or a decision that cannot be determined from context alone.",
		Parameters: []core.ParameterConfig{
			{Name: "question", Type: "string", Required: true, Description: "The question to ask the user (required)"},
		},
		Timeout: 10 * time.Minute, // Match AskUserManager.DefaultAskUserTimeout
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "ask_user")
			result, err := handleAskUser(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "ask_user")
			}
			return postProcessResult(ctx, agent, "ask_user", args, result), nil
		},
	})

	// 12. run_subagent (30min timeout)
	registry.Register(core.ToolConfig{
		Name:        "run_subagent",
		Description: "Delegate a SINGLE implementation task to a subagent. Runs an in-process agent with a focused task, waits for completion, and returns all output. Use this when: (1) Tasks must be done SEQUENTIALLY with dependencies between them, (2) You need to review results before deciding next steps, (3) Working on a single focused feature. For MULTIPLE INDEPENDENT tasks, use run_parallel_subagents instead for faster completion.\n\n**REQUIRED**: You MUST specify a persona parameter. Personas are configured from JSON defaults plus user config (for example: general, coder, debugger, tester, code_reviewer, researcher, web_scraper).\n\nSubagents use focused per-persona tool subsets from configuration for more deterministic behavior. NO TIMEOUT - runs until completion. Subagent provider and model are configured via config settings (subagent_provider and subagent_model).",
		Parameters: []core.ParameterConfig{
			{Name: "prompt", Type: "string", Required: true, Description: "The prompt/task for the subagent to execute (required)"},
			{Name: "persona", Type: "string", Required: true, Description: "REQUIRED: Subagent persona ID or alias (see /persona list)"},
			{Name: "context", Type: "string", Description: "Context from previous subagent work (files created, summaries, etc.)"},
			{Name: "files", Type: "string", Description: "Comma-separated list of relevant file paths (e.g., 'models/user.go,pkg/auth/jwt.go')"},
			{Name: "working_dir", Type: "string", Description: "Optional: directory to use as the subagent's working directory (must be within $HOME). Use this to spawn subagents operating in a different project directory."},
		},
		Timeout: 30 * time.Minute,
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "run_subagent")
			result, err := handleRunSubagent(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "run_subagent")
			}
			return postProcessResult(ctx, agent, "run_subagent", args, result), nil
		},
	})

	// 13. run_parallel_subagents (30min timeout)
	registry.Register(core.ToolConfig{
		Name:        "run_parallel_subagents",
		Description: "Execute MULTIPLE INDEPENDENT subagent tasks CONCURRENTLY in parallel. Use this when you have 2+ tasks that can be done SIMULTANEOUSLY without dependencies (e.g., researching different code areas, writing code + tests concurrently, analyzing multiple files). This is MUCH FASTER than running tasks sequentially. Waits for ALL tasks to complete and returns results for each task by ID. Results include stdout, stderr, exit_code, completed status, and timed_out status for each task ID. Prefer this over run_subagent when tasks are independent.\n\nAccepts simple array of strings: [\"task 1 description\", \"task 2 description\", \"task 3\"]. IDs will be auto-generated (task-1, task-2, etc.).\n\nNote: Personas are only supported for single subagent execution via run_subagent. Parallel subagents use the default subagent configuration.\n\nSubagent provider and model are configured via config settings (subagent_provider and subagent_model).",
		Parameters: []core.ParameterConfig{
			{Name: "subagents", Type: "array", Required: true, Description: "Array of task descriptions as strings: [\"task 1\", \"task 2\", \"task 3\"]. Auto-generates IDs like task-1, task-2, etc. Example: [\"Research X\", \"Implement Y\", \"Write tests for Z\"]"},
		},
		Timeout: 30 * time.Minute,
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "run_parallel_subagents")
			result, err := handleRunParallelSubagents(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "run_parallel_subagents")
			}
			return postProcessResult(ctx, agent, "run_parallel_subagents", args, result), nil
		},
	})

	// 14. search_files (SafeForParallel)
	registry.Register(core.ToolConfig{
		Name:        "search_files",
		Description: "Search text pattern in files (cross-platform, ignores .git, node_modules, .sprout by default)",
		Parameters: []core.ParameterConfig{
			{Name: "search_pattern", Type: "string", Required: true, Alternatives: []string{"pattern"}, Description: "Text pattern or regex to search for"},
			{Name: "directory", Type: "string", Alternatives: []string{"root"}, Description: "Directory to search (default: .)"},
			{Name: "file_glob", Type: "string", Alternatives: []string{"file_pattern", "glob"}, Description: "Glob to limit files (e.g., *.go)"},
			{Name: "case_sensitive", Type: "boolean", Description: "Case sensitive search (default: false)"},
			{Name: "max_results", Type: "integer", Description: "Maximum results to return (default: 50)"},
			{Name: "max_bytes", Type: "integer", Description: "Maximum total bytes of matches to return (default: 102400)"},
		},
		SafeForParallel: true,
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "search_files")
			result, err := handleSearchFiles(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "search_files")
			}
			return postProcessResult(ctx, agent, "search_files", args, result), nil
		},
	})

	// 14b. repo_map (SafeForParallel)
	registry.Register(core.ToolConfig{
		Name:        "repo_map",
		Description: "Generate a lightweight overview of the codebase showing file paths and top-level symbols (functions, types, interfaces, classes) with line numbers. Use this before reading files to identify which files and functions are relevant to your task, then use read_file with view_range to read only the sections you need. Output is limited to ~1024 tokens.",
		Parameters: []core.ParameterConfig{
			{Name: "directory", Type: "string", Description: "Directory to scan (default: workspace root)"},
		},
		SafeForParallel: true,
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "repo_map")
			result, err := handleRepoMap(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "repo_map")
			}
			return postProcessResult(ctx, agent, "repo_map", args, result), nil
		},
	})

	// 15. web_search
	registry.Register(core.ToolConfig{
		Name:        "web_search",
		Description: "Search web for relevant URLs",
		Parameters: []core.ParameterConfig{
			{Name: "query", Type: "string", Required: true, Description: "Search query to find relevant web content"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "web_search")
			result, err := handleWebSearch(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "web_search")
			}
			return postProcessResult(ctx, agent, "web_search", args, result), nil
		},
	})

	// 16. fetch_url (SafeForParallel, has HandlerWithImages)
	registry.Register(core.ToolConfig{
		Name:        "fetch_url",
		Description: "Fetch and extract content from a URL. For HTML/text content, extracts readable text. For images and PDFs (when the model supports vision), returns visual content directly.",
		Parameters: []core.ParameterConfig{
			{Name: "url", Type: "string", Required: true, Description: "URL to fetch content from"},
		},
		SafeForParallel: true,
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "fetch_url")
			result, err := handleFetchURL(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "fetch_url")
			}
			return postProcessResult(ctx, agent, "fetch_url", args, result), nil
		},
		HandlerWithImages: func(ctx context.Context, args map[string]interface{}) ([]core.ImageData, string, error) {
			logToolExecution(agent, "fetch_url")
			imgs, result, err := handleFetchURLWithImages(ctx, agent, args)
			if err != nil {
				msg, _ := handleToolError(agent, err, "fetch_url")
				return imgs, msg, err
			}
			return imgs, postProcessResult(ctx, agent, "fetch_url", args, result), nil
		},
	})

	// 17. browse_url
	registry.Register(core.ToolConfig{
		Name:        "browse_url",
		Description: "Open a URL in a headless browser. Use this directly for localhost app debugging, JS-rendered scraping, and web UI verification when you need rendered state or when Playwright/MCP is unavailable. Supports screenshots, rendered DOM/text capture, persistent browser sessions across tool calls, navigation and interaction steps, assertions, selector inspection, browser console/error capture, network request summaries including CORS signals, cookies/storage snapshots, and responsive testing via custom viewport sizes.",
		Parameters: []core.ParameterConfig{
			{Name: "url", Type: "string", Required: true, Description: "URL to browse — works with localhost URLs for testing local apps"},
			{Name: "action", Type: "string", Description: "What to do: 'screenshot' (save PNG), 'dom' (return rendered HTML), 'text' (return visible text, default), or 'inspect' (return structured JSON with page state and diagnostics)"},
			{Name: "screenshot_path", Type: "string", Description: "File path to save screenshot (required when action=screenshot, e.g. /tmp/sprout_examples/screenshot.png)"},
			{Name: "session_id", Type: "string", Description: "Reuse a persistent built-in browser session across multiple browse_url calls for iterative debugging"},
			{Name: "persist_session", Type: "boolean", Description: "Keep the browser page alive after this call and return a session_id in inspect output"},
			{Name: "close_session", Type: "boolean", Description: "Close the referenced persistent session after this call completes"},
			{Name: "viewport_width", Type: "integer", Description: "Browser viewport width in pixels (default: 1280)"},
			{Name: "viewport_height", Type: "integer", Description: "Browser viewport height in pixels (default: 720)"},
			{Name: "user_agent", Type: "string", Description: "Override the browser User-Agent string"},
			{Name: "wait_for_selector", Type: "string", Description: "Optional CSS selector to wait for before capturing output or running steps"},
			{Name: "wait_timeout_ms", Type: "integer", Description: "Optional selector wait timeout in milliseconds (default: 10000)"},
			{Name: "steps", Type: "array", Description: "Optional interaction steps. Each step object supports action=wait_for|wait_for_text|assert_selector|assert_text|assert_title|assert_url|click|hover|type|fill|press|sleep|scroll_to|navigate|reload|back|forward|eval plus selector/value/key/millis/script/expect fields as needed"},
			{Name: "capture_selectors", Type: "array", Description: "Optional list of CSS selectors to capture after interactions (text/html/value/basic attrs)"},
			{Name: "capture_dom", Type: "boolean", Description: "Include rendered DOM in inspect output"},
			{Name: "capture_text", Type: "boolean", Description: "Include visible text in inspect output"},
			{Name: "include_console", Type: "boolean", Description: "Include browser console messages and page errors in inspect output"},
			{Name: "capture_network", Type: "boolean", Description: "Include fetch/XHR network request summaries in inspect output"},
			{Name: "capture_storage", Type: "boolean", Description: "Include localStorage and sessionStorage snapshots in inspect output"},
			{Name: "capture_cookies", Type: "boolean", Description: "Include document.cookie-visible cookies in inspect output"},
			{Name: "response_max_chars", Type: "integer", Description: "Optional per-field truncation limit for inspect output"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "browse_url")
			result, err := handleBrowseURL(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "browse_url")
			}
			return postProcessResult(ctx, agent, "browse_url", args, result), nil
		},
	})

	// 18. analyze_ui_screenshot
	registry.Register(core.ToolConfig{
		Name:        "analyze_ui_screenshot",
		Description: "Analyze UI screenshots, mockups, or live HTML pages for implementation feedback. Accepts image files (PNG/JPG/WebP), remote image URLs, and local HTML files which are automatically rendered via a headless browser before analysis. Ideal for quick visual testing of dev builds and design reviews.",
		Parameters: []core.ParameterConfig{
			{Name: "image_path", Type: "string", Required: true, Description: "Path or URL to the UI screenshot or HTML file"},
			{Name: "analysis_prompt", Type: "string", Description: "Optional custom vision prompt for analysis"},
			{Name: "viewport_width", Type: "integer", Description: "Browser viewport width in pixels for HTML files (default: 1280)"},
			{Name: "viewport_height", Type: "integer", Description: "Browser viewport height in pixels for HTML files (default: 720)"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "analyze_ui_screenshot")
			result, err := handleAnalyzeUIScreenshot(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "analyze_ui_screenshot")
			}
			return postProcessResult(ctx, agent, "analyze_ui_screenshot", args, result), nil
		},
	})

	// 19. analyze_image_content (has HandlerWithImages)
	registry.Register(core.ToolConfig{
		Name:        "analyze_image_content",
		Description: "Analyze images/PDFs for text/code extraction or general insights. Supports local file paths and remote HTTP(S) URLs.",
		Parameters: []core.ParameterConfig{
			{Name: "image_path", Type: "string", Required: true, Description: "Path or URL to an image or PDF to analyze (local path or HTTP(S) URL)"},
			{Name: "analysis_prompt", Type: "string", Description: "Optional custom vision prompt"},
			{Name: "analysis_mode", Type: "string", Description: "Optional analysis mode override"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "analyze_image_content")
			result, err := handleAnalyzeImageContent(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "analyze_image_content")
			}
			return postProcessResult(ctx, agent, "analyze_image_content", args, result), nil
		},
		HandlerWithImages: func(ctx context.Context, args map[string]interface{}) ([]core.ImageData, string, error) {
			logToolExecution(agent, "analyze_image_content")
			imgs, result, err := handleAnalyzeImageContentWithImages(ctx, agent, args)
			if err != nil {
				msg, _ := handleToolError(agent, err, "analyze_image_content")
				return imgs, msg, err
			}
			return imgs, postProcessResult(ctx, agent, "analyze_image_content", args, result), nil
		},
	})

	// 20. view_history
	registry.Register(core.ToolConfig{
		Name:        "view_history",
		Description: "View recent change history tracked by the agent",
		Parameters: []core.ParameterConfig{
			{Name: "limit", Type: "integer", Description: "Maximum number of entries to return (default 10)"},
			{Name: "file_filter", Type: "string", Alternatives: []string{"filename"}, Description: "Filter by filename (partial match)"},
			{Name: "since", Type: "string", Description: "Only include changes after this ISO 8601 timestamp"},
			{Name: "show_content", Type: "boolean", Description: "Include content summaries for each change"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "view_history")
			result, err := handleViewHistory(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "view_history")
			}
			return postProcessResult(ctx, agent, "view_history", args, result), nil
		},
	})

	// 21. rollback_changes
	registry.Register(core.ToolConfig{
		Name:        "rollback_changes",
		Description: "Preview or perform a rollback of tracked revisions",
		Parameters: []core.ParameterConfig{
			{Name: "revision_id", Type: "string", Description: "Revision ID to rollback (leave blank to list revisions)"},
			{Name: "file_path", Type: "string", Alternatives: []string{"filename"}, Description: "Rollback only this file from the revision"},
			{Name: "confirm", Type: "boolean", Description: "Set to true to execute the rollback"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "rollback_changes")
			result, err := handleRollbackChanges(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "rollback_changes")
			}
			return postProcessResult(ctx, agent, "rollback_changes", args, result), nil
		},
	})

	// 22. self_review
	registry.Register(core.ToolConfig{
		Name:        "self_review",
		Description: "Review the agent's own work against a canonical specification extracted from the conversation to detect scope creep and ensure alignment with user requirements",
		Parameters: []core.ParameterConfig{
			{Name: "revision_id", Type: "string", Description: "Optional revision ID to review (defaults to current/most recent revision)"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "self_review")
			result, err := handleSelfReview(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "self_review")
			}
			return postProcessResult(ctx, agent, "self_review", args, result), nil
		},
	})

	// 23. list_skills
	registry.Register(core.ToolConfig{
		Name:        "list_skills",
		Description: "List all available skills that can be activated. Skills are instruction bundles that can be loaded into context to provide domain expertise.",
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "list_skills")
			result, err := handleListSkills(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "list_skills")
			}
			return postProcessResult(ctx, agent, "list_skills", args, result), nil
		},
	})

	// 24. activate_skill
	registry.Register(core.ToolConfig{
		Name:        "activate_skill",
		Description: "Activate a skill to load its instructions into your context. Use this when you need the skill's expertise for the current task.",
		Parameters: []core.ParameterConfig{
			{Name: "skill_id", Type: "string", Required: true, Alternatives: []string{"skill", "id"}, Description: "The ID of the skill to activate (e.g., 'project-planning', 'browse-debugging')"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "activate_skill")
			result, err := handleActivateSkill(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "activate_skill")
			}
			return postProcessResult(ctx, agent, "activate_skill", args, result), nil
		},
	})

	// 25. add_memory
	registry.Register(core.ToolConfig{
		Name:        "add_memory",
		Description: "Save a memory to persist across all future conversations. Use this to remember user preferences, learned patterns, project-specific conventions, or anything useful for future sessions. Memories are stored as markdown files in ~/.config/sprout/memories/ and loaded into your system prompt automatically.",
		Parameters: []core.ParameterConfig{
			{Name: "name", Type: "string", Required: true, Alternatives: []string{"title"}, Description: "Short descriptive name for the memory (e.g., 'git-safety', 'test-conventions')"},
			{Name: "content", Type: "string", Required: true, Description: "Markdown content to store in the memory file"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "add_memory")
			result, err := handleAddMemory(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "add_memory")
			}
			return postProcessResult(ctx, agent, "add_memory", args, result), nil
		},
	})

	// 26. read_memory
	registry.Register(core.ToolConfig{
		Name:        "read_memory",
		Description: "Read a specific memory by name. Returns the full markdown content of the memory file.",
		Parameters: []core.ParameterConfig{
			{Name: "name", Type: "string", Required: true, Description: "Name of the memory to read (without .md extension, e.g., 'git-safety')"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "read_memory")
			result, err := handleReadMemory(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "read_memory")
			}
			return postProcessResult(ctx, agent, "read_memory", args, result), nil
		},
	})

	// 27. list_memories
	registry.Register(core.ToolConfig{
		Name:        "list_memories",
		Description: "List all saved memories. Returns memory names and their first lines (titles). Memories persist across all conversations.",
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "list_memories")
			result, err := handleListMemories(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "list_memories")
			}
			return postProcessResult(ctx, agent, "list_memories", args, result), nil
		},
	})

	// 28. delete_memory
	registry.Register(core.ToolConfig{
		Name:        "delete_memory",
		Description: "Delete a memory by name. Permanently removes the memory file from ~/.config/sprout/memories/.",
		Parameters: []core.ParameterConfig{
			{Name: "name", Type: "string", Required: true, Description: "Name of the memory to delete (e.g., 'git-safety')"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "delete_memory")
			result, err := handleDeleteMemory(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "delete_memory")
			}
			return postProcessResult(ctx, agent, "delete_memory", args, result), nil
		},
	})

	// 29. embedding_index
	registry.Register(core.ToolConfig{
		Name:        "embedding_index",
		Description: "Manage the embedding index for duplicate detection and semantic search. Use 'build' to create a full index, 'update' to incrementally update changed files, or 'status' to check index state.",
		Parameters: []core.ParameterConfig{
			{Name: "operation", Type: "string", Required: true, Description: "Operation to perform: 'build' (full re-index), 'update' (incremental via git diff), or 'status' (check index state)"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "embedding_index")
			result, err := handleEmbeddingIndex(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "embedding_index")
			}
			return postProcessResult(ctx, agent, "embedding_index", args, result), nil
		},
	})

	// 30. semantic_search
	registry.Register(core.ToolConfig{
		Name:        "semantic_search",
		Description: "Search the codebase for semantically similar code using embedding vectors. Unlike text search, this finds code that does the same thing even with different names or implementations.",
		Parameters: []core.ParameterConfig{
			{Name: "query", Type: "string", Required: true, Description: "Natural language description of what you're looking for"},
			{Name: "top_k", Type: "integer", Description: "Maximum results to return (default: 5)"},
			{Name: "threshold", Type: "number", Description: "Minimum similarity score 0.0-1.0 (default: 0.75)"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "semantic_search")
			result, err := handleSemanticSearch(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "semantic_search")
			}
			return postProcessResult(ctx, agent, "semantic_search", args, result), nil
		},
	})

	// 31. task_queue_read
	registry.Register(core.ToolConfig{
		Name:        "task_queue_read",
		Description: "Read pending tasks from the persistent task queue. Returns tasks sorted by priority (high > medium > low). The queue persists across sessions and is stored at ~/.config/sprout/task_queue.json.",
		Parameters: []core.ParameterConfig{
			{Name: "status", Type: "string", Description: "Filter tasks by status: pending, in_progress, completed, failed, blocked, or all (default: pending)"},
			{Name: "limit", Type: "integer", Description: "Maximum number of tasks to return (default: 10)"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "task_queue_read")
			result, err := handleTaskQueueRead(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "task_queue_read")
			}
			return postProcessResult(ctx, agent, "task_queue_read", args, result), nil
		},
	})

	// 32. task_queue_publish
	registry.Register(core.ToolConfig{
		Name:        "task_queue_publish",
		Description: "Update a task in the persistent queue. Used to claim tasks (set status to in_progress), record progress, mark completion, or publish failure. Optionally break a task into subtasks.",
		Parameters: []core.ParameterConfig{
			{Name: "task_id", Type: "string", Required: true, Description: "The task ID to update"},
			{Name: "status", Type: "string", Required: true, Description: "New status: in_progress, completed, failed, or blocked"},
			{Name: "result", Type: "string", Description: "Summary of work done or error message"},
			{Name: "subtasks", Type: "array", Description: "Break down into subtasks. Each item: {title, working_dir?, persona?, priority?}"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "task_queue_publish")
			result, err := handleTaskQueuePublish(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "task_queue_publish")
			}
			return postProcessResult(ctx, agent, "task_queue_publish", args, result), nil
		},
	})

	// 33. task_queue_add
	registry.Register(core.ToolConfig{
		Name:        "task_queue_add",
		Description: "Add a new task to the persistent queue. Tasks persist across sessions and can be processed by the Executive Assistant persona.",
		Parameters: []core.ParameterConfig{
			{Name: "title", Type: "string", Required: true, Description: "Task title (required)"},
			{Name: "description", Type: "string", Description: "Detailed task description"},
			{Name: "priority", Type: "string", Description: "Priority: high, medium, or low (default: medium)"},
			{Name: "working_dir", Type: "string", Description: "Working directory for the task (e.g., ~/projects/my-repo)"},
			{Name: "persona", Type: "string", Description: "Persona to use when executing this task (e.g., repo_orchestrator)"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, "task_queue_add")
			result, err := handleTaskQueueAdd(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, "task_queue_add")
			}
			return postProcessResult(ctx, agent, "task_queue_add", args, result), nil
		},
	})

	return registry
}

// ---------------------------------------------------------------------------
// richEventPublisher — enriches seed's lightweight tool_start/tool_end events
// with rich metadata (display_name, persona, is_subagent, subagent_type)
// and emits CLI tool_log output for tool execution.
// ---------------------------------------------------------------------------

// richEventPublisher wraps an *events.EventBus and enriches ALL events with
// agent event metadata (client_id, chat_id, user_id) so that the WebSocket
// event router can deliver them to the correct browser tab.  For tool_start
// and tool_end events it also adds display_name, persona, is_subagent, and
// subagent_type fields that the webui expects, and emits CLI tool_log output.
type richEventPublisher struct {
	bus   *events.EventBus
	agent *Agent
}

func newRichEventPublisher(bus *events.EventBus, agent *Agent) *richEventPublisher {
	return &richEventPublisher{bus: bus, agent: agent}
}

// Publish implements core.EventPublisher. All events are decorated with the
// agent's event metadata (client_id, chat_id, user_id) so that the WebSocket
// handler's shouldForwardEventToConnection can route them to the correct
// browser connection.  Tool events receive additional enrichment (display_name,
// persona, is_subagent, subagent_type) and emit CLI tool_log output.
func (r *richEventPublisher) Publish(eventType string, data any) {
	// Decorate with agent event metadata for WebSocket routing.
	data = r.decorateWithMetadata(data)

	switch eventType {
	case core.EventTypeToolStart, core.EventTypeToolEnd:
		// Tool execution now happens entirely inside seed's ToolRegistry, which
		// doesn't go through Agent.callTool — so the only place we can keep
		// sprout's TotalToolCalls counter in sync is here, on tool_end.
		// SubagentResult.ToolCalls reads this counter; without the increment,
		// every subagent reports 0 tool calls and the orchestrator thinks
		// nothing happened.
		if eventType == core.EventTypeToolEnd && r.agent != nil && r.agent.state != nil {
			r.agent.state.IncrementTotalToolCalls()
		}
		enriched := r.enrichEventData(data, eventType)
		r.bus.Publish(eventType, enriched)
	default:
		r.bus.Publish(eventType, data)
	}
}

// decorateWithMetadata merges the agent's event metadata (client_id, chat_id,
// user_id) into the event payload. This ensures that events published by seed's
// core agent through the EventPublisher are properly routed to the originating
// browser tab via shouldForwardEventToConnection. Without this decoration,
// events without client_id/chat_id are silently dropped by the WebSocket
// forwarding logic.
func (r *richEventPublisher) decorateWithMetadata(data any) any {
	if r.agent == nil {
		return data
	}
	return r.agent.decorateEventPayload(data)
}

// enrichEventData adds rich metadata fields to a tool event payload.
// For tool_end, it also emits a CLI tool_log when streaming is disabled.
func (r *richEventPublisher) enrichEventData(data any, eventType string) any {
	if data == nil {
		return data
	}

	payload, ok := data.(map[string]interface{})
	if !ok {
		return data
	}

	// Extract tool_name (both event types have this)
	toolName, _ := payload["tool_name"].(string)

	if toolName == "" {
		return data
	}

	displayName := buildDisplayName(toolName, payload)
	isSubagent := isSubagentTool(toolName)
	var subagentType string
	if isSubagent {
		subagentType = func() string {
			if toolName == "run_subagent" {
				return "single"
			}
			if toolName == "run_parallel_subagents" {
				return "parallel"
			}
			return ""
		}()
	}
	persona := extractPersona(payload)

	// Enrich with rich fields
	payload["display_name"] = displayName
	if persona != "" {
		payload["persona"] = persona
	}
	if isSubagent {
		payload["is_subagent"] = true
		payload["subagent_type"] = subagentType
	}

	// Emit CLI tool_log for tool execution progress.
	// tool_start: "executing tool [ToolName args...]" (always, so the user sees it immediately)
	// tool_end: "executed [ToolName args...]" (only when not streaming, since streaming shows live progress)
	// Subagents: also emit — the subagent's streaming callback prefixes with [persona],
	// giving full visibility for CLI auditing. Parallel subagents include a task index.
	if r.agent != nil {
		if eventType == core.EventTypeToolStart {
			// Build a ToolCall from the event payload so we can use the rich formatToolCall formatter.
			arguments, _ := payload["arguments"].(string)
			tc := api.ToolCall{
				Function: api.ToolCallFunction{
					Name:      toolName,
					Arguments: arguments,
				},
			}
			r.agent.ToolLog("executing tool", formatToolCall(tc))
		} else if eventType == core.EventTypeToolEnd && !r.agent.IsStreamingEnabled() {
			r.agent.ToolLog("executed", displayName)
		}
	}
	return payload
}

// buildDisplayName constructs a human-readable tool name by appending the
// primary argument to the tool name. For example:
//   - read_file /path/to/file → "read_file /path/to/file"
//   - shell_command ls -la → "shell_command ls -la"
//   - run_subagent with prompt → "run_subagent [task]"
func buildDisplayName(toolName string, payload map[string]interface{}) string {
	switch toolName {
	case "read_file", "write_file", "edit_file", "write_structured_file", "patch_structured_file", "read_memory":
		if path, ok := payload["path"].(string); ok && path != "" {
			return fmt.Sprintf("%s %s", toolName, path)
		}
	case "shell_command":
		if cmd, ok := payload["command"].(string); ok && cmd != "" {
			// Truncate long commands for readability
			if len(cmd) > 80 {
				return fmt.Sprintf("%s %s...", toolName, cmd[:77])
			}
			return fmt.Sprintf("%s %s", toolName, cmd)
		}
	case "git":
		if op, ok := payload["operation"].(string); ok && op != "" {
			if args, ok2 := payload["args"].(string); ok2 && args != "" {
				return fmt.Sprintf("%s %s %s", toolName, op, args)
			}
			return fmt.Sprintf("%s %s", toolName, op)
		}
	case "commit":
		if msg, ok := payload["message"].(string); ok && msg != "" {
			if len(msg) > 80 {
				return fmt.Sprintf("%s %s...", toolName, msg[:77])
			}
			return fmt.Sprintf("%s %s", toolName, msg)
		}
		return toolName
	case "search_files":
		if pattern, ok := payload["search_pattern"].(string); ok && pattern != "" {
			return fmt.Sprintf("%s %s", toolName, pattern)
		}
	case "web_search", "semantic_search":
		if query, ok := payload["query"].(string); ok && query != "" {
			if len(query) > 80 {
				return fmt.Sprintf("%s %s...", toolName, query[:77])
			}
			return fmt.Sprintf("%s %s", toolName, query)
		}
	case "fetch_url", "browse_url":
		if url, ok := payload["url"].(string); ok && url != "" {
			// Truncate URLs for readability
			if len(url) > 80 {
				return fmt.Sprintf("%s %s...", toolName, url[:77])
			}
			return fmt.Sprintf("%s %s", toolName, url)
		}
	case "run_subagent":
		if prompt, ok := payload["prompt"].(string); ok && prompt != "" {
			if len(prompt) > 80 {
				return fmt.Sprintf("%s [task: %s...]", toolName, prompt[:77])
			}
			return fmt.Sprintf("%s [task: %s]", toolName, prompt)
		}
	case "run_parallel_subagents":
		if subagents, ok := payload["subagents"].([]interface{}); ok {
			return fmt.Sprintf("%s (%d subagents)", toolName, len(subagents))
		}
		return toolName
	case "ask_user":
		if question, ok := payload["question"].(string); ok && question != "" {
			if len(question) > 80 {
				return fmt.Sprintf("%s %s...", toolName, question[:77])
			}
			return fmt.Sprintf("%s %s", toolName, question)
		}
	case "TodoWrite":
		if todos, ok := payload["todos"].([]interface{}); ok {
			return fmt.Sprintf("%s (%d items)", toolName, len(todos))
		}
	case "embedding_index":
		if operation, ok := payload["operation"].(string); ok && operation != "" {
			return fmt.Sprintf("%s %s", toolName, operation)
		}
	case "activate_skill":
		if skillID, ok := payload["skill_id"].(string); ok && skillID != "" {
			return fmt.Sprintf("%s %s", toolName, skillID)
		}
	case "add_memory":
		if name, ok := payload["name"].(string); ok && name != "" {
			return fmt.Sprintf("%s %s", toolName, name)
		}
	case "delete_memory":
		if name, ok := payload["name"].(string); ok && name != "" {
			return fmt.Sprintf("%s %s", toolName, name)
		}
	case "analyze_ui_screenshot":
		if imgPath, ok := payload["image_path"].(string); ok && imgPath != "" {
			return fmt.Sprintf("%s %s", toolName, imgPath)
		}
	case "analyze_image_content":
		if imgPath, ok := payload["image_path"].(string); ok && imgPath != "" {
			return fmt.Sprintf("%s %s", toolName, imgPath)
		}
	}

	return toolName
}

// extractPersona extracts the persona from tool arguments if present.
func extractPersona(payload map[string]interface{}) string {
	if persona, ok := payload["persona"].(string); ok && persona != "" {
		return persona
	}
	return ""
}

// ---------------------------------------------------------------------------
// Post-processing helpers: inlined in handler closures (seed's PostExecuteHook
// only receives (name, result) — no agent, no args, no context).
// ---------------------------------------------------------------------------

// buildSecretSource constructs the source string used by the elevation gate
// to identify the origin of detected secrets.
func buildSecretSource(toolName string, args map[string]interface{}) string {
	switch toolName {
	case "shell_command":
		if cmd, ok := args["command"].(string); ok {
			if len(cmd) > 80 {
				return toolName + ": " + cmd[:77] + "..."
			}
			return toolName + ": " + cmd
		}
	case "read_file", "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		if path, ok := args["path"].(string); ok && path != "" {
			return toolName + ": " + path
		}
	case "search_files":
		if pattern, ok := args["search_pattern"].(string); ok && pattern != "" {
			return toolName + ": " + pattern
		}
	}
	return toolName
}

// formatTodoItemsForEvent converts a []tools.TodoItem slice into the
// []map[string]interface{} format expected by PublishTodoUpdate.
func formatTodoItemsForEvent(todos []tools.TodoItem) []map[string]interface{} {
	result := make([]map[string]interface{}, len(todos))
	for i, t := range todos {
		result[i] = map[string]interface{}{
			"id":      t.ID,
			"content": t.Content,
			"status":  t.Status,
		}
	}
	return result
}

// logToolExecution is a legacy helper that was used to print tool execution
// messages in non-streaming mode. Now that richEventPublisher emits
// ToolLog("executing tool", ...) on tool_start for all modes, this is a
// no-op to avoid duplicate output.
func logToolExecution(_ *Agent, _ string) {
}

// handleToolError wraps a handler error into a sanitized result string and
// returns it along with the original error. Returning a non-nil error ensures
// seed's circuit breaker failure tracking and success/error classification
// work correctly, while the result string is sanitized for secret safety
// and model context. It also prints [FAIL] or [⚠️ SECURITY CAUTION] lines
// to the terminal (routed through the streaming callback for subagents).
func handleToolError(agent *Agent, err error, toolName string) (string, error) {
	if err == nil {
		return "", nil
	}
	safeMsg := sanitizeToolFailureMessage(err.Error())

	// Security caution requires a special LLM verification signal.
	if strings.Contains(err.Error(), "security caution:") {
		if agent != nil {
			agent.PrintLine("")
			agent.PrintLine(fmt.Sprintf("[⚠️  SECURITY CAUTION - LLM VERIFICATION REQUIRED] %s", safeMsg))
			agent.PrintLine("")
		}
		return fmt.Sprintf("SECURITY_CAUTION_REQUIRED: %s", safeMsg), err
	}

	if agent != nil {
		agent.PrintLine("")
		agent.PrintLine(fmt.Sprintf("[FAIL] Tool '%s' failed: %s", toolName, safeMsg))
		agent.PrintLine("")
	}

	return fmt.Sprintf("Error: %s", safeMsg), err
}
// isLocalProvider returns true if the provider runs locally and never sends
// data outside the user's network. Secret redaction is skipped for these
// providers since there's no off-network leakage risk.
func isLocalProvider(agent *Agent) bool {
	if agent == nil {
		return false
	}
	ct := agent.GetProviderType()
	switch ct {
	case api.OllamaLocalClientType,
		api.OllamaClientType,      // "ollama" alias for ollama-local
		api.OllamaTurboClientType,
		api.LMStudioClientType,
		api.TestClientType,
		api.EditorClientType:
		return true
	}
	return false
}

// 1. Model-specific constraints (fetch_url truncation, analyze_image_content compaction)
// 2. Universal truncation (50K cap)
// 3. Secret redaction with elevation gate
// 4. Duplicate embedding check for write tools
// 5. TodoWrite event emission
// Returns the final result string to show to the LLM.
func postProcessResult(ctx context.Context, agent *Agent, toolName string, args map[string]interface{}, result string) string {
	if result == "" {
		return result
	}

	// 1. Model-specific constraints (constrainToolResultForModel handles fetch_url and analyze_image_content)
	result = constrainToolResultForModel(toolName, args, result)

	// 2. Universal truncation
	result = truncateToolResult(result)

	// 3. Secret redaction (only for sensitive tools, skip if local provider)
	if !isLocalProvider(agent) && isSecretSensitiveTool(toolName) && agent.security.GetOutputRedactor() != nil {
		redactResult := agent.security.GetOutputRedactor().RedactToolOutput(result, toolName, args)
		if len(redactResult.Secrets) > 0 {
			source := buildSecretSource(toolName, args)
			action, evalErr := agent.security.GetElevationGate().Evaluate(redactResult.Secrets, source)
			if evalErr != nil {
				if agent.debug {
					agent.debugLog("[security] elevation gate error: %v\n", evalErr)
				}
			}
			switch action {
			case security.SecretAllow:
				// keep original (already redacted by the redactor as fallback)
				if agent.debug {
					agent.debugLog("[security] user allowed %d secret(s) in %s\n", len(redactResult.Secrets), toolName)
				}
				result = redactResult.Content
			case security.SecretBlock:
				if agent.debug {
					agent.debugLog("[security] blocked %d secret(s) in %s\n", len(redactResult.Secrets), toolName)
				}
				return fmt.Sprintf("BLOCKED: detected secrets in output. Operation blocked. Found %d secret(s) — user chose to block.", len(redactResult.Secrets))
			default:
				// SecretRedact — redactResult.Content is already redacted
				if agent.debug {
					agent.debugLog("[security] redacted %d secret(s) from %s\n", len(redactResult.Secrets), toolName)
				}
				result = redactResult.Content
			}
		}
	}

	// 4. Duplicate embedding check for write tools
	if shouldCheckDuplicates(toolName, agent) {
		if path, ok := args["path"].(string); ok && path != "" {
			if note := runDuplicateCheck(ctx, agent, path); note != "" {
				result = result + note
			}
		}
	}

	// 5. TodoWrite event emission
	if toolName == "TodoWrite" {
		agent.PublishTodoUpdate(formatTodoItemsForEvent(agent.GetTodoManager().Read()))
	}

	return result
}

// ---------------------------------------------------------------------------
// Pre-execute hook: security classification + subagent nesting prevention
// ---------------------------------------------------------------------------

func newPreExecuteHook(agent *Agent) func(name string, args map[string]interface{}) error {
	if agent == nil {
		return nil
	}
	return func(name string, args map[string]interface{}) error {
		// 1. Depth-based subagent nesting prevention
		// Agents at or beyond the maximum nesting depth cannot spawn further subagents.
		// This prevents runaway agent chains while allowing configurable multi-level nesting.
		// ask_user is allowed for subagents because they share the event bus with the
		// primary agent and questions are routed through the same WebUI/CLI prompt mechanism.
		if !agent.CanSpawnSubagents() {
			if name == "run_subagent" || name == "run_parallel_subagents" {
				return agenterrors.NewSecurityError(
					fmt.Sprintf("SUBAGENT_RESTRICTION: Agent at depth %d cannot spawn subagents (max depth: %d). "+
						"This restriction prevents runaway agent chains and ensures proper task delegation. "+
						"If you need additional work done, please complete your current task and return "+
						"your results to the parent agent for further delegation.",
						agent.SubagentDepth(), agent.MaxSubagentDepth()), nil)
			}
		}

		// 2. Security classification
		secResult := tools.ClassifyToolCall(name, args)
		if !secResult.ShouldBlock && !secResult.ShouldPrompt {
			return nil // safe, no action needed
		}

		// Unsafe mode bypasses all security checks
		if agent.GetUnsafeMode() {
			if agent.debug {
				agent.debugLog("[UNLOCK] Unsafe mode: bypassing security validation for %s (risk: %s)\n", name, secResult.Risk)
			}
			return nil
		}

		isSubagent := agent.IsSubagent()

		// WebUI approval path
		if mgr := agent.GetSecurityApprovalMgr(); mgr != nil && agent.GetEventBus() != nil && !isSubagent && agent.HasActiveWebUIClients() {
			if agent.debug {
				agent.debugLog("[APPROVAL] Requesting security approval via webui for %s (risk: %s)\n", name, secResult.Risk)
			}
			extras := map[string]string{}
			if secResult.RiskType != "" {
				extras["risk_type"] = formatRiskType(secResult.RiskType)
			}
			switch name {
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
			if !mgr.RequestToolApproval(agent.GetEventBus(), agent.GetEventClientID(), agent.GetEventUserID(), name, secResult.Risk.String(), secResult.Reasoning, extras) {
				return agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", name, secResult.Reasoning), nil)
			}
			return nil
		}

		// CLI approval path
		agentConfig := agent.GetConfig()
		logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)
		canPrompt := logger != nil && logger.IsInteractive() && !isSubagent

		if canPrompt {
			prompt := buildSecurityPrompt(name, args, secResult)
			if !logger.AskForConfirmation(prompt, false, false) {
				return agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", name, secResult.Reasoning), nil)
			}
			return nil
		}

		// Non-interactive paths
		if secResult.ShouldBlock {
			return agenterrors.NewSecurityError(fmt.Sprintf("security block: %s — %s", name, secResult.Reasoning), nil)
		}

		if secResult.ShouldPrompt && !isSubagent {
			return agenterrors.NewSecurityError(
				fmt.Sprintf("security caution: %s — %s (requires LLM verification: confirm this action is safe, expected, and aligned with user goals before proceeding)",
					name, secResult.Reasoning), nil)
		}

		return nil
	}
}
