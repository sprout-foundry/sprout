//go:build js && wasm

package main

import (
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/wasmshell"
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
		r := wasmshell.ParseAndExecute(command)
		return r.Stdout, r.Stderr, r.ExitCode
	})
}
