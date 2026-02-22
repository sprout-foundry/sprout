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
	// Some models (e.g., Qwen) append "<|channel|>commentary" suffix to tool names.
	// Strip it if present to extract the actual tool name.
	toolName := strings.TrimSuffix(toolCall.Function.Name, "<|channel|>commentary")

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	// Log the tool call for debugging
	a.debugLog("🔧 Executing tool: %s with args: %v\n", toolName, args)

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
	if _, exists := validToolSet[toolName]; exists {
		isValidTool = true
	}
	isMCPTool := false

	// If not a standard tool, check if it's an MCP tool
	if !isValidTool && strings.HasPrefix(toolName, "mcp_") {
		isMCPTool = a.isValidMCPTool(toolName)
		isValidTool = isMCPTool
	}

	if !isValidTool {
		// Check for common misnamed tools and suggest corrections
		suggestion := a.suggestCorrectToolName(toolName)
		if suggestion != "" {
			return "", fmt.Errorf("unknown tool '%s'. Did you mean '%s'? Valid tools are: %v",
				toolName, suggestion, validTools)
		}
		return "", fmt.Errorf("unknown tool '%s'. Valid tools are: %v", toolName, validTools)
	}

	// Use the tool registry for data-driven tool execution
	result, err := registry.ExecuteTool(context.Background(), toolName, args, a)

	// If tool not found in registry, check for special cases
	if err != nil && strings.Contains(err.Error(), "unknown tool") {
		// Handle mcp_tools meta-tool
		if toolName == "mcp_tools" {
			return a.handleMCPToolsCommand(args)
		}

		// Handle direct MCP tool calls
		if isMCPTool {
			return a.executeMCPTool(toolName, args)
		}
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}

	return result, err
}
