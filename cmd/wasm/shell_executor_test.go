//go:build js && wasm

// Integration test for the WASM shell executor wiring: cmd/wasm's init()
// registers a wasmshell-backed implementation into pkg/agent_tools. This
// test exercises the bridge by invoking ExecuteShellCommand against a
// known wasmshell builtin (echo) and checking the captured output.
//
// This pins SP-045-4e: the agent's run_shell_command tool now produces
// real output inside MEMFS under WASM instead of failing with the
// "executor not registered" sentinel.

package main

import (
	"context"
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

func TestWASMShellExecutor_EchoBuiltin(t *testing.T) {
	out, err := tools.ExecuteShellCommand(context.Background(), "echo hello world")
	if err != nil {
		t.Fatalf("ExecuteShellCommand returned unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected echo output to contain 'hello world', got: %q", out)
	}
}

func TestWASMShellExecutor_NonzeroExitFormatted(t *testing.T) {
	// `cat /no/such/path` errors via the wasmshell builtin — exit code
	// non-zero, but the function should still return without an error
	// (matches native silent-mode semantics: command failures are normal
	// outcomes, not tool failures).
	out, err := tools.ExecuteShellCommand(context.Background(), "cat /no/such/path")
	if err != nil {
		t.Fatalf("ExecuteShellCommand returned unexpected error: %v", err)
	}
	// We just want to verify SOMETHING came back (stderr from cat). The
	// exact wasmshell error message format isn't part of this test's
	// contract; what matters is that the executor wired through.
	if out == "" {
		t.Error("expected non-empty output for failing cat command, got empty string")
	}
}

func TestWASMShellExecutor_RegisteredAtInit(t *testing.T) {
	// The shell_executor.go init() function should have registered the
	// executor before tests run. If this test fails, the init wiring
	// regressed — likely shell_executor.go got accidentally tagged out
	// of the WASM build.
	out, err := tools.ExecuteShellCommand(context.Background(), "pwd")
	if err != nil {
		t.Fatalf("ExecuteShellCommand returned unexpected error (executor missing?): %v", err)
	}
	if strings.Contains(out, "shell execution unavailable in WASM") {
		t.Errorf("executor not registered — got the unconfigured-WASM error path: %q", out)
	}
}
