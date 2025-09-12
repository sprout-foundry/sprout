package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/agent_tools"
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
	validTools := []string{"shell_command", "read_file", "write_file", "edit_file", "add_todo", "update_todo_status", "list_todos", "add_bulk_todos", "auto_complete_todos", "get_next_todo", "list_all_todos", "get_active_todos_compact", "archive_completed", "update_todo_status_bulk", "analyze_ui_screenshot", "analyze_image_content"}
	isValidTool := false
	for _, valid := range validTools {
		if toolCall.Function.Name == valid {
			isValidTool = true
			break
		}
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
		return a.executeShellCommandWithTruncation(command)

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

	case "add_todo":
		title, ok := args["title"].(string)
		if !ok {
			return "", fmt.Errorf("invalid title argument")
		}
		description := ""
		if desc, ok := args["description"].(string); ok {
			description = desc
		}
		priority := ""
		if prio, ok := args["priority"].(string); ok {
			priority = prio
		}
		a.ToolLog("adding todo", title)
		a.debugLog("Adding todo: %s\n", title)
		result := tools.AddTodo(title, description, priority)
		a.debugLog("Add todo result: %s\n", result)
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

	case "add_bulk_todos":
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
			}
			
			todos = append(todos, todo)
		}
		
		// Show the todo titles being created
		todoTitles := make([]string, len(todos))
		for i, todo := range todos {
			todoTitles[i] = todo.Title
		}
		if len(todoTitles) <= 3 {
			a.ToolLog("adding todos", strings.Join(todoTitles, ", "))
		} else {
			a.ToolLog("adding todos", fmt.Sprintf("%s, %s, +%d more", todoTitles[0], todoTitles[1], len(todoTitles)-2))
		}
		a.debugLog("Adding bulk todos: %d items\n", len(todos))
		result := tools.AddBulkTodos(todos)
		a.debugLog("Add bulk todos result: %s\n", result)
		return result, nil

	case "auto_complete_todos":
		context, ok := args["context"].(string)
		if !ok {
			return "", fmt.Errorf("invalid context argument")
		}
		a.ToolLog("auto completing todos", context)
		a.debugLog("Auto completing todos with context: %s\n", context)
		result := tools.AutoCompleteTodos(context)
		a.debugLog("Auto complete result: %s\n", result)
		return result, nil

	case "get_next_todo":
		a.ToolLog("getting next todo", "")
		a.debugLog("Getting next todo\n")
		result := tools.GetNextTodo()
		a.debugLog("Next todo result: %s\n", result)
		return result, nil

	case "list_all_todos":
		a.ToolLog("listing all todos", "full context")
		result := tools.ListAllTodos()
		return result, nil

	case "get_active_todos_compact":
		a.ToolLog("getting active todos", "compact")
		result := tools.GetActiveTodosCompact()
		return result, nil

	case "archive_completed":
		a.ToolLog("archiving completed", "")
		result := tools.ArchiveCompleted()
		return result, nil

	case "update_todo_status_bulk":
		updatesRaw, ok := args["updates"]
		if !ok {
			return "", fmt.Errorf("missing updates argument")
		}
		
		updatesSlice, ok := updatesRaw.([]interface{})
		if !ok {
			return "", fmt.Errorf("updates must be an array")
		}
		
		var updates []struct {
			ID     string
			Status string
		}
		
		for _, updateRaw := range updatesSlice {
			updateMap, ok := updateRaw.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("each update must be an object")
			}
			
			update := struct {
				ID     string
				Status string
			}{}
			
			if id, ok := updateMap["id"].(string); ok {
				update.ID = id
			}
			if status, ok := updateMap["status"].(string); ok {
				update.Status = status
			}
			
			updates = append(updates, update)
		}
		
		a.ToolLog("bulk status update", fmt.Sprintf("%d items", len(updates)))
		result := tools.UpdateTodoStatusBulk(updates)
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

	default:
		return "", fmt.Errorf("unknown tool: %s", toolCall.Function.Name)
	}
}

// extractToolCallsFromContent attempts to parse tool calls from the assistant's content or reasoning_content
func (a *Agent) extractToolCallsFromContent(content string) []api.ToolCall {
	var toolCalls []api.ToolCall

	if content == "" {
		return toolCalls
	}

	// Handle markdown code blocks first
	content = a.extractFromMarkdownBlocks(content)

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