package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent_api"
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
	validTools := []string{"shell_command", "read_file", "write_file", "edit_file", "search_files", "add_todos", "update_todo_status", "list_todos", "web_search", "fetch_url", "analyze_ui_screenshot", "analyze_image_content"}
	isValidTool := false
	isMCPTool := false

	// Check if it's a standard tool
	for _, valid := range validTools {
		if toolCall.Function.Name == valid {
			isValidTool = true
			break
		}
	}

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
	registry := GetToolRegistry()
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

	// Legacy switch statement - remove after testing
	/*
		switch toolCall.Function.Name {
		case "shell_command":
			command, ok := args["command"].(string)
			if !ok {
				// Try alternative parameter name for backward compatibility
				command, ok = args["cmd"].(string)
				if !ok {
					return "", fmt.Errorf("invalid command argument")
				}
			}
			return a.executeShellCommandWithTruncation(ctx, command)

		case "read_file":
			filePath, ok := args["file_path"].(string)
			if !ok {
				// Try alternative parameter name for backward compatibility
				filePath, ok = args["path"].(string)
				if !ok {
					return "", fmt.Errorf("invalid file_path argument")
				}
			}

			// Check for optional line range parameters
			var startLine, endLine int
			if start, ok := args["start_line"].(float64); ok {
				startLine = int(start)
			}
			if end, ok := args["end_line"].(float64); ok {
				endLine = int(end)
			}

			// Log the operation
			if startLine > 0 || endLine > 0 {
				a.ToolLog("reading file", fmt.Sprintf("%s (lines %d-%d)", filePath, startLine, endLine))
				a.debugLog("Reading file: %s (lines %d-%d)\n", filePath, startLine, endLine)
				result, err := tools.ReadFileWithRange(filePath, startLine, endLine)
				a.debugLog("Read file result: %s, error: %v\n", result, err)
				return result, err
			} else {
				a.ToolLog("reading file", filePath)
				a.debugLog("Reading file: %s\n", filePath)
				result, err := tools.ReadFile(filePath)
				a.debugLog("Read file result: %s, error: %v\n", result, err)
				return result, err
			}

		case "write_file":
			filePath, ok := args["file_path"].(string)
			if !ok {
				// Try alternative parameter name for backward compatibility
				filePath, ok = args["path"].(string)
				if !ok {
					return "", fmt.Errorf("invalid file_path argument")
				}
			}
			content, ok := args["content"].(string)
			if !ok {
				return "", fmt.Errorf("invalid content argument")
			}
			a.ToolLog("writing file", filePath)
			a.debugLog("Writing file: %s\n", filePath)

			// Track the file write for change tracking
			if trackErr := a.TrackFileWrite(filePath, content); trackErr != nil {
				a.debugLog("Warning: Failed to track file write: %v\n", trackErr)
			}

			result, err := tools.WriteFile(filePath, content)
			a.debugLog("Write file result: %s, error: %v\n", result, err)
			return result, err

		case "edit_file":
			filePath, ok := args["file_path"].(string)
			if !ok {
				// Try alternative parameter name for backward compatibility
				filePath, ok = args["path"].(string)
				if !ok {
					return "", fmt.Errorf("invalid file_path argument")
				}
			}
			oldString, ok := args["old_string"].(string)
			if !ok {
				return "", fmt.Errorf("invalid old_string argument")
			}
			newString, ok := args["new_string"].(string)
			if !ok {
				return "", fmt.Errorf("invalid new_string argument")
			}

			// Read the original content for diff display
			originalContent, err := tools.ReadFile(filePath)
			if err != nil {
				return "", fmt.Errorf("failed to read original file for diff: %w", err)
			}

			// Check circuit breaker before editing
			if blocked, warning := a.CheckCircuitBreaker("edit_file", filePath, 3); blocked {
				return warning, fmt.Errorf("circuit breaker triggered - too many edit attempts on same file")
			}

			a.ToolLog("editing file", filePath)
			a.debugLog("Editing file: %s\n", filePath)
			result, err := tools.EditFile(filePath, oldString, newString)

			if err == nil {
				// Read the new content and show diff
				newContent, readErr := tools.ReadFile(filePath)
				if readErr == nil {
					a.ShowColoredDiff(originalContent, newContent, 50)

					// Track the file edit for change tracking
					if trackErr := a.TrackFileEdit(filePath, originalContent, newContent); trackErr != nil {
						a.debugLog("Warning: Failed to track file edit: %v\n", trackErr)
					}
				}
			}

			a.debugLog("Edit file result: %s, error: %v\n", result, err)
			return result, err

		case "add_todos":
			todosRaw, ok := args["todos"]
			if !ok {
				return "", fmt.Errorf("missing todos argument")
			}

			// Parse the todos array
			todosSlice, ok := todosRaw.([]interface{})
			if !ok {
				return "", fmt.Errorf("todos must be an array")
			}

			var todos []struct {
				Title       string
				Description string
				Priority    string
			}

			for _, todoRaw := range todosSlice {
				todoMap, ok := todoRaw.(map[string]interface{})
				if !ok {
					return "", fmt.Errorf("each todo must be an object")
				}

				todo := struct {
					Title       string
					Description string
					Priority    string
				}{}

				if title, ok := todoMap["title"].(string); ok {
					todo.Title = title
				}
				if desc, ok := todoMap["description"].(string); ok {
					todo.Description = desc
				}
				if prio, ok := todoMap["priority"].(string); ok {
					todo.Priority = prio
				} else {
					todo.Priority = "medium" // default
				}

				todos = append(todos, todo)
			}

			// Show the todo titles being created
			todoTitles := make([]string, len(todos))
			for i, todo := range todos {
				todoTitles[i] = todo.Title
			}
			if len(todoTitles) == 1 {
				a.ToolLog("adding todo", todoTitles[0])
			} else if len(todoTitles) <= 3 {
				a.ToolLog("adding todos", strings.Join(todoTitles, ", "))
			} else {
				a.ToolLog("adding todos", fmt.Sprintf("%s, %s, +%d more", todoTitles[0], todoTitles[1], len(todoTitles)-2))
			}
			a.debugLog("Adding %d todos\n", len(todos))
			result := tools.AddBulkTodos(todos)
			a.debugLog("Add todos result: %s\n", result)
			return result, nil

		case "update_todo_status":
			id, ok := args["id"].(string)
			if !ok {
				return "", fmt.Errorf("invalid id argument")
			}
			status, ok := args["status"].(string)
			if !ok {
				return "", fmt.Errorf("invalid status argument")
			}
			// Show better ToolLog message based on status
			var logMessage string
			switch status {
			case "in_progress":
				// Extract the todo title for a better message
				todoTitle := ""
				for _, item := range tools.GetAllTodos() {
					if item.ID == id {
						todoTitle = item.Title
						break
					}
				}
				if todoTitle != "" {
					logMessage = fmt.Sprintf("starting %s", todoTitle)
				} else {
					logMessage = fmt.Sprintf("starting %s", id)
				}
			case "completed":
				// Extract the todo title for a better message
				todoTitle := ""
				for _, item := range tools.GetAllTodos() {
					if item.ID == id {
						todoTitle = item.Title
						break
					}
				}
				if todoTitle != "" {
					logMessage = fmt.Sprintf("completed %s", todoTitle)
				} else {
					logMessage = fmt.Sprintf("completed %s", id)
				}
			default:
				logMessage = fmt.Sprintf("%s -> %s", id, status)
			}
			a.ToolLog("todo update", logMessage)
			a.debugLog("Updating todo %s to %s\n", id, status)
			result := tools.UpdateTodoStatus(id, status)
			a.debugLog("Update todo result: %s\n", result)
			return result, nil

		case "list_todos":
			a.ToolLog("listing todos", "")
			a.debugLog("Listing todos\n")
			result := tools.ListTodos()
			a.debugLog("List todos result: %s\n", result)
			return result, nil

		case "analyze_ui_screenshot":
			imagePath, ok := args["image_path"].(string)
			if !ok {
				return "", fmt.Errorf("invalid image_path argument")
			}

			// Clear any previous vision usage before the call
			tools.ClearLastVisionUsage()

			// UI screenshot analysis always uses optimized prompts for better caching
			a.ToolLog("UI screenshot analysis", fmt.Sprintf("%s [optimized prompt]",
				filepath.Base(imagePath)))

			// Check for interrupt before expensive vision call
			if a.CheckForInterrupt() {
				return "", fmt.Errorf("🛑 UI analysis interrupted by user")
			}
			// Always use empty prompt for UI screenshots to maximize caching efficiency
			result, err := tools.AnalyzeImage(imagePath, "", "frontend")
			if err != nil {
				return "", fmt.Errorf("UI screenshot analysis failed: %w", err)
			}

			// Check if vision model usage needs to be tracked
			if visionUsage := tools.GetLastVisionUsage(); visionUsage != nil {
				// Add vision model costs to agent's tracking
				a.totalCost += visionUsage.EstimatedCost
				a.totalTokens += visionUsage.TotalTokens
				a.promptTokens += visionUsage.PromptTokens
				a.completionTokens += visionUsage.CompletionTokens

				// Call stats update callback if set
				if a.statsUpdateCallback != nil {
					// Debug log when callback is invoked
					if os.Getenv("DEBUG") == "1" {
						fmt.Fprintf(os.Stderr, "\n[DEBUG] Invoking stats callback from vision tool: total=%d, cost=%.4f\n", a.totalTokens, a.totalCost)
					}
					a.statsUpdateCallback(a.totalTokens, a.totalCost)
				}

				// Always log vision costs (they're significant)
				a.debugLog("💰 UI Screenshot call: %s [frontend] → %d tokens, $%.6f\n",
					filepath.Base(imagePath), visionUsage.TotalTokens, visionUsage.EstimatedCost)
			}

			return result, nil

		case "analyze_image_content":
			imagePath, ok := args["image_path"].(string)
			if !ok {
				return "", fmt.Errorf("invalid image_path argument")
			}

			// Get optional analysis prompt
			analysisPrompt := ""
			if prompt, ok := args["analysis_prompt"].(string); ok {
				analysisPrompt = prompt
			}

			// Clear any previous vision usage before the call
			tools.ClearLastVisionUsage()

			// Enhanced logging for content analysis
			promptInfo := "auto"
			if analysisPrompt != "" {
				promptInfo = fmt.Sprintf("custom (%d chars)", len(analysisPrompt))
			}

			a.ToolLog("image content analysis", fmt.Sprintf("%s [prompt:%s]",
				filepath.Base(imagePath), promptInfo))

			// Check for interrupt before expensive vision call
			if a.CheckForInterrupt() {
				return "", fmt.Errorf("🛑 Content analysis interrupted by user")
			}
			result, err := tools.AnalyzeImage(imagePath, analysisPrompt, "general")
			if err != nil {
				return "", fmt.Errorf("image content analysis failed: %w", err)
			}

			// Check if vision model usage needs to be tracked
			if visionUsage := tools.GetLastVisionUsage(); visionUsage != nil {
				// Add vision model costs to agent's tracking
				a.totalCost += visionUsage.EstimatedCost
				a.totalTokens += visionUsage.TotalTokens
				a.promptTokens += visionUsage.PromptTokens
				a.completionTokens += visionUsage.CompletionTokens

				// Always log vision costs (they're significant)
				a.debugLog("💰 Content Analysis call: %s [general] → %d tokens, $%.6f\n",
					filepath.Base(imagePath), visionUsage.TotalTokens, visionUsage.EstimatedCost)
			}

			return result, nil

		case "web_search":
			query, ok := args["query"].(string)
			if !ok {
				return "", fmt.Errorf("invalid query argument")
			}
			a.ToolLog("web search", query)
			a.debugLog("Performing web search: %s\n", query)

			// Get the config manager from the agent
			cfg := a.GetConfigManager()
			result, err := tools.WebSearch(query, cfg)
			if err != nil {
				a.debugLog("Web search failed: %v\n", err)
				return "", fmt.Errorf("web search failed: %w", err)
			}
			a.debugLog("Web search completed, found %d characters of content\n", len(result))
			return result, nil

		case "fetch_url":
			url, ok := args["url"].(string)
			if !ok {
				return "", fmt.Errorf("invalid url argument")
			}
			a.ToolLog("fetch url", url)
			a.debugLog("Fetching URL: %s\n", url)

			// Get the config manager from the agent
			cfg := a.GetConfigManager()
			result, err := tools.FetchURL(url, cfg)
			if err != nil {
				a.debugLog("URL fetch failed: %v\n", err)
				return "", fmt.Errorf("URL fetch failed: %w", err)
			}
			a.debugLog("URL fetch completed, found %d characters of content\n", len(result))
			return result, nil

		case "search_files":
			pattern, ok := args["pattern"].(string)
			if !ok {
				return "", fmt.Errorf("invalid pattern argument")
			}

			directory := "."
			if dir, ok := args["directory"].(string); ok && dir != "" {
				directory = dir
			}

			filePattern := ""
			if fp, ok := args["file_pattern"].(string); ok {
				filePattern = fp
			}

			caseSensitive := false
			if cs, ok := args["case_sensitive"].(bool); ok {
				caseSensitive = cs
			}

			maxResults := 100
			if mr, ok := args["max_results"].(float64); ok {
				maxResults = int(mr)
			}

			a.ToolLog("searching files", fmt.Sprintf("'%s' in %s", pattern, directory))
			a.debugLog("Searching files: pattern='%s', directory='%s', file_pattern='%s'\n", pattern, directory, filePattern)

			var command string
			grepFlags := "-n"
			if !caseSensitive {
				grepFlags += "i"
			}

			if filePattern != "" {
				command = fmt.Sprintf("find %s -name '%s' -type f -exec grep %s '%s' {} + | head -%d",
					directory, filePattern, grepFlags, pattern, maxResults)
			} else {
				command = fmt.Sprintf("find %s -type f -exec grep %s '%s' {} + | head -%d",
					directory, grepFlags, pattern, maxResults)
			}

			result, err := a.executeShellCommandWithTruncation(command)
			if err != nil {
				a.debugLog("File search failed: %v\n", err)
				return "", fmt.Errorf("file search failed: %w", err)
			}

			if result == "" {
				return fmt.Sprintf("No matches found for pattern '%s' in %s", pattern, directory), nil
			}

			a.debugLog("File search completed, found results\n")
			return result, nil

		default:
			// Check if it's an MCP tool
			if isMCPTool {
				return a.executeMCPTool(toolCall.Function.Name, args)
			}
			return "", fmt.Errorf("unknown tool: %s", toolCall.Function.Name)
		}
	*/
}

// extractToolCallsFromContent attempts to parse tool calls from the assistant's content or reasoning_content
func (a *Agent) extractToolCallsFromContent(content string) []api.ToolCall {
	var toolCalls []api.ToolCall

	if content == "" {
		return toolCalls
	}

	// Handle markdown code blocks first
	content = a.extractFromMarkdownBlocks(content)

	// First check for XML-style tool calls (e.g., <function=shell_command>)
	xmlToolCalls := a.extractXMLToolCalls(content)
	if len(xmlToolCalls) > 0 {
		return xmlToolCalls
	}

	// Look for tool_calls JSON structure in content
	if strings.Contains(content, "tool_calls") {
		// Try to extract and parse tool calls from content
		start := strings.Index(content, `{"tool_calls":`)
		if start != -1 {
			// Find the end of the JSON object - need to count braces properly
			jsonStr := a.extractJSONFromContent(content[start:])
			if jsonStr != "" {
				var toolCallData struct {
					ToolCalls []api.ToolCall `json:"tool_calls"`
				}

				if err := json.Unmarshal([]byte(jsonStr), &toolCallData); err == nil {
					toolCalls = toolCallData.ToolCalls
				}
			}
		}
	}

	// Also look for individual tool calls with specific function names
	if len(toolCalls) == 0 {
		toolCalls = a.extractIndividualToolCalls(content)
	}

	// Also check for alternative formats like {"cmd": ["bash", "-lc", "ls -R"]}
	if strings.Contains(content, `"cmd":`) {
		// Try to parse the cmd format
		var cmdData struct {
			Cmd []string `json:"cmd"`
		}

		if err := json.Unmarshal([]byte(content), &cmdData); err == nil && len(cmdData.Cmd) > 0 {
			// Convert cmd format to shell_command tool call
			command := strings.Join(cmdData.Cmd[1:], " ") // Skip the shell (e.g., "bash")
			if len(cmdData.Cmd) > 1 {
				command = strings.Join(cmdData.Cmd[1:], " ")
			}

			toolCall := api.ToolCall{
				ID:   fmt.Sprintf("call_%d", time.Now().UnixNano()),
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      "shell_command",
					Arguments: fmt.Sprintf(`{"command": "%s"}`, command),
				},
			}
			toolCalls = append(toolCalls, toolCall)
		}
	}

	return toolCalls
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
	functionNames := []string{"write_file", "read_file", "edit_file", "shell_command", "analyze_ui_screenshot", "analyze_image_content"}

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
