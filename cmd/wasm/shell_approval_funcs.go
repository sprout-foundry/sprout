//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

func shellApprovalJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"respondToShellApproval": js.FuncOf(respondToShellApprovalFunc),
	}
}

// respondToShellApprovalFunc is the JS bridge wrapper around
// agent.DeliverShellDecision.  Called from the webui's
// ShellApprovalPanel in cloud/WASM mode to deliver the user's
// per-part accept/reject decision back to the blocking agent.
//
// Arguments:
//   1. requestID — string  (the shell approval request ID)
//   2. decisions — object  { partID: boolean } (per-part accept/reject)
//
// Returns { delivered: bool }.
func respondToShellApprovalFunc(_ js.Value, args []js.Value) interface{} {
	requestID := argString(args, 0, "")
	decisions := argBoolMap(args, 1)

	return map[string]interface{}{
		"delivered": agent.DeliverShellDecision(requestID, decisions),
	}
}

// argBoolMap reads a positional JS object argument and converts it to a
// map[string]bool. Returns an empty map for missing/non-object arguments.
func argBoolMap(args []js.Value, idx int) map[string]bool {
	if idx >= len(args) || args[idx].IsUndefined() || args[idx].IsNull() {
		return map[string]bool{}
	}
	v := args[idx]
	if v.Type() != js.TypeObject {
		return map[string]bool{}
	}
	keys := js.Global().Get("Object").Call("keys", v)
	if keys.IsUndefined() {
		return map[string]bool{}
	}
	length := keys.Get("length").Int()
	result := make(map[string]bool, length)
	for i := 0; i < length; i++ {
		key := keys.Index(i).String()
		val := v.Get(key)
		if val.Type() == js.TypeBoolean {
			result[key] = val.Bool()
		}
	}
	return result
}
