//go:build js && wasm

package tools

import (
	"context"
	"fmt"
	"strings"
)

// WASMShellExecutor is the type of the function used to execute shell
// commands under js/wasm. The cmd/wasm package installs an implementation
// at init time that routes through pkg/wasmshell.ParseAndExecute.
//
// We use a package-level function variable rather than importing wasmshell
// directly to keep the dependency direction clean — pkg/agent_tools is a
// foundational package that should not pull in WASM-specific layers.
type WASMShellExecutor func(command string) (stdout, stderr string, exitCode int)

// wasmShellExec holds the installed executor. Nil until the WASM bootstrap
// calls RegisterWASMShellExecutor; an unconfigured WASM build will return a
// clear error from runShellCommand rather than silently no-opping.
var wasmShellExec WASMShellExecutor

// RegisterWASMShellExecutor installs the function that runShellCommand uses
// to execute commands under js/wasm. The cmd/wasm bootstrap calls this in
// an init() with a wasmshell-backed implementation.
//
// Subsequent calls replace the previous executor — host pages that swap
// in a stubbed executor for testing can do so safely.
func RegisterWASMShellExecutor(fn WASMShellExecutor) {
	wasmShellExec = fn
}

// runShellCommand under js/wasm routes through whatever WASMShellExecutor
// was registered. Streaming is ignored — wasmshell is a single-shot
// command runner with no streaming surface today. The captured output is
// returned as the final result, matching the native silent-mode shape.
func runShellCommand(_ context.Context, command string, _ bool) (string, error) {
	if wasmShellExec == nil {
		return "", fmt.Errorf("shell execution unavailable in WASM: no executor registered (sprout-foundry bug — cmd/wasm should call tools.RegisterWASMShellExecutor in init)")
	}

	stdout, stderr, exitCode := wasmShellExec(command)
	combined := stdout
	if stderr != "" {
		if combined != "" && !strings.HasSuffix(combined, "\n") {
			combined += "\n"
		}
		combined += stderr
	}

	// Truncated-preview echo skipped under WASM — there's no tty to print to,
	// and JS-side observability is wired through the agent event bus.
	finalOutput := buildShellOutputWithStatus(combined, command, exitCode, nil)
	return finalOutput, nil
}
