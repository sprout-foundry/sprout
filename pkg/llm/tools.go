package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Tool represents a function that can be called by the LLM
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction defines the structure of a tool function
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  ToolParameters `json:"parameters"`
}

// ToolParameters defines the parameters schema for a tool
type ToolParameters struct {
	Type       string                  `json:"type"`
	Properties map[string]ToolProperty `json:"properties"`
	Required   []string                `json:"required"`
}

// ToolProperty defines a single parameter property
type ToolProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

// ToolCall represents a call to a tool made by the LLM
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction represents the function call details
type ToolCallFunction struct {
	Name       string          `json:"name"`
	Arguments  string          `json:"arguments,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

// ToolMessage represents a tool call message in the conversation
type ToolMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// GetAvailableTools returns the list of tools available to the LLM
func GetAvailableTools() []Tool {
	// Return only OpenAI-compatible tools, excluding old action-based tools
	// that can confuse LLMs expecting standard function calling format
	return []Tool{
		// {
		// 	Type: "function",
		// 	Function: ToolFunction{
		// 		Name:        "search_web",
		// 		Description: "Search the web for information to help answer questions or provide context",
		// 		Parameters: ToolParameters{
		// 			Type: "object",
		// 			Properties: map[string]ToolProperty{
		// 				"query": {
		// 					Type:        "string",
		// 					Description: "The search query to find relevant information",
		// 				},
		// 			},
		// 			Required: []string{"query"},
		// 		},
		// 	},
		// },
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "read_file",
				Description: "Read the contents of a file from the workspace",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProperty{
						"target_file": {
							Type:        "string",
							Description: "The path to the file to read",
						},
					},
					Required: []string{"target_file"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "delete_file",
				Description: "Delete a file from the workspace",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProperty{
						"target_file": {
							Type:        "string",
							Description: "The path of the file to delete",
						},
					},
					Required: []string{"target_file"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "replace_file_content",
				Description: "Replace the entire content of a file with new content",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProperty{
						"target_file": {
							Type:        "string",
							Description: "The path of the file to replace",
						},
						"new_content": {
							Type:        "string",
							Description: "The new content to write to the file",
						},
					},
					Required: []string{"target_file", "new_content"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "run_shell_command",
				Description: "Execute a shell command and return the output",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProperty{
						"command": {
							Type:        "string",
							Description: "The shell command to execute",
						},
					},
					Required: []string{"command"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "ask_user",
				Description: "Ask the user a question when more information is needed",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProperty{
						"question": {
							Type:        "string",
							Description: "The question to ask the user",
						},
					},
					Required: []string{"question"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "validate_file",
				Description: "Validate a file for syntax errors, compilation issues, or other problems",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProperty{
						"target_file": {
							Type:        "string",
							Description: "The path to the file to validate",
						},
						"validation_type": {
							Type:        "string",
							Description: "Type of validation to perform",
							Enum:        []string{"syntax", "compilation", "basic", "full"},
						},
					},
					Required: []string{"target_file"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "edit_file_section",
				Description: "Edit a specific section of a file efficiently (function, struct, etc.)",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProperty{
						"target_file": {
							Type:        "string",
							Description: "The path to the file to edit",
						},
						"instructions": {
							Type:        "string",
							Description: "Detailed instructions for what changes to make",
						},
						"target_section": {
							Type:        "string",
							Description: "Optional: specific function/struct name or section to target",
						},
					},
					Required: []string{"target_file", "instructions"},
				},
			},
		},

		{
			Type: "function",
			Function: ToolFunction{
				Name:        "fix_validation_issues",
				Description: "Attempt to automatically fix validation issues in a file",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProperty{
						"target_file": {
							Type:        "string",
							Description: "The path to the file with validation issues",
						},
						"error_description": {
							Type:        "string",
							Description: "Description of the validation errors to fix",
						},
					},
					Required: []string{"target_file", "error_description"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "preflight",
				Description: "Verify file exists/writable, clean git state, and required CLIs available",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProperty{
						"target_file": {
							Type:        "string",
							Description: "Optional target file to check for existence and writability",
						},
					},
					Required: []string{},
				},
			},
		},
	}
}

// ParseToolCalls extracts tool calls from an LLM response
func ParseToolCalls(response string) ([]ToolCall, error) {
	// Try to parse the response as a tool message
	var toolMessage ToolMessage
	if err := json.Unmarshal([]byte(response), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
		toolMessage.ToolCalls = normalizeToolCallArgs(toolMessage.ToolCalls)
		return toolMessage.ToolCalls, nil
	}

	// Try to extract from JSON code blocks
	if start := strings.Index(response, "```json"); start >= 0 {
		start += 7 // Skip "```json"
		if end := strings.Index(response[start:], "```"); end > 0 {
			jsonStr := strings.TrimSpace(response[start : start+end])
			// Try to parse with object arguments first (common LLM variation)
			if toolCalls := parseObjectArgsToolCalls(jsonStr); len(toolCalls) > 0 {
				toolCalls = normalizeToolCallArgs(toolCalls)
				return toolCalls, nil
			}

			// Fall back to standard format
			if err := json.Unmarshal([]byte(jsonStr), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
				toolMessage.ToolCalls = normalizeToolCallArgs(toolMessage.ToolCalls)
				return toolMessage.ToolCalls, nil
			}
		}
	}

	// Look for standalone JSON objects containing tool_calls
	if strings.Contains(response, "tool_calls") {
		// First, try to find JSON wrapped in markdown code blocks
		if strings.Contains(response, "```json") && strings.Contains(response, "```") {
			// Extract JSON from markdown code block
			start := strings.Index(response, "```json")
			if start >= 0 {
				start += 7 // Skip "```json"
				end := strings.Index(response[start:], "```")
				if end >= 0 {
					jsonStr := strings.TrimSpace(response[start : start+end])
					// Try to parse with object arguments first (common LLM variation)
					if toolCalls := parseObjectArgsToolCalls(jsonStr); len(toolCalls) > 0 {
						toolCalls = normalizeToolCallArgs(toolCalls)
						return toolCalls, nil
					}

					// Fall back to standard format
					if err := json.Unmarshal([]byte(jsonStr), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
						toolMessage.ToolCalls = normalizeToolCallArgs(toolMessage.ToolCalls)
						return toolMessage.ToolCalls, nil
					}

					// Try to parse simplified tool call format from markdown code blocks too
					if toolCalls := parseSimplifiedToolCalls(jsonStr); len(toolCalls) > 0 {
						toolCalls = normalizeToolCallArgs(toolCalls)
						return toolCalls, nil
					}

					// Try to parse tool calls with object arguments (common LLM variation)
					if toolCalls := parseObjectArgsToolCalls(jsonStr); len(toolCalls) > 0 {
						toolCalls = normalizeToolCallArgs(toolCalls)
						return toolCalls, nil
					}
				}
			}
		}

		// Also try generic fenced blocks without language
		if strings.Contains(response, "```") {
			idx := 0
			for idx < len(response) {
				start := strings.Index(response[idx:], "```")
				if start == -1 {
					break
				}
				start += idx + 3
				end := strings.Index(response[start:], "```")
				if end == -1 {
					break
				}
				block := strings.TrimSpace(response[start : start+end])
				if strings.Contains(block, "tool_calls") {
					if toolCalls := parseObjectArgsToolCalls(block); len(toolCalls) > 0 {
						toolCalls = normalizeToolCallArgs(toolCalls)
						return toolCalls, nil
					}
					if err := json.Unmarshal([]byte(block), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
						toolMessage.ToolCalls = normalizeToolCallArgs(toolMessage.ToolCalls)
						return toolMessage.ToolCalls, nil
					}
				}
				idx = start + end + 3
			}
		}
	}

	// Fallback: Find JSON object boundaries anywhere in the response
	start := strings.Index(response, "{")
	if start >= 0 {
		depth := 0
		for i := start; i < len(response); i++ {
			if response[i] == '{' {
				depth++
			} else if response[i] == '}' {
				depth--
				if depth == 0 {
					jsonStr := response[start : i+1]
					// Try to parse with object arguments first (common LLM variation)
					if toolCalls := parseObjectArgsToolCalls(jsonStr); len(toolCalls) > 0 {
						toolCalls = normalizeToolCallArgs(toolCalls)
						return toolCalls, nil
					}

					// Fall back to standard format
					if err := json.Unmarshal([]byte(jsonStr), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
						toolMessage.ToolCalls = normalizeToolCallArgs(toolMessage.ToolCalls)
						return toolMessage.ToolCalls, nil
					}

					// Try to parse simplified tool calls
					if toolCalls := parseSimplifiedToolCalls(jsonStr); len(toolCalls) > 0 {
						toolCalls = normalizeToolCallArgs(toolCalls)
						return toolCalls, nil
					}
				}
			}
		}
	}

	return []ToolCall{}, fmt.Errorf("no tool calls found")
}

// normalizeToolCallArgs converts string-encoded arguments to canonical JSON objects
func normalizeToolCallArgs(calls []ToolCall) []ToolCall {
	for i, tc := range calls {
		args := strings.TrimSpace(tc.Function.Arguments)
		if args == "" {
			continue
		}
		var obj map[string]any
		if json.Unmarshal([]byte(args), &obj) == nil {
			if b, err := json.Marshal(obj); err == nil {
				calls[i].Function.Arguments = string(b)
			}
		}
	}
	return calls
}

// parseSimplifiedToolCalls handles simplified tool call formats that don't follow full OpenAI spec
func parseSimplifiedToolCalls(jsonStr string) []ToolCall {
	var simplified struct {
		ToolCalls []struct {
			Type     string `json:"type"`
			FilePath string `json:"file_path,omitempty"`
			Command  string `json:"command,omitempty"`
			Question string `json:"question,omitempty"`
			Action   string `json:"action,omitempty"`
			Query    string `json:"query,omitempty"`
		} `json:"tool_calls"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &simplified); err != nil {
		return nil
	}

	var toolCalls []ToolCall
	for i, call := range simplified.ToolCalls {
		var toolCall ToolCall
		toolCall.ID = fmt.Sprintf("simplified_%d", i)
		toolCall.Type = "function"

		// Map simplified format to function call
		switch call.Type {
		case "read_file":
			toolCall.Function.Name = "read_file"
			toolCall.Function.Arguments = fmt.Sprintf(`{"target_file":"%s"}`, call.FilePath)
		case "run_shell_command":
			toolCall.Function.Name = "run_shell_command"
			toolCall.Function.Arguments = fmt.Sprintf(`{"command":"%s"}`, call.Command)
		case "ask_user":
			toolCall.Function.Name = "ask_user"
			toolCall.Function.Arguments = fmt.Sprintf(`{"question":"%s"}`, call.Question)
		default:
			// Try to use the type as function name and convert other fields to arguments
			toolCall.Function.Name = call.Type
			args := make(map[string]string)
			if call.FilePath != "" {
				args["target_file"] = call.FilePath
			}
			if call.Command != "" {
				args["command"] = call.Command
			}
			if call.Question != "" {
				args["question"] = call.Question
			}
			if call.Action != "" {
				args["action"] = call.Action
			}
			if call.Query != "" {
				args["query"] = call.Query
			}
			if len(args) > 0 {
				argsJson, _ := json.Marshal(args)
				toolCall.Function.Arguments = string(argsJson)
			}
		}

		toolCalls = append(toolCalls, toolCall)
	}

	return toolCalls
}

// parseObjectArgsToolCalls handles tool calls where arguments are provided as objects instead of JSON strings
func parseObjectArgsToolCalls(jsonStr string) []ToolCall {
	var objectArgs struct {
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string      `json:"name"`
				Arguments interface{} `json:"arguments"` // Can be string or object
			} `json:"function"`
		} `json:"tool_calls"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &objectArgs); err != nil {
		return nil
	}

	var toolCalls []ToolCall
	for _, call := range objectArgs.ToolCalls {
		var toolCall ToolCall
		toolCall.ID = call.ID
		toolCall.Type = call.Type

		// Convert arguments to JSON string if it's an object
		if argsObj, ok := call.Function.Arguments.(map[string]interface{}); ok {
			// Arguments is an object, convert to JSON string
			argsJson, err := json.Marshal(argsObj)
			if err != nil {
				continue
			}
			toolCall.Function.Arguments = string(argsJson)
		} else if argsStr, ok := call.Function.Arguments.(string); ok {
			// Arguments is already a string, use as-is
			toolCall.Function.Arguments = argsStr
		} else {
			// Try to convert to string as fallback
			argsJson, err := json.Marshal(call.Function.Arguments)
			if err != nil {
				continue
			}
			toolCall.Function.Arguments = string(argsJson)
		}

		toolCall.Function.Name = call.Function.Name
		toolCalls = append(toolCalls, toolCall)
	}

	return toolCalls
}

// GetStandardToolDescriptions returns the standard tool descriptions used across the system
func GetStandardToolDescriptions() string {
	return `Available tools:
- read_file: {"target_file": "path/to/file"} - Read a file to understand its content
- edit_file_section: {"target_file": "path/to/file", "old_text": "text to replace", "new_text": "replacement text"} - Edit a specific part of a file
- run_shell_command: {"command": "shell command"} - Run shell commands for diagnostics or testing
- validate_file: {"target_file": "path/to/file"} - Check Go syntax of a file
- ask_user: {"question": "question text"} - Ask the user a question when more information is needed`
}

// GetSystemMessageForAnalysis returns a system message for analysis-focused LLM interactions
func GetSystemMessageForAnalysis() string {
	return fmt.Sprintf(`You are an expert code analyst and software developer. Use available tools to gather grounded evidence before providing analysis.

%s

WORKFLOW FOR ANALYSIS:
1. Use run_shell_command with "find . -name '*.go' | head -20" or similar to understand the project structure
2. Use run_shell_command with "grep -r 'pattern' --include='*.go'" to find relevant files
3. Use read_file to examine specific files that need analysis
4. Use run_shell_command for system-level information

AFTER gathering evidence with tools, provide your analysis based on the actual codebase content.`, FormatToolsForPrompt())
}

// GetSystemMessageForEditing returns a system message for code editing workflows
func GetSystemMessageForEditing() string {
	return fmt.Sprintf(`You are an expert software developer. Use tools to understand the codebase before making targeted edits.

%s

WORKFLOW FOR EDITING:
1. Use run_shell_command to understand the project structure (find, ls, grep)
2. Use read_file to examine files before editing
3. Make minimal, targeted changes
4. Use validate_file after changes to ensure correctness

When making edits, be precise and only change what is specifically requested.`, FormatToolsForPrompt())
}

// GetSystemMessageForStepExecution returns a system message for granular step execution
func GetSystemMessageForStepExecution() string {
	return fmt.Sprintf(`You are executing a specific step in a larger development task. Use available tools to complete this step accurately.

%s

WORKFLOW FOR STEP EXECUTION:
- Use run_shell_command with "find . -name '*.go' | head -20" to understand the project structure
- Use run_shell_command with "grep -r 'pattern' --include='*.go'" to find relevant files
- Use read_file to examine files that need to be modified
- Use run_shell_command for system operations or file system checks
- Use validate_file after making changes to ensure they are correct

Focus on completing the specific step assigned to you. Do not implement additional features or other steps.`, FormatToolsForPrompt())
}

// GetSystemMessageForInformational returns a system message for simple informational queries
func GetSystemMessageForInformational() string {
	return fmt.Sprintf(`You are a helpful assistant that answers questions by using available tools. Always use tools to gather information directly.

%s

For simple questions, use the appropriate tools immediately:
- "What files are in the current directory?" → run_shell_command with "ls -la"
- "Show me the content of main.go" → read_file
- "What are the available commands?" → run_shell_command with "ls -la"

Answer questions directly using tool outputs. Do not generate code or create todos.`, FormatToolsForPrompt())
}

// FormatToolsForPrompt formats the available tools for inclusion in a system prompt
// This is used for LLMs that don't support native tool calling
func FormatToolsForPrompt() string {
	return `CRITICAL: YOU MUST EMIT TOOL CALLS IN STRICT JSON FORMAT

IMPORTANT: When you need to use tools, output ONLY a JSON object. Do NOT include any text before or after the JSON.

TOOL CALL FORMAT (MANDATORY):
{
  "tool_calls": [
    {
      "id": "call_1",
      "type": "function",
      "function": {
        "name": "tool_name",
        "arguments": "{\"param\": \"value\", \"param2\": \"value2\"}"
      }
    }
  ]
}

STRICT RULES:
🚫 NEVER mix tool calls with explanatory text
🚫 NEVER output prose when making tool calls
🚫 ONLY emit the JSON object when using tools
✅ Use read_file BEFORE editing any file
✅ Use run_shell_command (find, grep, ls) to discover files
✅ Use validate_file after making changes
✅ Keep tool calls under 300 tokens total

` + GetStandardToolDescriptions() + `

WORKFLOW:
1. If you need to read/modify files, use read_file first
2. Make changes with edit_file_section
3. Validate with validate_file
4. Use run_shell_command (find, grep, ls) to explore unknown areas
5. When you have all info needed, provide your final response WITHOUT tool calls`
}
