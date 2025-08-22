// Package llm provides tool definitions and parsing utilities.
package llm

import (
	"encoding/json"
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
				Name:        "micro_edit",
				Description: "Apply a very small, targeted change to a file (limited sized diff).",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProperty{
						"file_path": {
							Type:        "string",
							Description: "The path to the file to edit",
						},
						"instructions": {
							Type:        "string",
							Description: "Minimal instructions for the small edit",
						},
					},
					Required: []string{},
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
						"dir": {
							Type:        "string",
							Description: "Optional directory root to scope load_tree (e.g., pkg/llm)",
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
	if strings.Contains(response, "```json") {
		start := strings.Index(response, "```json") + 7
		end := strings.Index(response[start:], "```")
		if end > 0 {
			jsonStr := strings.TrimSpace(response[start : start+end])
			if err := json.Unmarshal([]byte(jsonStr), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
				return toolMessage.ToolCalls, nil
			}
		}
	}

	// Look for standalone JSON objects containing tool_calls
	if strings.Contains(response, "tool_calls") {
		// Find JSON object boundaries
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
						if err := json.Unmarshal([]byte(jsonStr), &toolMessage); err == nil && len(toolMessage.ToolCalls) > 0 {
							return toolMessage.ToolCalls, nil
						}
						break
					}
				}
			}
		}
	}
	return toolCalls, nil
}

// FormatToolsForPrompt formats the available tools for inclusion in a system prompt
// This is used for LLMs that don't support native tool calling
func FormatToolsForPrompt() string {
	return `CONTROL MESSAGE (≤300 tokens)

PLAN:
{
  "action": "read_file|micro_edit|edit_file_section|validate_file|workspace_context|run_shell_command|ask_user|search_web",
  "target_file": "path/to/file (optional)",
  "instructions": "minimal instruction (optional)",
  "stop_when": "explicit completion criteria"
}

TOOL_CALLS:
{
  "tool_calls": [
    {"id": "call_1", "type": "function", "function": {"name": "tool_name", "arguments": "{\"param\":\"value\"}"}}
  ]
}

RULES:
- Emit PLAN then TOOL_CALLS JSON only (no prose) until stop_when is satisfied
- If user mentions a file you don’t have: use read_file first
- Prefer micro_edit for tiny changes; otherwise edit_file_section
- Validate after edits; for docs-only, consider success without build/test
- Hard caps: workspace_context ≤2, shell ≤5; dedupe exact shell commands

AVAILABLE TOOLS:
- read_file {file_path}
- edit_file_section {file_path,instructions,target_section?}
- micro_edit {file_path?,instructions?}
- validate_file {file_path,validation_type?}
- workspace_context {action,query?}
- run_shell_command {command}
- ask_user {question}`
	// - search_web {query}`
}
