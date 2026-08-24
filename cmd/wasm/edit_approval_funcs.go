//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

func editApprovalJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"respondToEditDecision": js.FuncOf(respondToEditDecisionFunc),
	}
}

// respondToEditDecisionFunc is the JS bridge wrapper around
// agent.DeliverEditDecision.  Called from the webui's
// EditApprovalPanel in cloud/WASM mode to deliver the user's
// per-hunk accept/reject decision back to the blocking agent.
//
// Arguments:
//  1. requestID  — string  (the edit approval request ID)
//  2. approved   — bool    (true = accept, false = reject)
//  3. acceptedHunks — array of strings (hunk IDs to accept)
//
// Returns { delivered: bool }.
func respondToEditDecisionFunc(_ js.Value, args []js.Value) interface{} {
	requestID := argString(args, 0, "")
	approved := argBool(args, 1, false)
	acceptedHunks := argStringSlice(args, 2)

	decision := agent.EditDecision{
		Approved:      approved,
		AcceptedHunks: acceptedHunks,
	}

	return map[string]interface{}{
		"delivered": agent.DeliverEditDecision(requestID, decision),
	}
}
