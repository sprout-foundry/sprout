package agent

import (
	"fmt"
	"os"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/configuration"
)

// ProcessQuery handles the main conversation loop with the LLM
func (a *Agent) ProcessQuery(userQuery string) (string, error) {
	handler := NewConversationHandler(a)
	return handler.ProcessQuery(userQuery)
}

// ProcessQueryWithContinuity processes a query with continuity from previous actions
func (a *Agent) ProcessQueryWithContinuity(userQuery string) (string, error) {
	// Ensure changes are committed even if there are unexpected errors or early termination
	defer func() {
		// Only commit if we have changes and they haven't been committed yet
		if a.IsChangeTrackingEnabled() && a.GetChangeCount() > 0 {
			a.debugLog("DEFER: Attempting to commit %d tracked changes\n", a.GetChangeCount())
			// Check if changes are already committed by trying to commit (it's safe due to committed flag)
			if commitErr := a.CommitChanges("Session cleanup - ensuring changes are not lost"); commitErr != nil {
				a.debugLog("Warning: Failed to commit tracked changes during cleanup: %v\n", commitErr)
			} else {
				a.debugLog("DEFER: Successfully committed tracked changes during cleanup\n")
			}
		} else {
			a.debugLog("DEFER: No changes to commit (enabled: %v, count: %d)\n", a.IsChangeTrackingEnabled(), a.GetChangeCount())
		}

		// Auto-save memory state after every successful turn
		a.autoSaveState()
		a.debugLog("DEFER: Auto-saved memory state\n")
	}()

	// Load previous state if available
	if a.previousSummary != "" {
		continuityPrompt := fmt.Sprintf(`
CONTEXT FROM PREVIOUS SESSION:
%s

CURRENT TASK:
%s

Note: The user cannot see the previous session's responses, so please provide a complete answer without referencing "previous responses" or "as mentioned before". If this task relates to the previous session, build upon that work but present your response as if it's the first time addressing this topic.`,
			a.previousSummary, userQuery)

		return a.ProcessQuery(continuityPrompt)
	}

	// No previous state, process normally
	return a.ProcessQuery(userQuery)
}

// getOptimizedToolDefinitions returns tool definitions optimized based on conversation context
func (a *Agent) getOptimizedToolDefinitions(messages []api.Message) []api.Tool {
	// Start with standard tools
	tools := api.GetToolDefinitions()

	// Filter out run_subagent and run_parallel_subagents when:
	// 1. Running as a subagent (prevents nested subagents)
	// 2. User explicitly disabled subagents via --no-subagents flag or LEDIT_NO_SUBAGENTS env
	noSubagents := os.Getenv("LEDIT_SUBAGENT") == "1" || os.Getenv("LEDIT_NO_SUBAGENTS") == "1"
	if noSubagents {
		filtered := make([]api.Tool, 0, len(tools))
		for _, tool := range tools {
			// Skip run_subagent and run_parallel_subagents
			if tool.Function.Name == "run_subagent" || tool.Function.Name == "run_parallel_subagents" {
				continue
			}
			filtered = append(filtered, tool)
		}
		tools = filtered
	}

	// Add MCP tools if available
	mcpTools := a.getMCPTools()
	if mcpTools != nil {
		tools = append(tools, mcpTools...)
	}

	// For custom providers, apply tool filtering only when tool_calls is explicitly configured.
	if customProvider, ok := a.getCurrentCustomProvider(); ok {
		if len(customProvider.ToolCalls) > 0 {
			allowedToolSet := makeAllowedToolSet(customProvider.ToolCalls)
			tools = filterToolsByName(tools, allowedToolSet)
		}
	}

	// Apply active persona tool filter (used for direct /persona and subagent persona runs).
	if personaAllowlist := a.getActivePersonaToolAllowlist(); len(personaAllowlist) > 0 {
		tools = filterToolsByName(tools, makeAllowedToolSet(personaAllowlist))
	}

	// Future: Could optimize by analyzing conversation context
	// and only returning relevant tools
	return tools
}

func (a *Agent) getCurrentCustomProvider() (*configuration.CustomProviderConfig, bool) {
	if a.configManager == nil {
		return nil, false
	}
	config := a.configManager.GetConfig()
	if config == nil || config.CustomProviders == nil {
		return nil, false
	}

	provider, exists := config.CustomProviders[string(a.clientType)]
	if !exists {
		return nil, false
	}
	return &provider, true
}

func makeAllowedToolSet(toolNames []string) map[string]struct{} {
	toolSet := make(map[string]struct{}, len(toolNames))
	for _, name := range toolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		toolSet[trimmed] = struct{}{}
	}
	return toolSet
}

func filterToolsByName(tools []api.Tool, allowed map[string]struct{}) []api.Tool {
	filtered := make([]api.Tool, 0, len(tools))
	for _, tool := range tools {
		if _, ok := allowed[tool.Function.Name]; !ok {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

// ClearConversationHistory clears the conversation history
func (a *Agent) ClearConversationHistory() {
	// Keep messages empty; system prompt is added during prepareMessages
	a.messages = []api.Message{}
	a.currentIteration = 0
	a.previousSummary = ""

	a.debugLog("🧹 Conversation history cleared\n")
}

// SetConversationOptimization enables or disables conversation optimization
func (a *Agent) SetConversationOptimization(enabled bool) {
	if a.optimizer != nil {
		a.optimizer.SetEnabled(enabled)
		if enabled {
			a.debugLog("✨ Conversation optimization enabled\n")
		} else {
			a.debugLog("🔧 Conversation optimization disabled\n")
		}
	}
}

// GetOptimizationStats returns optimization statistics
func (a *Agent) GetOptimizationStats() map[string]interface{} {
	if a.optimizer != nil {
		return a.optimizer.GetOptimizationStats()
	}
	return map[string]interface{}{
		"enabled": false,
		"message": "Optimizer not initialized",
	}
}

// processImagesInQuery detects and processes images in user queries
func (a *Agent) processImagesInQuery(query string) (string, error) {
	// Skip if no client is available
	if a.client == nil {
		return query, nil
	}

	// Check if vision processing is available
	if !tools.HasVisionCapability() {
		// No vision capability available, return original query
		return query, nil
	}

	// Determine analysis mode from query context
	var analysisMode string
	if containsFrontendKeywords(query) {
		analysisMode = "frontend"
	} else {
		analysisMode = "general"
	}

	// Create vision processor using the current provider's vision model
	processor, err := tools.NewVisionProcessorWithProvider(a.debug, a.clientType)
	if err != nil {
		// Fall back to default vision processor if current provider doesn't support vision
		processor, err = tools.NewVisionProcessorWithMode(a.debug, analysisMode)
		if err != nil {
			return query, fmt.Errorf("failed to create vision processor: %w", err)
		}
	}

	// Process any images found in the text
	enhancedQuery, analyses, err := processor.ProcessImagesInText(query)
	if err != nil {
		return query, fmt.Errorf("failed to process images: %w", err)
	}

	// If images were processed, log the enhancement
	if len(analyses) > 0 {
		a.debugLog("🖼️ Processed %d image(s) and enhanced query with vision analysis\n", len(analyses))
		for _, analysis := range analyses {
			a.debugLog("  - %s: %s\n", analysis.ImagePath, analysis.Description[:min(100, len(analysis.Description))])
		}
	}

	return enhancedQuery, nil
}
