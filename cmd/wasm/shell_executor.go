//go:build js && wasm

package main

import (
	"strings"
	"sync"
	"syscall/js"
	"time"

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
		// Intercept gittool: commands — dispatch to browser-side async git tools.
		// This path uses the channel-blocking pattern (same as asPromise) to
		// wait for the JS Promise from globalThis.__sproutGitTools.execute().
		if strings.HasPrefix(command, "gittool:") {
			return callGitToolJS(command)
		}

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

// callGitToolJS dispatches a "gittool:<name> <json>" command to the
// browser-side async git tools via globalThis.__sproutGitTools.execute().
// Blocks until the Promise resolves (or 30s timeout) using the channel
// pattern — the same approach as asPromise for bridging Go goroutines
// with JS Promises.
func callGitToolJS(command string) (stdout, stderr string, exitCode int) {
	gitTools := js.Global().Get("__sproutGitTools")
	if !gitTools.Truthy() {
		return "", "git tools not registered: call registerGitToolGlobal() first", 1
	}

	// Parse "gittool:<name> <json>" — name is between prefix and first space
	rest := strings.TrimPrefix(command, "gittool:")
	spaceIdx := strings.IndexAny(rest, " \t")
	var toolName, argsStr string
	if spaceIdx == -1 {
		toolName = strings.TrimSpace(rest)
		argsStr = "{}"
	} else {
		toolName = strings.TrimSpace(rest[:spaceIdx])
		argsStr = strings.TrimSpace(rest[spaceIdx:])
	}
	if toolName == "" {
		return "", "gittool: empty tool name", 1
	}
	if argsStr == "" {
		argsStr = "{}"
	}

	// Parse args JSON → JS object
	argsObj := js.Global().Get("JSON").Call("parse", argsStr)

	// Call execute() → returns a Promise<string>
	promise := gitTools.Call("execute", toolName, argsObj)

	// Block on the Promise via callbacks writing to channels.
	resultCh := make(chan string, 1)
	errCh := make(chan string, 1)

	then := js.FuncOf(func(_ js.Value, pargs []js.Value) interface{} {
		if len(pargs) > 0 {
			resultCh <- pargs[0].String()
		} else {
			resultCh <- ""
		}
		return nil
	})
	catch := js.FuncOf(func(_ js.Value, pargs []js.Value) interface{} {
		if len(pargs) > 0 {
			errCh <- pargs[0].String()
		} else {
			errCh <- "unknown error"
		}
		return nil
	})
	defer then.Release()
	defer catch.Release()
	promise.Call("then", then, catch)

	select {
	case result := <-resultCh:
		return result, "", 0
	case errMsg := <-errCh:
		return "", "git tool error: " + errMsg, 1
	case <-time.After(30 * time.Second):
		return "", "git tool timeout (30s)", 1
	}
}
