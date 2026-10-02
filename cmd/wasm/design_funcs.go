//go:build js

package main

// design_funcs.go — the design health surface for the WASM shell
// (SP-140-6 §6b). The design scanners are pure Go, so the hosted editor can
// serve GET /api/design/status from the same truth the agent tools read —
// no second validation implementation in the browser.

import (
	"encoding/json"

	"github.com/sprout-foundry/sprout/pkg/design"
	"syscall/js"
)

// designStatusFunc returns the §6b design status for the workspace root as a
// JSON string (the cloudWasmHandlers /api/design/status body), or an error
// string when the workspace has no root.
func designStatusFunc(_ js.Value, args []js.Value) interface{} {
	root := workspaceRoot
	if len(args) > 0 && args[0].Type() == js.TypeString && args[0].String() != "" {
		root = args[0].String()
	}
	status := design.BuildDesignStatus(root, nil)
	data, err := json.Marshal(status)
	if err != nil {
		return "design status: " + err.Error()
	}
	return string(data)
}

func designJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"designStatus": designStatusFunc,
	}
}
