//go:build js && wasm

package main

import (
	"syscall/js"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// wasmAskUserMgr is the in-process AskUserManager for all WASM agent
// instances. It holds the pending request→response channel map so that
// ask_user requests from the agent can be answered by the webui via the
// RespondToAskUser bridge function.
var wasmAskUserMgr = tools.NewAskUserManager()

func askUserJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"respondToAskUser": js.FuncOf(respondToAskUserFunc),
	}
}

// RespondToAskUser delivers a user's answer to a pending ask_user request,
// unblocking the agent's RequestAskUser call. Returns true if a matching
// request existed and was resolved, false otherwise (unknown/expired ID).
func RespondToAskUser(requestID, response string) bool {
	return wasmAskUserMgr.RespondToAskUser(requestID, response)
}

// respondToAskUserFunc is the JS bridge wrapper around RespondToAskUser.
// Called from the webui when the AskUserDialog is dismissed in cloud mode.
// Returns { delivered: bool }.
func respondToAskUserFunc(_ js.Value, args []js.Value) interface{} {
	requestID := argString(args, 0, "")
	response := argString(args, 1, "")
	return map[string]interface{}{
		"delivered": RespondToAskUser(requestID, response),
	}
}
