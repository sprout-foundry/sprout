package agent

import (
	"context"
	"fmt"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// handleRequestClarification is the tool handler for the request_clarification tool.
// It is called by subagents when they need clarification from their parent.
func handleRequestClarification(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	question, _ := args["question"].(string)
	if question == "" {
		return "", agenterrors.NewTool("request_clarification", "question parameter is required", nil)
	}

	a.initSubManagers()

	if a.clarificationManager == nil {
		return "", agenterrors.NewTool("request_clarification", "clarification manager not available", nil)
	}

	if a.subagentID == "" {
		return "", agenterrors.NewTool("request_clarification", "request_clarification is only available for subagents", nil)
	}

	response, err := a.clarificationManager.RequestClarification(ctx, a.subagentID, question)
	if err != nil {
		return fmt.Sprintf("Clarification request failed: %v", err), nil
	}

	return fmt.Sprintf("Clarification received: %s", response), nil
}
