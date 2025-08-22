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
						"file_path": {
							Type:        "string",
							Description: "The path to the file to read",
						},
					},
					Required: []string{"file_path"},
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
						"file_path": {
							Type:        "string",
							Description: "The path to the file to validate",
						},
						"validation_type": {
							Type:        "string",
							Description: "Type of validation to perform",
							Enum:        []string{"syntax", "compilation", "basic", "full"},
						},
					},
					Required: []string{"file_path"},
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
						"file_path": {
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
					Required: []string{"file_path", "instructions"},
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
						"file_path": {
							Type:        "string",
							Description: "The path to the file with validation issues",
						},
						"error_description": {
							Type:        "string",
							Description: "Description of the validation errors to fix",
						},
					},
					Required: []string{"file_path", "error_description"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "workspace_context",
				Description: "Access workspace information: file tree, embeddings search, or keyword search across the codebase",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProperty{
						"action": {
							Type:        "string",
							Description: "One of: search_embeddings, search_keywords, load_tree, load_summary",
							Enum:        []string{"search_embeddings", "search_keywords", "load_tree", "load_summary"},
						},
						"query": {
							Type:        "string",
							Description: "Search terms for embeddings or keyword search (required for search actions)",
						},
					},
					Required: []string{"action"},
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
						"file_path": {
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
	var toolCalls []ToolCall

	// Try to parse the response as a tool message
	var toolMessage ToolMessage
	if err := json.Unmarshal([]byte(response), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
		return toolMessage.ToolCalls, nil
	}

	// Try to extract from JSON code blocks
	if start := strings.Index(response, "```json"); start >= 0 {
		start += 7 // Skip "```json"
		if end := strings.Index(response[start:], "```"); end > 0 {
			jsonStr := strings.TrimSpace(response[start : start+end])
			// Try to parse with object arguments first (common LLM variation)
			if toolCalls := parseObjectArgsToolCalls(jsonStr); len(toolCalls) > 0 {
				return toolCalls, nil
			}

			// Fall back to standard format
			if err := json.Unmarshal([]byte(jsonStr), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
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
						return toolCalls, nil
					}

					// Fall back to standard format
					if err := json.Unmarshal([]byte(jsonStr), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
						return toolMessage.ToolCalls, nil
					}

					// Try to parse simplified tool call format from markdown code blocks too
					if toolCalls := parseSimplifiedToolCalls(jsonStr); len(toolCalls) > 0 {
						return toolCalls, nil
					}

					// Try to parse tool calls with object arguments (common LLM variation)
					if toolCalls := parseObjectArgsToolCalls(jsonStr); len(toolCalls) > 0 {
						return toolCalls, nil
					}
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
							return toolCalls, nil
						}

						// Fall back to standard format
						if err := json.Unmarshal([]byte(jsonStr), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
							return toolMessage.ToolCalls, nil
						}

						// Try to parse simplified tool call format (for models that don't use full OpenAI format)
						if toolCalls := parseSimplifiedToolCalls(jsonStr); len(toolCalls) > 0 {
							return toolCalls, nil
						}

						// Try to parse tool calls with object arguments (common LLM variation)
						if toolCalls := parseObjectArgsToolCalls(jsonStr); len(toolCalls) > 0 {
							return toolCalls, nil
						}
						break
					}
				}
			}
		}
	}

	// If that fails, look for tool calls in the response text
	// This is a fallback for LLMs that don't support proper tool calling format
	// but can generate structured tool calls in their response
	return toolCalls, nil
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
			toolCall.Function.Arguments = fmt.Sprintf(`{"file_path":"%s"}`, call.FilePath)
		case "run_shell_command":
			toolCall.Function.Name = "run_shell_command"
			toolCall.Function.Arguments = fmt.Sprintf(`{"command":"%s"}`, call.Command)
		case "ask_user":
			toolCall.Function.Name = "ask_user"
			toolCall.Function.Arguments = fmt.Sprintf(`{"question":"%s"}`, call.Question)
		case "workspace_context":
			toolCall.Function.Name = "workspace_context"
			if call.Action == "search_keywords" {
				toolCall.Function.Arguments = fmt.Sprintf(`{"action":"search_keywords","query":"%s"}`, call.Query)
			} else {
				toolCall.Function.Arguments = fmt.Sprintf(`{"action":"%s"}`, call.Action)
			}
		default:
			// Try to use the type as function name and convert other fields to arguments
			toolCall.Function.Name = call.Type
			args := make(map[string]string)
			if call.FilePath != "" {
				args["file_path"] = call.FilePath
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

// convertObjectArgsToString converts tool calls with object arguments to string arguments
func convertObjectArgsToString(toolCalls []ToolCall) []ToolCall {
	// We need to use raw JSON parsing to detect object arguments
	// since ToolCallFunction.Arguments is defined as string
	type RawFunction struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}

	type RawToolCall struct {
		ID       string      `json:"id"`
		Type     string      `json:"type"`
		Function RawFunction `json:"function"`
	}

	type RawToolMessage struct {
		ToolCalls []RawToolCall `json:"tool_calls"`
	}

	// Convert to JSON and back to detect object arguments
	jsonData, err := json.Marshal(map[string]interface{}{"tool_calls": toolCalls})
	if err != nil {
		return nil
	}

	var rawMessage RawToolMessage
	if err := json.Unmarshal(jsonData, &rawMessage); err != nil {
		return nil
	}

	converted := false
	for i, call := range rawMessage.ToolCalls {
		// Try to unmarshal arguments as an object to see if it's not a string
		var argsObj map[string]interface{}
		if json.Unmarshal(call.Function.Arguments, &argsObj) == nil {
			// Arguments is an object, convert to JSON string
			argsJson, err := json.Marshal(argsObj)
			if err != nil {
				continue
			}
			toolCalls[i].Function.Arguments = string(argsJson)
			converted = true
		}
	}

	if converted {
		return toolCalls
	}
	return nil
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

// FormatToolsForPrompt formats the available tools for inclusion in a system prompt
// This is used for LLMs that don't support native tool calling
func FormatToolsForPrompt() string {
	return `CONTROL MESSAGE (≤300 tokens)
Example tool call:
{
  "tool_calls": [
    {"id": "call_1", "type": "function", "function": {"name": "tool_name", "arguments": "{\"param\":\"value\"}"}}
  ]
}

RULES:
- Emit TOOL_CALLS JSON only (no prose) until you have completed the task.
- If user mentions a file you don’t have: use read_file first
- Use edit_file_section for file changes
- Validate after edits; for docs-only, consider success without build/test
- Hard caps: workspace_context ≤2, shell ≤5; dedupe exact shell commands

AVAILABLE TOOLS:
- read_file {file_path}
- edit_file_section {file_path,instructions,target_section?}

- validate_file {file_path,validation_type?}
- workspace_context {action,query?}
- run_shell_command {command}
- ask_user {question}`
	// - search_web {query}`
}
