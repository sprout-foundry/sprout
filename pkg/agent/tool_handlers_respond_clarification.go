package agent

import (
	"context"
	"fmt"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// handleRespondClarification is the tool handler for the respond_clarification tool.
// It is called by parent agents to respond to a subagent's clarification request.
func handleRespondClarification(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	requestID, _ := args["request_id"].(string)
	if requestID == "" {
		return "", agenterrors.NewTool("respond_clarification", "request_id parameter is required", nil)
	}

	response, _ := args["response"].(string)
	if response == "" {
		return "", agenterrors.NewTool("respond_clarification", "response parameter is required", nil)
	}

	if a.subagentID != "" {
		return "", agenterrors.NewTool("respond_clarification", "respond_clarification is only available for parent agents", nil)
	}

	a.initSubManagers()

	if a.clarificationManager == nil {
		return "", agenterrors.NewTool("respond_clarification", "clarification manager not available", nil)
	}

	err := a.clarificationManager.RespondClarification(requestID, response)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Clarification response delivered for request %s", requestID), nil
}
