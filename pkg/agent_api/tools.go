package api

func GetToolDefinitions() []Tool {
	// Added ask_user tool for user clarification interactions
	// This tool simply returns a prompt string that the agent can display to the user.
	// It does not perform I/O in this non‑interactive environment.
	// The implementation is defined in tools/ask_user.go.

	return []Tool{
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "shell_command",
				Description: "Execute shell commands",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{
							"type":        "string",
							"description": "Shell command to execute",
							"minLength":   1,
						},
					},
					"required":             []string{"command"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "read_file",
				Description: "Read file contents, optionally with line range",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"file_path": map[string]interface{}{
							"type":        "string",
							"description": "Path to file to read",
							"minLength":   1,
						},
						"start_line": map[string]interface{}{
							"type":        "integer",
							"description": "Optional: Start line number (1-based) for reading a specific range",
							"minimum":     1,
						},
						"end_line": map[string]interface{}{
							"type":        "integer",
							"description": "Optional: End line number (1-based) for reading a specific range",
							"minimum":     1,
						},
					},
					"required":             []string{"file_path"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "edit_file",
				Description: "Replace exact string match in file",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"file_path": map[string]interface{}{
							"type":        "string",
							"description": "Path to file to edit",
							"minLength":   1,
						},
						"old_string": map[string]interface{}{
							"type":        "string",
							"description": "Exact string to replace (must match exactly including whitespace)",
							"minLength":   1,
						},
						"new_string": map[string]interface{}{
							"type":        "string",
							"description": "New string to replace with",
						},
					},
					"required":             []string{"file_path", "old_string", "new_string"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "write_file",
				Description: "Create or overwrite file with content",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"file_path": map[string]interface{}{
							"type":        "string",
							"description": "Path to file to write",
							"minLength":   1,
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "Content to write to file",
						},
					},
					"required":             []string{"file_path", "content"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "add_todos",
				Description: "Create task list for multi-step work",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"todos": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"title": map[string]interface{}{
										"type":        "string",
										"description": "Brief title of the todo item",
										"minLength":   1,
									},
									"description": map[string]interface{}{
										"type":        "string",
										"description": "Optional detailed description",
									},
									"priority": map[string]interface{}{
										"type":        "string",
										"description": "Priority level for the todo item",
										"enum":        []string{"high", "medium", "low"},
										"default":     "medium",
									},
								},
								"required": []string{"title"},
							},
							"description": "Array of todo items to add (can be single item or multiple)",
							"minItems":    1,
						},
					},
					"required":             []string{"todos"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "update_todo_status",
				Description: "Update task status (pending/in_progress/completed/cancelled)",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "ID of the todo item to update",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "New status for the todo item",
							"enum":        []string{"pending", "in_progress", "completed", "cancelled"},
						},
					},
					"required":             []string{"id", "status"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "list_todos",
				Description: "Show current task list and progress",
				Parameters: map[string]interface{}{
					"type":                 "object",
					"properties":           map[string]interface{}{},
					"required":             []string{},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "get_active_todos_compact",
				Description: "Minimal task view (in-progress + pending summary)",
				Parameters: map[string]interface{}{
					"type":                 "object",
					"properties":           map[string]interface{}{},
					"required":             []string{},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "archive_completed",
				Description: "Remove completed/cancelled todos from active view",
				Parameters: map[string]interface{}{
					"type":                 "object",
					"properties":           map[string]interface{}{},
					"required":             []string{},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "analyze_ui_screenshot",
				Description: "Analyze UI/mockup images for implementation details",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"image_path": map[string]interface{}{
							"type":        "string",
							"description": "Path to UI screenshot, mockup, or design file",
						},
					},
					"required":             []string{"image_path"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "analyze_image_content",
				Description: "Extract text/code from images",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"image_path": map[string]interface{}{
							"type":        "string",
							"description": "Path to image file containing text, code, or general content",
						},
						"analysis_prompt": map[string]interface{}{
							"type":        "string",
							"description": "Optional specific prompt for content extraction (extract text, read code, analyze diagram, etc.)",
						},
					},
					"required":             []string{"image_path"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "web_search",
				Description: "Search web for relevant URLs",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Search query to find relevant web content",
							"minLength":   1,
							"maxLength":   500,
						},
					},
					"required":             []string{"query"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "fetch_url",
				Description: "Fetch and extract content from URL",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url": map[string]interface{}{
							"type":        "string",
							"description": "URL to fetch content from",
						},
					},
					"required":             []string{"url"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "search_files",
				Description: "Search text in files using patterns",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"search_pattern": map[string]interface{}{
							"type":        "string",
							"description": "Text or regular expression to search for",
							"minLength":   1,
						},
						"directory": map[string]interface{}{
							"type":        "string",
							"description": "Directory to search in (use '.' for current directory)",
							"default":     ".",
						},
						"file_glob": map[string]interface{}{
							"type":        "string",
							"description": "File glob to limit search (e.g., '*.go', '*.js')",
						},
						"case_sensitive": map[string]interface{}{
							"type":        "boolean",
							"description": "Whether the search should be case sensitive",
							"default":     false,
						},
						"max_results": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of results to return",
							"minimum":     1,
							"maximum":     1000,
							"default":     100,
						},
					},
					"required":             []string{"search_pattern"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "view_history",
				Description: "View change history of files across sessions to see what writes/edits have happened",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of changes to return (default: 10)",
							"minimum":     1,
							"maximum":     100,
							"default":     10,
						},
						"file_filter": map[string]interface{}{
							"type":        "string",
							"description": "Filter changes by filename (partial match)",
						},
						"since": map[string]interface{}{
							"type":        "string",
							"description": "Filter changes since this time (ISO 8601 format)",
						},
						"show_content": map[string]interface{}{
							"type":        "boolean",
							"description": "Include actual file content changes in output (default: false)",
							"default":     false,
						},
					},
					"required":             []string{},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "rollback_changes",
				Description: "Rollback previous changes by revision ID or specific file",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"revision_id": map[string]interface{}{
							"type":        "string",
							"description": "Revision ID to rollback (get from view_history). If not provided, shows available revisions",
						},
						"file_path": map[string]interface{}{
							"type":        "string",
							"description": "Optional: rollback only this specific file from the revision",
						},
						"confirm": map[string]interface{}{
							"type":        "boolean",
							"description": "Must be true to actually perform rollback (default: false for preview)",
							"default":     false,
						},
					},
					"required":             []string{},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "mcp_tools",
				Description: "Access MCP server tools",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"action": map[string]interface{}{
							"type":        "string",
							"description": "Action to perform: 'list' to discover tools, 'call' to execute",
							"enum":        []string{"list", "call"},
						},
						"server": map[string]interface{}{
							"type":        "string",
							"description": "Server name (optional for list, required for call). Examples: github-mcp, filesystem",
						},
						"tool": map[string]interface{}{
							"type":        "string",
							"description": "Tool name (required for call action)",
						},
						"arguments": map[string]interface{}{
							"type":        "object",
							"description": "Arguments for the tool (required for call action)",
						},
					},
					"required":             []string{"action"},
					"additionalProperties": false,
				},
			},
		},
	}
}
