package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// executeTool handles the execution of individual tool calls
func (a *Agent) executeTool(toolCall api.ToolCall) (string, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	// Log the tool call for debugging
	a.debugLog("🔧 Executing tool: %s with args: %v\n", toolCall.Function.Name, args)

	// Validate tool name and provide helpful error for common mistakes
	registry := GetToolRegistry()
	availableTools := registry.GetAvailableTools()
	validTools := make([]string, 0, len(availableTools)+1)
	validTools = append(validTools, availableTools...)
	validTools = append(validTools, "mcp_tools")
	sort.Strings(validTools)

	validToolSet := make(map[string]struct{}, len(validTools))
	for _, name := range validTools {
		validToolSet[name] = struct{}{}
	}

	isValidTool := false
	if _, exists := validToolSet[toolCall.Function.Name]; exists {
		isValidTool = true
	}
	isMCPTool := false

	// If not a standard tool, check if it's an MCP tool
	if !isValidTool && strings.HasPrefix(toolCall.Function.Name, "mcp_") {
		isMCPTool = a.isValidMCPTool(toolCall.Function.Name)
		isValidTool = isMCPTool
	}

	if !isValidTool {
		// Check for common misnamed tools and suggest corrections
		suggestion := a.suggestCorrectToolName(toolCall.Function.Name)
		if suggestion != "" {
			return "", fmt.Errorf("unknown tool '%s'. Did you mean '%s'? Valid tools are: %v",
				toolCall.Function.Name, suggestion, validTools)
		}
		return "", fmt.Errorf("unknown tool '%s'. Valid tools are: %v", toolCall.Function.Name, validTools)
	}

	// Use the tool registry for data-driven tool execution
	result, err := registry.ExecuteTool(context.Background(), toolCall.Function.Name, args, a)

	// If tool not found in registry, check for special cases
	if err != nil && strings.Contains(err.Error(), "unknown tool") {
		// Handle mcp_tools meta-tool
		if toolCall.Function.Name == "mcp_tools" {
			return a.handleMCPToolsCommand(args)
		}

		// Handle direct MCP tool calls
		if isMCPTool {
			return a.executeMCPTool(toolCall.Function.Name, args)
		}
		return "", fmt.Errorf("unknown tool: %s", toolCall.Function.Name)
	}

	return result, err
}

// extractJSONFromContent extracts complete JSON object by counting braces
func (a *Agent) extractJSONFromContent(content string) string {
	if !strings.HasPrefix(content, "{") {
		return ""
	}

	braceCount := 0
	inString := false
	escapeNext := false

	for i, char := range content {
		if escapeNext {
			escapeNext = false
			continue
		}

		if char == '\\' {
			escapeNext = true
			continue
		}

		if char == '"' {
			inString = !inString
			continue
		}

		if !inString {
			if char == '{' {
				braceCount++
			} else if char == '}' {
				braceCount--
				if braceCount == 0 {
					return content[:i+1]
				}
			}
		}
	}

	return ""
}

// extractIndividualToolCalls looks for individual tool calls by function name
func (a *Agent) extractIndividualToolCalls(content string) []api.ToolCall {
	var toolCalls []api.ToolCall

	// Look for patterns like: "function": {"name": "write_file", "arguments": "..."}
	functionNames := []string{
		"write_file",
		"read_file",
		"edit_file",
		"shell_command",
		"search_files",
		"find",
		"find_files",
		"validate_build",
		"add_todo",
		"add_todos",
		"update_todo_status",
		"list_todos",
		"get_active_todos_compact",
		"archive_completed",
		"auto_complete_todos",
		"web_search",
		"fetch_url",
		"analyze_ui_screenshot",
		"analyze_image_content",
		"view_history",
		"rollback_changes",
	}

	for _, funcName := range functionNames {
		pattern := fmt.Sprintf(`"function":\s*{\s*"name":\s*"%s"`, funcName)
		if matched, _ := regexp.MatchString(pattern, content); matched {
			// Try to extract the complete tool call
			if toolCall := a.extractSingleToolCall(content, funcName); toolCall != nil {
				toolCalls = append(toolCalls, *toolCall)
			}
		}
	}

	return toolCalls
}

// extractSingleToolCall extracts a single tool call for a specific function
func (a *Agent) extractSingleToolCall(content, functionName string) *api.ToolCall {
	// Look for the function pattern and try to extract the complete structure
	start := strings.Index(content, fmt.Sprintf(`"name": "%s"`, functionName))
	if start == -1 {
		return nil
	}

	// Find the start of the tool call object (work backwards to find opening brace)
	tcStart := strings.LastIndex(content[:start], `{"id":`)
	if tcStart == -1 {
		tcStart = strings.LastIndex(content[:start], `{`)
		if tcStart == -1 {
			return nil
		}
	}

	// Extract JSON from the tool call start position
	jsonStr := a.extractJSONFromContent(content[tcStart:])
	if jsonStr == "" {
		return nil
	}

	var toolCall api.ToolCall
	if err := json.Unmarshal([]byte(jsonStr), &toolCall); err == nil {
		// Generate ID if missing
		if toolCall.ID == "" {
			toolCall.ID = fmt.Sprintf("call_%d", time.Now().UnixNano())
		}
		if toolCall.Type == "" {
			toolCall.Type = "function"
		}
		return &toolCall
	}

	return nil
}

// containsMalformedToolCalls checks if content contains tool call-like patterns that aren't properly formatted
func (a *Agent) containsMalformedToolCalls(content string) bool {
	if content == "" {
		return false
	}

	// Check for common patterns that indicate malformed tool calls
	patterns := []string{
		`{"tool_calls":`,
		`"function":`,
		`"arguments":`,
		`shell_command`,
		`read_file`,
		`write_file`,
		`edit_file`,
		`"cmd":`, // Also detect the cmd format
	}

	for _, pattern := range patterns {
		if strings.Contains(content, pattern) {
			return true
		}
	}

	return false
}

// extractFromMarkdownBlocks extracts JSON from markdown code blocks
func (a *Agent) extractFromMarkdownBlocks(content string) string {
	// Extract JSON from ```json blocks
	jsonBlockRegex := regexp.MustCompile("```json\\s*([\\s\\S]*?)```")
	matches := jsonBlockRegex.FindAllStringSubmatch(content, -1)

	var extracted strings.Builder
	extracted.WriteString(content) // Keep original content

	for _, match := range matches {
		if len(match) > 1 {
			extracted.WriteString("\n")
			extracted.WriteString(strings.TrimSpace(match[1]))
		}
	}

	return extracted.String()
}

// extractXMLToolCalls extracts XML-style tool calls like <function=shell_command><parameter=command>ls</parameter></function>
func (a *Agent) extractXMLToolCalls(content string) []api.ToolCall {
	var toolCalls []api.ToolCall

	// Regex to match XML-style function calls
	// Matches: <function=FUNCTION_NAME>...</function>
	// Also handle cases where </tool_call> is used instead of </function>
	functionRegex := regexp.MustCompile(`<function=(\w+)>([\s\S]*?)(?:</function>|</tool_call>)`)
	functionMatches := functionRegex.FindAllStringSubmatch(content, -1)

	for _, match := range functionMatches {
		if len(match) < 3 {
			continue
		}

		functionName := match[1]
		functionContent := match[2]

		// Extract parameters from within the function
		// Matches: <parameter=PARAM_NAME>VALUE</parameter>
		paramRegex := regexp.MustCompile(`<parameter=(\w+)>([\s\S]*?)</parameter>`)
		paramMatches := paramRegex.FindAllStringSubmatch(functionContent, -1)

		// Build arguments JSON
		args := make(map[string]interface{})
		for _, paramMatch := range paramMatches {
			if len(paramMatch) >= 3 {
				paramName := paramMatch[1]
				paramValue := strings.TrimSpace(paramMatch[2])
				args[paramName] = paramValue
			}
		}

		// Convert to JSON string for arguments
		argsJSON, err := json.Marshal(args)
		if err != nil {
			continue
		}

		// Create tool call
		toolCall := api.ToolCall{
			ID:   fmt.Sprintf("call_%d", time.Now().UnixNano()),
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      functionName,
				Arguments: string(argsJSON),
			},
		}

		toolCalls = append(toolCalls, toolCall)
	}

	return toolCalls
}
