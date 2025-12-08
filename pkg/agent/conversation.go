package agent

import (
	"fmt"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
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

	// Add MCP tools if available
	mcpTools := a.getMCPTools()
	if mcpTools != nil {
		tools = append(tools, mcpTools...)
	}

	// Future: Could optimize by analyzing conversation context
	// and only returning relevant tools
	return tools
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
	// Skip if the current client doesn't support vision
	if a.client == nil || !a.client.SupportsVision() {
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
