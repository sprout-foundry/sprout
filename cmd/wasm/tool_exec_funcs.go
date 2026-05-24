//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/sprout-foundry/sprout/pkg/wasmshell"
)

// toolExecJSFuncs returns the JS API surface for tool execution hooks.
// These allow the JS host to intercept, audit, or modify agent tool calls.
func toolExecJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"setToolExecutionHook":   js.FuncOf(setToolExecutionHookFunc),
		"clearToolExecutionHook": js.FuncOf(clearToolExecutionHookFunc),
		"executeCommandDirect":   js.FuncOf(executeCommandDirectFunc),
	}
}

// setToolExecutionHookFunc registers a JS callback that will be called
// before every tool execution from the agent loop.
//
//	args[0]: JS function (command: string) => null | string | {stdout, stderr, exitCode}
//	Returns: {ok: true}
func setToolExecutionHookFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 || args[0].Type() != js.TypeFunction {
		return marshalJS(map[string]bool{"ok": false})
	}

	toolHookMu.Lock()
	toolExecutionHook = args[0]
	toolHookMu.Unlock()

	return marshalJS(map[string]bool{"ok": true})
}

// clearToolExecutionHookFunc removes the tool execution hook, restoring
// direct wasmshell execution for all agent tool calls.
//
//	Returns: {ok: true}
func clearToolExecutionHookFunc(this js.Value, args []js.Value) interface{} {
	toolHookMu.Lock()
	// js.Value{}.Truthy() == false, so the executor path treats this as "no hook"
	toolExecutionHook = js.Value{}
	toolHookMu.Unlock()

	return marshalJS(map[string]bool{"ok": true})
}

// executeCommandDirectFunc bypasses the hook and executes a command
// directly through wasmshell. Useful inside hooks that want to audit
// AND execute — they call SproutWasm.executeCommandDirect(cmd).
//
//	args[0]: command string
//	Returns: JSON CmdResult string
func executeCommandDirectFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 || args[0].Type() != js.TypeString {
		return wasmshell.JSONResult(wasmshell.CmdResult{
			Stderr:   "executeCommandDirect: missing or invalid argument\n",
			ExitCode: 1,
		})
	}

	input := args[0].String()
	result := wasmshell.ParseAndExecute(input)
	return wasmshell.JSONResult(result)
}
