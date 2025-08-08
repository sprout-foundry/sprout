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
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
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
	return []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "search_web",
				Description: "Search the web for information to help answer questions or provide context",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProperty{
						"query": {
							Type:        "string",
							Description: "The search query to find relevant information",
						},
					},
					Required: []string{"query"},
				},
			},
		},
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

	// If that fails, look for tool calls in the response text
	// This is a fallback for LLMs that don't support proper tool calling format
	// but can generate structured tool calls in their response
	return toolCalls, nil
}

// FormatToolsForPrompt formats the available tools for inclusion in a system prompt
// This is used for LLMs that don't support native tool calling
func FormatToolsForPrompt() string {
	return `IMPORTANT: You have access to the following tools. You MUST use these tools when needed - do not make assumptions or guesses.

**TOOL USAGE RULES:**
- If the user mentions a file but you don't have its contents, you MUST use read_file
- If you need current information or documentation, you MUST use search_web
- If you need to check system state or run commands, you MUST use run_shell_command
- If you need clarification from the user, you MUST use ask_user

**TOOL CALL FORMAT:**
When you need to use tools, respond with a JSON object in this EXACT format:

{
  "tool_calls": [
    {
      "id": "call_1",
      "type": "function",
      "function": {
        "name": "tool_name",
        "arguments": "{\"param\": \"value\"}"
      }
    }
  ]
}

**AVAILABLE TOOLS:**

1. **read_file** - REQUIRED when user mentions files you don't have
   - Parameters: {"file_path": "path/to/file"}
   - Use: ANY time a file is referenced but not provided
   - Example: User says "update main.go" but main.go content not shown

2. **search_web** - REQUIRED for current info or unknown topics
   - Parameters: {"query": "search terms"}
   - Use: When you need documentation, current info, or help with unfamiliar topics

3. **run_shell_command** - REQUIRED for system operations
   - Parameters: {"command": "shell command"}
   - Use: When you need to check files, run tests, or examine system state

4. **ask_user** - REQUIRED when instructions are unclear
   - Parameters: {"question": "your question"}
   - Use: When you need clarification or additional information

5. **validate_file** - ESSENTIAL for quality assurance
   - Parameters: {"file_path": "path/to/file", "validation_type": "syntax|compilation|basic|full"}
   - Use: After making changes to verify correctness and catch issues early

6. **edit_file_section** - EFFICIENT for targeted edits
   - Parameters: {"file_path": "path/to/file", "instructions": "what to change", "target_section": "optional function/struct name"}
   - Use: For precise edits to specific functions or sections

7. **fix_validation_issues** - AUTOMATED problem resolution
   - Parameters: {"file_path": "path/to/file", "error_description": "description of the issue"}
   - Use: When validation finds issues that can be automatically resolved

**WORKFLOW BEST PRACTICES:**
- After editing files, ALWAYS use validate_file to check for issues
- Use edit_file_section for targeted changes rather than rewriting entire files
- When validation fails, try fix_validation_issues before manual intervention
- Use run_shell_command for build/test verification and dependency checks

**CRITICAL:** Do NOT proceed without necessary information. Use tools first, then provide your response.`
}
