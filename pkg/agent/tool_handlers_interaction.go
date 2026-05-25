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
	userID := a.GetEventUserID()
	chatID := a.GetEventChatID()
	askUserMgr := a.security.GetAskUserMgr()

	// Route to the WebUI only when a browser is actually listening. Sprout
	// auto-starts a WebUI on a local port by default, so eventBus and
	// askUserMgr are typically non-nil even in a terminal-only session — if
	// we skipped this check the question would publish to a bus nobody is
	// listening to and the tool would hang for the 10-minute timeout while
	// the user sees nothing in their terminal. Mirrors the pattern used by
	// security prompts (see tool_handlers_shell.go:297, tool_definitions.go:569,
	// agent_getters.go:78).
	hasActiveWebUI := eventBus != nil && askUserMgr != nil && a.HasActiveWebUIClients()

	if a.debug {
		a.Logger().Debug("[ask_user] Prompting user: %s\n", question)
		a.Logger().Debug("[ask_user] eventBus=%v askUserMgr=%v hasActiveWebUI=%v clientID=%q userID=%q chatID=%q\n",
			eventBus != nil, askUserMgr != nil, hasActiveWebUI, clientID, userID, chatID)
	}

	var response string
	var err error
	if hasActiveWebUI {
		response, err = tools.AskUserWithEventBus(ctx, question, eventBus, clientID, userID, chatID, askUserMgr)
	} else {
		// No active browser tab → CLI fallback. Prints the question to stdout
		// and reads the user's answer from stdin.
		response, err = tools.AskUser(question)
	}
	if err != nil {
		if a.debug {
			a.Logger().Debug("[ask_user] Error: %v\n", err)
		}
		return "", fmt.Errorf("ask_user failed: %w", err)
	}

	if a.debug {
		a.Logger().Debug("[ask_user] User response: %s\n", response)
	}

	return response, nil
}
