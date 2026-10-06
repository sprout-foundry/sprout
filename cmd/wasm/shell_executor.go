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

		r := fromWorkspace(func() wasmshell.CmdResult { return wasmshell.ParseAndExecute(command) })
		if r.ExitCode == wasmshell.ExitCommandNotFound {
			return escalateUnavailableCommand(command, r)
		}
		return r.Stdout, r.Stderr, r.ExitCode
	})

	// Back the wasmshell "git" command with browser-side isomorphic-git.
	// wasmshell's cmdGit answers the read-only and local/remote write
	// subcommands; everything else stays a 127 so the escalation surface can
	// take it to a container.
	wasmshell.RegisterGitExecutor(func(subcommand string, args []string) wasmshell.CmdResult {
		return callShellGitJS(subcommand, args)
	})

	// Back the wasmshell "gh" command with browser-side GitHub support
	// (isomorphic-git clone/checkout + GitHub REST). Subcommands the adapter
	// doesn't implement stay a 127 so the escalation surface takes them to a
	// container.
	wasmshell.RegisterGhExecutor(func(subcommand string, args []string) wasmshell.CmdResult {
		return callShellGhJS(subcommand, args)
	})
}

// callShellGitJS runs one read-only git subcommand via
// globalThis.__sproutShellGit.execute(subcommand, args), installed by the
// webui on top of browserGit (isomorphic-git). Blocks until the Promise
// resolves or the timeout elapses, mirroring callGitToolJS.
func callShellGitJS(subcommand string, args []string) wasmshell.CmdResult {
	shellGit := js.Global().Get("__sproutShellGit")
	if !shellGit.Truthy() {
		return wasmshell.CmdResult{Stdout: "", Stderr: "git: browser git bridge not registered\n", ExitCode: 127}
	}

	promise := shellGit.Call("execute", subcommand, stringSliceToJS(args))

	resultCh := make(chan wasmshell.CmdResult, 1)
	errCh := make(chan string, 1)

	then := js.FuncOf(func(_ js.Value, pargs []js.Value) interface{} {
		if len(pargs) > 0 {
			resultCh <- shellGitResultFromJS(pargs[0])
		} else {
			resultCh <- wasmshell.CmdResult{Stdout: "", Stderr: "", ExitCode: 0}
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
		return result
	case errMsg := <-errCh:
		return wasmshell.CmdResult{Stdout: "", Stderr: "git: " + errMsg + "\n", ExitCode: 1}
	case <-time.After(30 * time.Second):
		return wasmshell.CmdResult{Stdout: "", Stderr: "git: timeout (30s)\n", ExitCode: 1}
	}
}

// callShellGhJS runs one gh subcommand via
// globalThis.__sproutShellGh.execute(subcommand, args), installed by the
// webui on top of browserGit (isomorphic-git) + GitHub REST. Blocks until
// the Promise resolves or the timeout elapses, mirroring callShellGitJS.
func callShellGhJS(subcommand string, args []string) wasmshell.CmdResult {
	shellGh := js.Global().Get("__sproutShellGh")
	if !shellGh.Truthy() {
		return wasmshell.CmdResult{Stdout: "", Stderr: "gh: browser GitHub bridge not registered\n", ExitCode: 127}
	}

	promise := shellGh.Call("execute", subcommand, stringSliceToJS(args))

	resultCh := make(chan wasmshell.CmdResult, 1)
	errCh := make(chan string, 1)

	then := js.FuncOf(func(_ js.Value, pargs []js.Value) interface{} {
		if len(pargs) > 0 {
			resultCh <- shellGitResultFromJS(pargs[0])
		} else {
			resultCh <- wasmshell.CmdResult{Stdout: "", Stderr: "", ExitCode: 0}
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
		return result
	case errMsg := <-errCh:
		return wasmshell.CmdResult{Stdout: "", Stderr: "gh: " + errMsg + "\n", ExitCode: 1}
	case <-time.After(30 * time.Second):
		return wasmshell.CmdResult{Stdout: "", Stderr: "gh: timeout (30s)\n", ExitCode: 1}
	}
}

// shellGitResultFromJS reads {stdout, stderr, exitCode} from a resolved
// __sproutShellGit / __sproutShellGh result object, defaulting missing fields
// to zero values.
func shellGitResultFromJS(obj js.Value) wasmshell.CmdResult {
	if obj.Type() != js.TypeObject {
		return wasmshell.CmdResult{Stdout: "", Stderr: "", ExitCode: 0}
	}
	stdout := ""
	if v := obj.Get("stdout"); v.Type() == js.TypeString {
		stdout = v.String()
	}
	stderr := ""
	if v := obj.Get("stderr"); v.Type() == js.TypeString {
		stderr = v.String()
	}
	exitCode := 0
	if v := obj.Get("exitCode"); v.Type() == js.TypeNumber {
		exitCode = v.Int()
	}
	return wasmshell.CmdResult{Stdout: stdout, Stderr: stderr, ExitCode: exitCode}
}

// stringSliceToJS converts a []string into a JS array of strings.
func stringSliceToJS(items []string) js.Value {
	arr := js.Global().Get("Array").New(len(items))
	for i, s := range items {
		arr.SetIndex(i, js.ValueOf(s))
	}
	return arr
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

// escalationTimeout bounds one escalated command end to end: starting the
// workspace, pushing files, the run itself (up to 10 minutes), and pulling
// results back.
const escalationTimeout = 15 * time.Minute

// escalateUnavailableCommand handles an agent shell command the browser shell
// can't run (exit 127). When the webui has installed the escalation bridge
// (globalThis.__sproutEscalate, cloud mode) it may run the command in the
// user's cloud workspace — subject to the user's escalation policy — and the
// real result is returned. Otherwise the 127 stands, with an explanation the
// model can act on instead of a bare "command not found".
//
// Only the agent loop reaches this: runAgent runs in its own goroutine, so
// blocking on the bridge's Promise doesn't stall the JS event loop.
func escalateUnavailableCommand(command string, local wasmshell.CmdResult) (stdout, stderr string, exitCode int) {
	bridge := js.Global().Get("__sproutEscalate")
	if !bridge.Truthy() {
		return local.Stdout, local.Stderr + browserShellNote, local.ExitCode
	}

	resultCh := make(chan js.Value, 1)
	errCh := make(chan string, 1)
	then := js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		if len(args) > 0 {
			resultCh <- args[0]
		} else {
			resultCh <- js.Undefined()
		}
		return nil
	})
	catch := js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		if len(args) > 0 {
			errCh <- args[0].String()
		} else {
			errCh <- "unknown error"
		}
		return nil
	})
	defer then.Release()
	defer catch.Release()
	bridge.Call("run", command).Call("then", then, catch)

	select {
	case res := <-resultCh:
		if res.Type() != js.TypeObject {
			return local.Stdout, local.Stderr + browserShellNote, local.ExitCode
		}
		if ran := res.Get("ran"); ran.Type() == js.TypeBoolean && ran.Bool() {
			r := shellGitResultFromJS(res)
			return r.Stdout, r.Stderr, r.ExitCode
		}
		// Not run (declined, no workspace, no repository): say why.
		note := browserShellNote
		if m := res.Get("message"); m.Type() == js.TypeString && m.String() != "" {
			note = "\n[sprout] " + m.String() + "\n"
		}
		return local.Stdout, local.Stderr + note, local.ExitCode
	case errMsg := <-errCh:
		return local.Stdout, local.Stderr + "\n[sprout] Running this in the cloud workspace failed: " + errMsg + "\n", local.ExitCode
	case <-time.After(escalationTimeout):
		return local.Stdout, local.Stderr + "\n[sprout] The cloud workspace run timed out.\n", 124
	}
}

// browserShellNote explains a 127 from the in-browser shell to the model.
const browserShellNote = "\n[sprout] This command isn't available in the in-browser shell, which runs " +
	"shell scripts and built-in file and text tools but no compilers, package managers, interpreters or " +
	"network tools. It needs a cloud workspace to run.\n"
