package agent

import (
	"context"
	"fmt"
)

// handleRequestClarification is the tool handler for the request_clarification tool.
// It is called by subagents when they need clarification from their parent.
func handleRequestClarification(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	question, _ := args["question"].(string)
	if question == "" {
		return "", fmt.Errorf("question parameter is required")
	}

	a.initSubManagers()

	if a.clarificationManager == nil {
		return "", fmt.Errorf("clarification manager not available")
	}

	if a.subagentID == "" {
		return "", fmt.Errorf("request_clarification is only available for subagents")
	}

	response, err := a.clarificationManager.RequestClarification(ctx, a.subagentID, question)
	if err != nil {
		return fmt.Sprintf("Clarification request failed: %v", err), nil
	}

	return fmt.Sprintf("Clarification received: %s", response), nil
}
