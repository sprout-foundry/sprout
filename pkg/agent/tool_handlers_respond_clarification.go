package agent

import (
	"context"
	"fmt"
)

// handleRespondClarification is the tool handler for the respond_clarification tool.
// It is called by parent agents to respond to a subagent's clarification request.
func handleRespondClarification(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	requestID, _ := args["request_id"].(string)
	if requestID == "" {
		return "", fmt.Errorf("request_id parameter is required")
	}

	response, _ := args["response"].(string)
	if response == "" {
		return "", fmt.Errorf("response parameter is required")
	}

	if a.delegateID != "" {
		return "", fmt.Errorf("respond_clarification is only available for parent agents")
	}

	a.initSubManagers()

	if a.clarificationManager == nil {
		return "", fmt.Errorf("clarification manager not available")
	}

	err := a.clarificationManager.RespondClarification(requestID, response)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Clarification response delivered for request %s", requestID), nil
}
