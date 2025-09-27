package api

// GetCompactToolDefinitions returns a more compact version of tool definitions
// to reduce token usage while maintaining functionality
func GetCompactToolDefinitions() []Tool {
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
							"type": "string",
						},
					},
					"required": []string{"command"},
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
				Description: "Read file contents (supports line ranges)",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"file_path": map[string]interface{}{
							"type": "string",
						},
						"start_line": map[string]interface{}{
							"type":    "integer",
							"minimum": 1,
						},
						"end_line": map[string]interface{}{
							"type":    "integer",
							"minimum": 1,
						},
					},
					"required": []string{"file_path"},
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
				Description: "Replace exact string in file",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"file_path": map[string]interface{}{
							"type": "string",
						},
						"old_string": map[string]interface{}{
							"type": "string",
						},
						"new_string": map[string]interface{}{
							"type": "string",
						},
					},
					"required": []string{"file_path", "old_string", "new_string"},
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
				Description: "Create/overwrite file",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"file_path": map[string]interface{}{
							"type": "string",
						},
						"content": map[string]interface{}{
							"type": "string",
						},
					},
					"required": []string{"file_path", "content"},
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
				Description: "Analyze UI/web screenshots",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"image_path": map[string]interface{}{
							"type": "string",
						},
					},
					"required": []string{"image_path"},
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
				Description: "Extract text/content from images",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"image_path": map[string]interface{}{
							"type": "string",
						},
						"analysis_prompt": map[string]interface{}{
							"type": "string",
						},
					},
					"required": []string{"image_path", "analysis_prompt"},
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
                    Description: "Create multiple tasks",
                    Parameters: map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                            "todos": map[string]interface{}{
                                "type": "array",
                                "items": map[string]interface{}{
                                    "type": "object",
                                    "properties": map[string]interface{}{
                                        "title":    map[string]interface{}{"type": "string"},
                                        "priority": map[string]interface{}{"type": "string"},
                                    },
                                    "required": []string{"title"},
                                },
                            },
                        },
                        "required": []string{"todos"},
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
				Description: "Update task status",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type": "string",
						},
                    "status": map[string]interface{}{
                        "type": "string",
                        "enum": []string{"pending", "in_progress", "completed", "cancelled"},
                    },
					},
					"required": []string{"id", "status"},
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
				Description: "View active tasks",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
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
				Description: "Minimal task view",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
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
				Name:        "auto_complete_todos",
				Description: "Auto-complete on success",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"context": map[string]interface{}{
							"type": "string",
							"enum": []string{"build_success", "test_success"},
						},
					},
					"required": []string{"context"},
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
				Description: "Remove completed tasks",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
	}
}
