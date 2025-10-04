package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
