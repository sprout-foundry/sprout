package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// handleDelegateStatus is the tool handler for the delegate_status tool.
// It checks the current status of an asynchronously running delegate.
func handleDelegateStatus(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent is required")
	}

	delegateID, _ := args["delegate_id"].(string)
	if delegateID == "" {
		return "", fmt.Errorf("delegate_id is required")
	}

	a.initSubManagers()

	status, result, found := a.asyncDelegateTracker.GetStatus(delegateID)
	if !found {
		return "", fmt.Errorf("delegate %q not found", delegateID)
	}

	response := map[string]interface{}{
		"delegate_id": delegateID,
		"status":      status,
	}

	if status == "running" {
		response["message"] = "Delegate is still running"
	} else {
		if result != nil {
			response["summary"] = result.Summary
			response["exit_status"] = result.ExitStatus
			if result.ErrorMessage != "" {
				response["error"] = result.ErrorMessage
			}
			response["iterations"] = result.Iterations
			response["tokens_used"] = result.TokensUsed
			response["cost"] = result.Cost
			if len(result.FilesChanged) > 0 {
				response["files_changed"] = result.FilesChanged
			}
			if len(result.ToolsCalled) > 0 {
				response["tool_calls"] = result.ToolsCalled
			}
		}
	}

	jsonBytes, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal status response: %w", err)
	}

	return string(jsonBytes), nil
}
