package agent

import (
	"context"
	"fmt"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// Tool handler implementation for ask_user operation

func handleAskUser(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	questionRaw, ok := args["question"]
	if !ok {
		return "", fmt.Errorf("missing 'question' parameter")
	}

	question, ok := questionRaw.(string)
	if !ok {
		return "", fmt.Errorf("'question' parameter must be a string")
	}

	if a == nil {
		// Fallback to CLI-only mode if agent is nil
		return tools.AskUser(question)
	}

	eventBus := a.GetEventBus()
	clientID := a.GetEventClientID()

	if a.debug {
		a.debugLog("[ask_user] Prompting user: %s\n", question)
	}

	// Use event bus if available (WebUI mode), otherwise fallback to stdin
	response, err := tools.AskUserWithEventBus(question, eventBus, clientID)
	if err != nil {
		if a.debug {
			a.debugLog("[ask_user] Error: %v\n", err)
		}
		return "", fmt.Errorf("ask_user failed: %w", err)
	}

	if a.debug {
		a.debugLog("[ask_user] User response: %s\n", response)
	}

	return response, nil
}
