//go:build js && wasm

package main

import (
	"sync"
	"syscall/js"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/wasmshell"
)

// toolExecutionHook is a JS callback that can intercept tool execution.
// When set (truthy), the agent loop's shell executor calls it before falling
// through to wasmshell. The hook can return null/undefined to allow normal
// execution, a string to reject, or an object {stdout, stderr, exitCode} to
// provide a custom result. Protected by toolHookMu for concurrent access.
var (
	toolExecutionHook js.Value
	toolHookMu        sync.RWMutex
)

// Bridge between pkg/agent_tools' WASM-specific shell hook and the
// in-browser wasmshell. Installing this at init() means the moment
// runAgent decides to run a shell tool, the call lands on wasmshell
// rather than the unconfigured-WASM error path.
//
// We register from cmd/wasm — not pkg/agent_tools or pkg/wasmshell — so
// the dependency direction stays clean: pkg/agent_tools doesn't know
// about wasmshell, and pkg/wasmshell doesn't know about agent_tools.
// cmd/wasm is the integration layer where the two meet.

func init() {
	tools.RegisterWASMShellExecutor(func(command string) (stdout, stderr string, exitCode int) {
		toolHookMu.RLock()
		hook := toolExecutionHook
		toolHookMu.RUnlock()

		if hook.Truthy() {
			result := hook.Invoke(command)

			if result.IsNull() || result.IsUndefined() {
				// Hook allows — fall through to normal execution
			} else if result.Type() == js.TypeString {
				// Hook rejected the command with an error message
				return "", result.String(), 1
			} else if result.Type() == js.TypeObject {
				// Hook provided a custom result. Validate field types
				// to avoid silent empty results from malformed hooks.
				stdoutVal := result.Get("stdout")
				stderrVal := result.Get("stderr")
				exitCodeVal := result.Get("exitCode")

				if stdoutVal.Type() != js.TypeString {
					return "", "hook returned invalid result: stdout must be a string", 1
				}
				if stderrVal.Type() != js.TypeString {
					return "", "hook returned invalid result: stderr must be a string", 1
				}
				if exitCodeVal.Type() != js.TypeNumber {
					return "", "hook returned invalid result: exitCode must be a number", 1
				}

				return stdoutVal.String(), stderrVal.String(), exitCodeVal.Int()
			}
		}

		r := wasmshell.ParseAndExecute(command)
		return r.Stdout, r.Stderr, r.ExitCode
	})
}
