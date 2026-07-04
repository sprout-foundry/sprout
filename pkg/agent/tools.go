package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// executeTool handles the execution of individual tool calls
func (a *Agent) executeTool(toolCall api.ToolCall) (string, error) {
	// Some models (e.g., Harmony/GPT-OSS) append "<|channel|>xxx" suffix to tool names
	// where xxx can be "commentary", "json", "tool_use", etc. Strip it to get the actual tool name.
	toolName := strings.Split(toolCall.Function.Name, "<|channel|>")[0]
	if alias := a.suggestCorrectToolName(toolName); alias != "" {
		toolName = alias
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return "", agenterrors.Wrap(err, "failed to parse tool arguments")
	}

	// Log the tool call for debugging
	a.Logger().Debug("[tool] Executing tool: %s with args: %v\n", toolName, args)

	// Validate tool name and provide helpful error for common mistakes
	allHandlers := tools.GetNewToolRegistry().All()
	validTools := make([]string, 0, len(allHandlers)+1)
	for _, h := range allHandlers {
		validTools = append(validTools, h.Name())
	}
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
			return "", agenterrors.NewInvalidInputError(fmt.Sprintf("unknown tool '%s'. Did you mean '%s'? Valid tools are: %s",
				toolName, suggestion, strings.Join(validTools, ", ")), nil)
		}
		return "", agenterrors.NewInvalidInputError(fmt.Sprintf("unknown tool '%s'. Valid tools are: %s", toolName, strings.Join(validTools, ", ")), nil)
	}

	// Execute the tool
	ctx := filesystem.WithWorkspaceRoot(context.Background(), a.GetWorkspaceRoot())
	_, result, err := ExecuteTool(ctx, toolName, args, a, toolCall.Function.Arguments)

	// Track tool call count
	a.state.IncrementTotalToolCalls()

	// If tool not found in registry, check for special cases
	if err != nil && agenterrors.IsInvalidInput(err) {
		// Handle mcp_tools meta-tool
		if toolName == "mcp_tools" {
			return a.handleMCPToolsCommand(args)
		}

		// Handle direct MCP tool calls
		if isMCPTool {
			return a.executeMCPTool(toolName, args)
		}
		return "", agenterrors.NewInvalidInputError(fmt.Sprintf("unknown tool '%s'", toolName), err)
	}

	if err != nil {
		return result, agenterrors.Wrap(err, fmt.Sprintf("execute tool %q", toolName))
	}
	return result, nil
}
