package agent

import (
	"context"
	"errors"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// Tool handler implementation for ask_user operation

func handleAskUser(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	req, err := parseAskUserToolArgs(args)
	if err != nil {
		return "", err
	}

	if a == nil {
		// Fallback to CLI-only mode if agent is nil
		response, err := tools.AskUser(req)
		if err != nil {
			return "", mapAskUserError(err)
		}
		return response, nil
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
	// the user sees nothing in their terminal.
	hasActiveWebUI := eventBus != nil && askUserMgr != nil && a.HasActiveWebUIClients()

	if a.debug {
		a.Logger().Debug("[ask_user] Prompting user: %s\n", req.Question)
		a.Logger().Debug("[ask_user] eventBus=%v askUserMgr=%v hasActiveWebUI=%v clientID=%q userID=%q chatID=%q options=%d\n",
			eventBus != nil, askUserMgr != nil, hasActiveWebUI, clientID, userID, chatID, len(req.Options))
	}

	var response string
	if hasActiveWebUI {
		response, err = tools.AskUserWithEventBus(ctx, req, eventBus, clientID, userID, chatID, askUserMgr)
	} else if a.isNonInteractive() {
		// No active browser tab AND no interactive terminal (daemon mode,
		// --skip-prompt, piped stdin). Short-circuit before calling AskUser
		// so we return the structured ErrAskUserNoChannel immediately rather
		// than relying on a separate TTY check deep in the tool layer.
		err = tools.ErrAskUserNoChannel
	} else {
		// CLI fallback: terminal is interactive. AskUser does its own
		// stdinIsTTY check (now consistent with isNonInteractive) and
		// returns ErrAskUserNoChannel if stdin is somehow unavailable.
		response, err = tools.AskUser(req)
	}
	if err != nil {
		if a.debug {
			a.Logger().Debug("[ask_user] Error: %v\n", err)
		}
		return "", mapAskUserError(err)
	}

	if a.debug {
		a.Logger().Debug("[ask_user] User response: %s\n", response)
	}

	return response, nil
}

func parseAskUserToolArgs(args map[string]interface{}) (tools.AskUserRequest, error) {
	questionRaw, ok := args["question"]
	if !ok {
		return tools.AskUserRequest{}, agenterrors.NewValidation("missing 'question' parameter", nil)
	}
	question, ok := questionRaw.(string)
	if !ok {
		return tools.AskUserRequest{}, agenterrors.NewValidation("'question' parameter must be a string", nil)
	}
	req := tools.AskUserRequest{Question: question}
	if h, ok := args["header"].(string); ok {
		req.Header = h
	}
	if d, ok := args["default"].(string); ok {
		req.Default = d
	}
	switch m := args["multi_select"].(type) {
	case bool:
		req.MultiSelect = m
	case string:
		req.MultiSelect = m == "true"
	}
	if raw, ok := args["options"]; ok {
		req.Options = coerceAskUserOptions(raw)
	}
	return req, nil
}

func coerceAskUserOptions(raw interface{}) []tools.AskUserOption {
	switch v := raw.(type) {
	case []interface{}:
		out := make([]tools.AskUserOption, 0, len(v))
		for _, entry := range v {
			switch e := entry.(type) {
			case string:
				if e != "" {
					out = append(out, tools.AskUserOption{Label: e})
				}
			case map[string]interface{}:
				opt := tools.AskUserOption{}
				if s, ok := e["label"].(string); ok {
					opt.Label = s
				}
				if s, ok := e["value"].(string); ok {
					opt.Value = s
				}
				if s, ok := e["description"].(string); ok {
					opt.Description = s
				}
				if opt.Label != "" {
					out = append(out, opt)
				}
			}
		}
		return out
	case []string:
		out := make([]tools.AskUserOption, 0, len(v))
		for _, s := range v {
			if s != "" {
				out = append(out, tools.AskUserOption{Label: s})
			}
		}
		return out
	}
	return nil
}

func mapAskUserError(err error) error {
	if errors.Is(err, tools.ErrAskUserNoChannel) {
		return agenterrors.NewTool("ask_user", "ask_user: no interactive input channel is available (no WebUI client connected and stdin is not a TTY). Make a best-effort decision based on the existing context, or report that you cannot proceed without user input", nil)
	}
	return agenterrors.Wrap(err, "ask_user failed")
}
