package agent

import (
	"context"
	"time"

	core "github.com/sprout-foundry/seed/core"
)

// registerTools registers all sprout tools with the seed core.ToolRegistry.
// Each registration includes parameter configs, timeouts, handler closures,
// and optional HandlerWithImages for multimodal tools.
func registerTools(registry *core.ToolRegistry, agent *Agent) {
	// ---------------------------------------------------------------
	// Register all 33 tools
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
			{Name: "skill_id", Type: "string", Required: true, Alternatives: []string{"skill", "id"}, Description: "The ID of the skill to activate (e.g., 'go-conventions', 'test-writing')"},
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
}
