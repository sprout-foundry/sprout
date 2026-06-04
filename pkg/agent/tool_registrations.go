package agent

import "time"

// newDefaultToolRegistry creates the registry with all tool configurations
func newDefaultToolRegistry() *ToolRegistry {
	registry := &ToolRegistry{
		tools: make(map[string]ToolConfig),
	}

	// Register shell_command tool. Interactive=true because the handler
	// streams subprocess stdout/stderr live to the user's terminal via
	// io.MultiWriter (see pkg/agent_tools/shell_native.go:76). The
	// activity-indicator spinner would interleave with that output.
	registry.RegisterTool(ToolConfig{
		Name:        "shell_command",
		Description: "Execute a shell command. Supports background execution (background=true) and checking accumulated output of a background session (check_background=session_id) and stopping a background session (stop_background=session_id)",
		Parameters: []ParameterConfig{
			{"command", "string", false, []string{"cmd"}, "The shell command to execute (required unless check_background or stop_background is provided)"},
			{"background", "boolean", false, []string{}, "Run command in background and return immediately with session_id (default: false)"},
			{"check_background", "string", false, []string{}, "Session ID of a background session to check (returns accumulated output)"},
			{"stop_background", "string", false, []string{}, "Session ID of a background session to stop/terminate"},
		},
		Handler:     handleShellCommand,
		Timeout:     2 * time.Minute,
		Interactive: true,
	})

	// Register git tool - handles operations that modify repository state or require network access
	registry.RegisterTool(ToolConfig{
		Name:        "git",
		Description: "Execute git operations that modify repository state or require network access. All destructive operations require user approval. Commit operations should use the /commit slash command for the interactive commit flow. For read-only operations (status, log, diff, branch, show), use shell_command instead.",
		Parameters: []ParameterConfig{
			{"operation", "string", true, []string{"op"}, "Git operation type: commit, push, pull, fetch, add, rm, mv, reset, rebase, merge, checkout, branch_delete, tag, clean, stash, am, apply, cherry_pick, revert, restore"},
			{"args", "string", false, []string{}, "Arguments to pass to the git command (optional). For pull: --rebase, --ff-only, remote/branch. For fetch: --all, --prune, remote. For restore: --staged, pathspec."},
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
			{"notes", "string", false, []string{"context", "extra_context"}, "Optional notes/context to integrate into the auto-generated commit message. Use this to provide context about why the changes were made, what task they relate to, or any other information that should be captured in the commit. These notes are combined with the diff analysis to produce a better commit message. Ignored if 'message' parameter is provided."},
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
		Handler:         handleReadFile,
		HandlerImages:   handleReadFileWithImages,
		SafeForParallel: true,
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

	// list_changes - Single read tool for the change tracker. Covers
	// manifest listing, per-file diffs (include_diff), activity-block
	// summaries (group_by="block"), and persisted history merging
	// (include_persisted). Replaces the four pre-consolidation tools:
	// list_changes, show_my_change, summarize_my_session, my_recent_changes.
	registry.RegisterTool(ToolConfig{
		Name: "list_changes",
		Description: "List files you've created, modified, or deleted this session. Returns `{revision_id, files: [{path, op, tool, timestamp, recoverable}]}`. `op` is \"create\"/\"edit\"/\"delete\"/\"bulk\". Bulk entries (rollups from `git checkout .` or build commands) carry `bulk_count` + `bulk_items` (path+op summaries).\n\n**Output-shape args** (all optional):\n• `include_diff` (bool) — adds a `diff` field with unified pre-session vs current diff per non-bulk entry. Use for \"what did you change in foo.go?\" without re-reading.\n• `group_by=\"block\"` — replaces `files` with `blocks: [{started_at, ended_at, tools, files}]` grouped by 30-second activity windows. Use for \"summarize what you've been doing\".\n• `include_persisted` (bool) — merges hot+warm persistent-history records so the timeline spans previous sessions. Items get `source`, `revision_id`, `tier`.\n\n**Use it**: before declaring a task complete, when drafting commit messages, when the user asks what changed, and for cross-session reasoning.\n\nShell commands (sed, mv, rm, tee, …) are tracked via a workspace-walk diff around every `shell_command`. Files outside the workspace, binaries, and files >1 MiB are reported with `recoverable: false`.",
		Parameters: []ParameterConfig{
			{"since", "string", false, []string{}, "Optional cutoff: RFC3339 timestamp or duration (2d, 12h, 30m). Only changes at/after this time."},
			{"tool", "string", false, []string{}, "Optional tool name filter (e.g. write_file, edit_file, shell_command)."},
			{"path_pattern", "string", false, []string{}, "Optional path glob filter (e.g. pkg/auth/*.go)."},
			{"include_diff", "boolean", false, []string{}, "When true, populate a per-file unified diff in each entry's `diff` field."},
			{"group_by", "string", false, []string{}, "Set to \"block\" to return an activity-block summary instead of the files array."},
			{"include_persisted", "boolean", false, []string{}, "When true, merge in change records from the persistent history (hot+warm tiers)."},
		},
		Handler: handleListChanges,
	})

	// revert_my_changes - Bulk undo by scope (all or since). The
	// previous file= scope was removed; use recover_file(scope=
	// "session_start") for single-file pre-session restores.
	registry.RegisterTool(ToolConfig{
		Name: "revert_my_changes",
		Description: "Bulk-undo YOUR session edits using ChangeTracker originals — does NOT touch git or other agents' / user's in-progress work.\n\n**Scope** (pick one):\n• `scope=\"all\"` (default) — every file the tracker recorded this session.\n• `since=<RFC3339 timestamp OR duration>` (e.g. \"30m\", \"2h\", \"2026-05-27T10:00:00Z\") — changes at/after that time.\n\nPer file: edits → write originals back; deletes → un-delete; creates → remove the file. Returns `{restored, failed, summary, entries: [{path, action, ok, message}]}`.\n\nFor SINGLE-file recovery use `recover_file` (`scope=\"session_start\"` for pre-session, `scope=\"latest\"` for last edit).\n\n**Prefer this over `git checkout`/`git reset`** — git wipes EVERYTHING including the user's uncommitted work; this only touches files YOU edited.",
		Parameters: []ParameterConfig{
			{"scope", "string", false, []string{}, "\"all\" to revert every change this session. Default when no other filter is provided."},
			{"since", "string", false, []string{}, "RFC3339 timestamp or duration (30m, 2h, 2d) — revert all changes recorded at/after this moment."},
		},
		Handler: handleRevertMyChanges,
	})

	// recover_file - Single-file recovery. Replaces both the historical
	// recover_file tool and the standalone recover_bulk via the
	// `scope` parameter.
	registry.RegisterTool(ToolConfig{
		Name: "recover_file",
		Description: "Restore one file (or one bulk entry's worth of files) from the ChangeTracker's session buffer. Works for any tool's edits — write_file, edit_file, or shell_command (rm, sed -i, mv, `git checkout .`).\n\n**`scope`**:\n• `\"latest\"` (default) — restore to the state immediately before the most recent tracked change for `path`. Undoes one specific edit.\n• `\"session_start\"` — restore to the state before the agent first touched this file this session. Use when the file went through multiple edits.\n• `\"bulk\"` — treat `path` as the bulk entry's `path` from list_changes (e.g. \"git checkout .\"). Restores every packed file. Use to undo a high-volume destructive command.\n\n**Per-op (single-file scopes)**: edit/modified → write originals back; delete → un-delete; create → remove the file.\n\nWhen scope is `\"latest\"` or `\"session_start\"` and `path` was packed into a bulk entry, recover_file finds it inside the bulk and restores just that one file.\n\n**Returns**: single-file scopes → `{recovered, path, action, message}`. Bulk scope → `{found, bulk_path, restored, failed, summary, entries[]}`.\n\n**Safety**: only files the tracker recorded can be recovered — call list_changes first. Files with `recoverable: false` (binary, >1 MiB, outside workspace) cannot be restored. Bulk entries recorded as count-only (memory cap exceeded) cannot be bulk-recovered.",
		Parameters: []ParameterConfig{
			{"path", "string", true, []string{"file_path", "bulk_path"}, "Absolute or relative path to the file to recover. For scope=\"bulk\", use the bulk entry's `path` field from list_changes."},
			{"scope", "string", false, []string{}, "\"latest\" (default), \"session_start\", or \"bulk\". See description for semantics."},
		},
		Handler: handleRecoverFile,
	})

	// ask_user - Ask user a question and wait for response
	registry.RegisterTool(ToolConfig{
		Name:        "ask_user",
		Description: "Ask the user a question and wait for their response. Use this when you need clarification, a decision, or any input that cannot be determined from context alone.\n\n**Prefer `options` when the answer is one of a small set of choices** (Yes/No, A/B/C, file paths to confirm). The WebUI renders them as buttons; the CLI renders a numbered list. The returned value is the option's `value` (falling back to `label`), so choose machine-friendly `value` strings.\n\nSet `multi_select: true` for checkbox-style selection (response is comma-joined values). Set `default` to the option `value` (or freeform string) that should be pre-selected.",
		Parameters: []ParameterConfig{
			{"question", "string", true, []string{}, "The question to ask the user. Markdown is supported in the WebUI; the CLI renders plain text."},
			{"header", "string", false, []string{}, "Short label (≤ 40 chars) shown above the question — useful for categorizing the prompt (e.g., \"Auth method\", \"Approach\", \"Confirm delete\")."},
			{"options", "array", false, []string{}, "Optional array of selectable choices. Each entry is {label, value?, description?}. When omitted the user types a freeform response."},
			{"multi_select", "boolean", false, []string{}, "When true, the user may pick multiple options. Response is a comma-joined list of selected values. Default false."},
			{"default", "string", false, []string{}, "Default response. Should match an option's `value` (or `label`) when `options` is set; otherwise it's the freeform default when the user submits empty input."},
		},
		Handler:     handleAskUser,
		Timeout:     10 * time.Minute, // Match AskUserManager.DefaultAskUserTimeout
		Interactive: true,
	})

	// Register run_subagent tool - for multi-agent collaboration
	registry.RegisterTool(ToolConfig{
		Name:        "run_subagent",
		Description: "Delegate a SINGLE implementation task to a subagent. Runs an in-process agent with a focused task, waits for completion, and returns all output. Use this when: (1) Tasks must be done SEQUENTIALLY with dependencies between them, (2) You need to review results before deciding next steps, (3) Working on a single focused feature. For MULTIPLE INDEPENDENT tasks, use run_parallel_subagents instead for faster completion.\n\n**REQUIRED**: You MUST specify a persona parameter. Personas are configured from JSON defaults plus user config (for example: general, coder, refactor, debugger, tester, reviewer, researcher, web_scraper).\n\nSubagents use focused per-persona tool subsets from configuration for more deterministic behavior. NO TIMEOUT - runs until completion. Subagent provider and model are configured via config settings (subagent_provider and subagent_model).\n\n**IMPORTANT — interpreting the result**: The subagent's response is a JSON envelope. The `files_modified` array (also mirrored as a `[subagent files modified] … [/subagent files modified]` block at the top of `stdout`) is the AUTHORITATIVE list of files this subagent edited. Trust it: if a file does not appear in that list, the subagent did not change it. Do NOT revert, undo, or treat as out-of-scope any file in the working tree unless you have independently confirmed it is unrelated to the subagent's reported changes AND unrelated to your own prior edits in this session. When in doubt, ask the user before reverting.",
		Parameters: []ParameterConfig{
			{"prompt", "string", true, []string{}, "The prompt/task for the subagent to execute (required)"},
			{"persona", "string", true, []string{}, "REQUIRED: Subagent persona ID or alias (see /persona list)"},
			{"context", "string", false, []string{}, "Context from previous subagent work (files created, summaries, etc.)"},
			{"files", "string", false, []string{}, "Comma-separated list of relevant file paths (e.g., 'models/user.go,pkg/auth/jwt.go')"},
			{"working_dir", "string", false, []string{}, "Optional: directory to use as the subagent's working directory (must be within $HOME). Use this to spawn subagents operating in a different project directory."},
		},
		Handler: handleRunSubagent,
		Timeout: 30 * time.Minute,
	})

	// Register run_parallel_subagents tool - for concurrent multi-agent execution
	registry.RegisterTool(ToolConfig{
		Name:        "run_parallel_subagents",
		Description: "Execute 2+ INDEPENDENT subagent tasks CONCURRENTLY. Use when tasks have no dependencies on each other (e.g. researching different code areas, code + tests, analyzing multiple files). Waits for ALL to complete; returns per-ID `{stdout, stderr, exit_code, completed, timed_out}`.\n\nAccepts an array of strings: `[\"task 1\", \"task 2\", …]`. IDs auto-generate as task-1, task-2, etc.\n\nPersonas are NOT supported here (use `run_subagent` for per-task personas) — parallel subagents use the default subagent config. Provider/model from `subagent_provider` / `subagent_model`.\n\n**Result contract**: each subagent's `files_modified` (also mirrored as `[subagent files modified] … [/subagent files modified]` at the top of its `stdout`) is the AUTHORITATIVE record of what it edited. Do NOT revert files unless they appear in some subagent's list AND you've decided to undo that specific work. Multiple subagents may touch related files — check every result's manifest before concluding a file is unrelated.",
		Parameters: []ParameterConfig{
			{"subagents", "array", true, []string{}, "Array of task descriptions as strings: [\"task 1\", \"task 2\", \"task 3\"]. Auto-generates IDs like task-1, task-2, etc. Example: [\"Research X\", \"Implement Y\", \"Write tests for Z\"]"},
		},
		Handler: handleRunParallelSubagents,
		Timeout: 30 * time.Minute,
	})

	// Register search_files tool (cross-platform file content search)
	registry.RegisterTool(ToolConfig{
		Name:        "search_files",
		Description: "Search text pattern in files (cross-platform, ignores .git, node_modules, .sprout by default)",
		Parameters: []ParameterConfig{
			{"search_pattern", "string", true, []string{"pattern"}, "Text pattern or regex to search for"},
			{"directory", "string", false, []string{"root"}, "Directory to search (default: .)"},
			{"file_glob", "string", false, []string{"file_pattern", "glob"}, "Glob to limit files (e.g., *.go)"},
			{"case_sensitive", "boolean", false, []string{}, "Case sensitive search (default: false)"},
			{"max_results", "integer", false, []string{}, "Maximum results to return (default: 50)"},
			{"max_bytes", "integer", false, []string{}, "Maximum total bytes of matches to return (default: 102400)"},
		},
		Handler:         handleSearchFiles,
		SafeForParallel: true,
	})

	// Register request_clarification tool
	registry.RegisterTool(ToolConfig{
		Name:        "request_clarification",
		Description: "Request clarification from the parent agent when you encounter ambiguity or need additional context during execution. The parent will receive your question and can respond with guidance. This tool will block until a response is received or a timeout expires.",
		Parameters: []ParameterConfig{
			{"question", "string", true, nil, "What you need clarification on"},
		},
		Handler:     handleRequestClarification,
		Timeout:     DefaultClarificationTimeout + 5*time.Second,
		Interactive: true,
	})

	// Register respond_clarification tool
	registry.RegisterTool(ToolConfig{
		Name:        "respond_clarification",
		Description: "Respond to a clarification request from a delegate agent. Provide the request_id and your response to give the delegate additional context or guidance.",
		Parameters: []ParameterConfig{
			{"request_id", "string", true, nil, "The ID of the clarification request to respond to"},
			{"response", "string", true, nil, "Your clarification response"},
		},
		Handler: handleRespondClarification,
	})

	// Register repo_map tool
	registry.RegisterTool(ToolConfig{
		Name:        "repo_map",
		Description: "Generate a lightweight overview of the codebase showing file paths and top-level symbols (functions, types, interfaces, classes) with line numbers. Use this before reading files to identify which files and functions are relevant to your task, then use read_file with view_range to read only the sections you need. Output is limited to ~1024 tokens. Supports Go, TypeScript, JavaScript, Python, Rust, Java, and C files.",
		Parameters: []ParameterConfig{
			{"directory", "string", false, []string{}, "Directory to scan (default: workspace root)"},
		},
		Handler:         handleRepoMap,
		SafeForParallel: true,
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
			{"screenshot_path", "string", false, []string{}, "File path to save screenshot (required when action=screenshot, e.g. /tmp/sprout_examples/screenshot.png)"},
			{"session_id", "string", false, []string{}, "Reuse a persistent built-in browser session across multiple browse_url calls for iterative debugging"},
			{"persist_session", "boolean", false, []string{}, "Keep the browser page alive after this call and return a session_id in inspect output"},
			{"close_session", "boolean", false, []string{}, "Close the referenced persistent session after this call completes"},
			{"viewport_width", "integer", false, []string{}, "Browser viewport width in pixels (default: 1280)"},
			{"viewport_height", "integer", false, []string{}, "Browser viewport height in pixels (default: 720)"},
			{"user_agent", "string", false, []string{}, "Override the browser User-Agent string"},
			{"wait_for_selector", "string", false, []string{}, "Optional CSS selector to wait for before capturing output or running steps"},
			{"wait_timeout_ms", "integer", false, []string{}, "Optional selector wait timeout in milliseconds (default: 10000)"},
			{"steps", "array", false, []string{}, "Optional interaction steps. Each step object supports action=wait_for|wait_for_text|assert_selector|assert_text|assert_title|assert_url|click|hover|type|fill|press|sleep|scroll_to|navigate|reload|back|forward|eval plus selector/value/key/millis/script/expect fields as needed"},
			{"capture_selectors", "array", false, []string{}, "Optional list of CSS selectors to capture after interactions (text/html/value/basic attrs)"},
			{"capture_dom", "boolean", false, []string{}, "Include rendered DOM in inspect output"},
			{"capture_text", "boolean", false, []string{}, "Include visible text in inspect output"},
			{"include_console", "boolean", false, []string{}, "Include browser console messages and page errors in inspect output"},
			{"capture_network", "boolean", false, []string{}, "Include fetch/XHR network request summaries in inspect output"},
			{"capture_storage", "boolean", false, []string{}, "Include localStorage and sessionStorage snapshots in inspect output"},
			{"capture_cookies", "boolean", false, []string{}, "Include document.cookie-visible cookies in inspect output"},
			{"response_max_chars", "integer", false, []string{}, "Optional per-field truncation limit for inspect output"},
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
			{"viewport_width", "integer", false, []string{}, "Browser viewport width in pixels for HTML files (default: 1280)"},
			{"viewport_height", "integer", false, []string{}, "Browser viewport height in pixels for HTML files (default: 720)"},
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
			{"limit", "integer", false, []string{}, "Maximum number of entries to return (default 10)"},
			{"file_filter", "string", false, []string{"filename"}, "Filter by filename (partial match)"},
			{"since", "string", false, []string{}, "Only include changes after this ISO 8601 timestamp"},
			{"show_content", "boolean", false, []string{}, "Include content summaries for each change"},
		},
		Handler:         handleViewHistory,
		SafeForParallel: true,
	})

	registry.RegisterTool(ToolConfig{
		Name:        "rollback_changes",
		Description: "Preview or perform a rollback of tracked revisions",
		Parameters: []ParameterConfig{
			{"revision_id", "string", false, []string{}, "Revision ID to rollback (leave blank to list revisions)"},
			{"file_path", "string", false, []string{"filename"}, "Rollback only this file from the revision"},
			{"confirm", "boolean", false, []string{}, "Set to true to execute the rollback"},
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
			{"skill_id", "string", true, []string{"skill", "id"}, "The ID of the skill to activate (e.g., 'project-planning', 'browse-debugging')"},
		},
		Handler: handleActivateSkill,
	})

	// manage_memory - Consolidated memory CRUD + search.
	registry.RegisterTool(ToolConfig{
		Name:        "manage_memory",
		Description: "Persistent markdown memories at ~/.config/sprout/memories/ that auto-load into the system prompt every future conversation. Operations:\n\n• `add` — create/overwrite. Required: `name` (slug, e.g. 'git-safety'), `content` (markdown).\n• `read` — full content of one memory. Required: `name`.\n• `list` — every saved memory with first-line title.\n• `delete` — remove a memory file. Required: `name`.\n• `search` — semantic search via embedding similarity. Required: `query`. Optional: `threshold` (0.0–1.0, default 0.75), `top_k` (default 5).\n\n**Use `add`** when the user shares a durable preference or convention. **Use `search`/`read`** to recall prior notes. **Use `delete`** when the user says to forget something specific. Memories auto-load — explicit reads are only for verification.",
		Parameters: []ParameterConfig{
			{"operation", "string", true, []string{}, "One of: 'add', 'read', 'list', 'delete', 'search'."},
			{"name", "string", false, []string{"title", "memory"}, "Memory name (required for add/read/delete). Short descriptive slug, no .md extension."},
			{"content", "string", false, []string{}, "Markdown content (required for add)."},
			{"query", "string", false, []string{}, "Natural-language search query (required for search)."},
			{"threshold", "number", false, []string{}, "Search-only: minimum similarity 0.0–1.0 (default 0.75)."},
			{"top_k", "integer", false, []string{}, "Search-only: maximum results to return (default 5)."},
		},
		Handler: handleManageMemory,
	})

	// embedding_index is registered against the new ToolHandler registry in
	// pkg/agent_tools/all.go. Dual-dispatch (tool_executor_sequential.go)
	// reaches it via env.EmbeddingMgr — no legacy entry needed here.

	// Register manage_settings tool
	registry.RegisterTool(ToolConfig{
		Name:        "manage_settings",
		Description: "Manage application settings and provider credentials. Supports getting/setting configuration values, listing available providers, testing credential validity, describing settings (with valid values), and previewing changes before applying.",
		Parameters: []ParameterConfig{
			{"operation", "string", true, []string{}, "Operation to perform: 'get' (retrieve a setting), 'set' (update a setting), 'list_providers' (list available providers), 'test_credential' (test if a provider has valid credentials), 'describe' (describe a setting with valid values and current value), 'describe_all' (describe all settings), or 'preview' (preview a setting change without applying it)"},
			{"key", "string", false, []string{}, "Setting key (required for get/set/describe/preview operations). Examples: 'provider', 'model', 'reasoning_effort', 'disable_thinking', 'resource_directory', 'history_scope', 'ea_mode', 'subagent_provider', 'subagent_model', 'commit_provider', 'commit_model', 'review_provider', 'review_model'"},
			{"value", "string", false, []string{}, "Setting value (required for set/preview operations)"},
			{"provider", "string", false, []string{}, "Provider name (required for test_credential operation, optional for list_providers filter)"},
		},
		Handler: handleManageSettings,
	})

	// semantic_search is registered against the new ToolHandler registry in
	// pkg/agent_tools/all.go. See the embedding_index comment above.

	// task_queue - Consolidated persistent task-queue CRUD.
	registry.RegisterTool(ToolConfig{
		Name:        "task_queue",
		Description: "Persistent cross-session task queue at ~/.config/sprout/task_queue.json. Processed by the Executive Assistant persona.\n\n• `read` — list tasks sorted by priority. Optional: `status` (pending|in_progress|completed|failed|blocked|all, default \"pending\"), `limit` (default 10).\n• `add` — create. Required: `title`. Optional: `description`, `priority` (high|medium|low, default medium), `working_dir`, `persona`.\n• `publish` — update existing (claim, progress, completion, failure). Required: `task_id`, `status` (in_progress|completed|failed|blocked). Optional: `result`, `subtasks` (array of `{title, working_dir?, persona?, priority?}`).\n\nUse `read` for \"what's on my queue?\". Use `add` when the user wants a task remembered beyond this session. Use `publish` (EA persona) to claim or complete queued tasks.",
		Parameters: []ParameterConfig{
			{"operation", "string", true, []string{}, "One of: 'read', 'add', 'publish'."},
			// Read filters.
			{"status", "string", false, []string{}, "Read: status filter (pending|in_progress|completed|failed|blocked|all). Publish: new status to set."},
			{"limit", "integer", false, []string{}, "Read-only: maximum tasks to return (default 10)."},
			// Add fields.
			{"title", "string", false, []string{}, "Add-only: task title."},
			{"description", "string", false, []string{}, "Add-only: detailed description."},
			{"priority", "string", false, []string{}, "Add-only: high|medium|low (default medium)."},
			{"working_dir", "string", false, []string{}, "Add-only: working directory for the task."},
			{"persona", "string", false, []string{}, "Add-only: persona to use when executing."},
			// Publish fields.
			{"task_id", "string", false, []string{}, "Publish-only: task ID to update."},
			{"result", "string", false, []string{}, "Publish-only: summary of work done or error message."},
			{"subtasks", "array", false, []string{}, "Publish-only: break the task down. Each item: {title, working_dir?, persona?, priority?}."},
		},
		Handler: handleTaskQueue,
	})

	// Register run_automate tool — always runs as a background process.
	// ALWAYS requires user approval via the security classifier on FIRST call;
	// subsequent calls for the same workflow during the same session are
	// pre-approved (so retries after failure don't re-prompt the user).
	registry.RegisterTool(ToolConfig{
		Name: "run_automate",
		Description: "Start an automated workflow from the project's automate/ directory as a long-running background process. " +
			"Use list_automate_workflows first to discover what's available. " +
			"BEFORE calling this tool: (1) read the workflow JSON and its prompt_file/command_file (if any) so you understand what the workflow will actually do; (2) write a brief plain-language overview to the user describing what will happen (steps, providers/models, expected duration, side effects like commits); (3) ask the user to confirm starting. The first call in a session triggers an explicit user approval prompt; subsequent calls for the SAME workflow during the same chat session are auto-approved so you (the primary agent) can restart it after failure without re-asking. " +
			"After invocation, the tool returns immediately with a session_id. The workflow keeps running in the background. " +
			"To monitor it efficiently, use shell_command(check_background=<session_id>, wait_seconds=600) — this blocks (up to 10 min) until the workflow exits or the wait elapses, returning the snapshot. " +
			"Cadence guidance: first check ~60–90s after start (catches early failures), then use wait_seconds=600 in a loop while status stays 'running'. Surface meaningful updates to the user between waits — never poll silently for hours. If the user asks for status mid-run, do an immediate check with wait_seconds=0. " +
			"When status is 'exited', read the captured output, decide if the run succeeded, and resume control to either report results, retry the workflow (no re-approval needed), or take corrective action. " +
			"Returns JSON with workflow, description, command, session_id, and status fields.",
		Parameters: []ParameterConfig{
			{"workflow", "string", true, []string{"name", "workflow_name"}, "Workflow filename or name (with or without .json extension) from the automate/ directory"},
		},
		Handler: handleRunAutomate,
		Timeout: 0, // No timeout — autonomous workflows run until completion
	})

	// Register list_automate_workflows tool
	registry.RegisterTool(ToolConfig{
		Name:        "list_automate_workflows",
		Description: "List available automated workflows from the project's automate/ directory. Returns workflow filenames and descriptions. Use this before run_automate to show the user what's available.",
		Parameters:  []ParameterConfig{},
		Handler:     handleListAutomateWorkflows,
	})

	return registry
}
