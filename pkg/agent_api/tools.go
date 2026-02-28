package api

func GetToolDefinitions() []Tool {
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
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to file to read",
							"minLength":   1,
						},
						"view_range": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "integer"},
							"description": "Line range as [start, end] array (1-based)",
						},
					},
					"required":             []string{"path"},
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
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to file to write",
							"minLength":   1,
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "Content to write to file",
						},
					},
					"required":             []string{"path", "content"},
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
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to file to edit",
							"minLength":   1,
						},
						"old_str": map[string]interface{}{
							"type":        "string",
							"description": "Exact string to replace (must match exactly including whitespace)",
							"minLength":   1,
						},
						"new_str": map[string]interface{}{
							"type":        "string",
							"description": "New string to replace with",
						},
					},
					"required":             []string{"path", "old_str", "new_str"},
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
				Name:        "TodoWrite",
				Description: "Use this tool to create and manage a structured task list for your current coding session.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"todos": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"content": map[string]interface{}{
										"type":        "string",
										"description": "The task description",
									},
									"status": map[string]interface{}{
										"type":        "string",
										"description": "Current status of the task",
										"enum":        []string{"pending", "in_progress", "completed"},
									},
									"priority": map[string]interface{}{
										"type":        "string",
										"description": "Priority of the task",
										"enum":        []string{"high", "medium", "low"},
									},
									"activeForm": map[string]interface{}{
										"type":        "string",
										"description": "Active form for display",
									},
									"id": map[string]interface{}{
										"type":        "string",
										"description": "Task identifier",
									},
								},
								"required": []string{"content", "status"},
							},
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
				Name:        "TodoRead",
				Description: "Use this tool to read the current to-do list for the session.",
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
						"analysis_mode": map[string]interface{}{
							"type":        "string",
							"description": "Optional analysis mode override (e.g. frontend, general, text)",
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
				Name:        "run_subagent",
				Description: "Delegate a SINGLE implementation task to a subagent.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"prompt": map[string]interface{}{
							"type":        "string",
							"description": "The prompt/task for the subagent to execute (required)",
							"minLength":   1,
						},
						"persona": map[string]interface{}{
							"type":        "string",
							"description": "REQUIRED: Subagent persona - choose from: general, coder, refactor, debugger, tester, code_reviewer, researcher, web_scraper",
							"enum":        []string{"general", "coder", "refactor", "debugger", "tester", "code_reviewer", "researcher", "web_scraper"},
						},
						"context": map[string]interface{}{
							"type":        "string",
							"description": "Context from previous subagent work",
						},
						"files": map[string]interface{}{
							"type":        "string",
							"description": "Comma-separated list of relevant file paths",
						},
						"model": map[string]interface{}{
							"type":        "string",
							"description": "Optional: Override model for this subagent",
						},
						"provider": map[string]interface{}{
							"type":        "string",
							"description": "Optional: Override provider",
						},
					},
					"required":             []string{"prompt", "persona"},
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
				Description: "View change history of files across sessions",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of changes to return",
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
							"description": "Include actual file content changes in output",
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
							"description": "Revision ID to rollback",
						},
						"file_path": map[string]interface{}{
							"type":        "string",
							"description": "Optional: rollback only this specific file from the revision",
						},
						"confirm": map[string]interface{}{
							"type":        "boolean",
							"description": "Must be true to actually perform rollback",
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
							"description": "Server name (optional for list, required for call)",
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
