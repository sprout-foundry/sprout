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
	"encoding/json"
	"strings"
	"testing"
	"syscall/js"

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

// --- Tool Execution Hook Tests (SP-045-Tier2b-toolExec) ---

func TestWASMShellExecutor_HookNotSet_FallsThrough(t *testing.T) {
	// With no hook registered (the default state), commands execute
	// normally through wasmshell. This verifies the baseline path.
	out, err := tools.ExecuteShellCommand(context.Background(), "echo no-hook")
	if err != nil {
		t.Fatalf("ExecuteShellCommand returned unexpected error: %v", err)
	}
	if !strings.Contains(out, "no-hook") {
		t.Errorf("expected echo output to contain 'no-hook', got: %q", out)
	}
}

func TestWASMShellExecutor_HookReturnsNull_FallsThrough(t *testing.T) {
	// A hook that returns js.Null() signals "allow normal execution".
	hook := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		return js.Null()
	})
	setToolExecutionHookFunc(js.Undefined(), []js.Value{hook.Value})
	defer clearToolExecutionHookFunc(js.Undefined(), nil)

	out, err := tools.ExecuteShellCommand(context.Background(), "echo null-hook")
	if err != nil {
		t.Fatalf("ExecuteShellCommand returned unexpected error: %v", err)
	}
	if !strings.Contains(out, "null-hook") {
		t.Errorf("expected echo output to contain 'null-hook' (hook should fall through), got: %q", out)
	}
}

func TestWASMShellExecutor_HookReturnsUndefined_FallsThrough(t *testing.T) {
	// A hook that returns js.Undefined() also signals "allow normal execution".
	hook := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		return js.Undefined()
	})
	setToolExecutionHookFunc(js.Undefined(), []js.Value{hook.Value})
	defer clearToolExecutionHookFunc(js.Undefined(), nil)

	out, err := tools.ExecuteShellCommand(context.Background(), "echo undef-hook")
	if err != nil {
		t.Fatalf("ExecuteShellCommand returned unexpected error: %v", err)
	}
	if !strings.Contains(out, "undef-hook") {
		t.Errorf("expected echo output to contain 'undef-hook' (hook should fall through), got: %q", out)
	}
}

func TestWASMShellExecutor_HookReturnsString_Rejection(t *testing.T) {
	// A hook that returns a string rejects the command — the string
	// becomes the stderr output and the command is NOT executed.
	rejectionMsg := "command rejected by policy"
	hook := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		return js.ValueOf(rejectionMsg)
	})
	setToolExecutionHookFunc(js.Undefined(), []js.Value{hook.Value})
	defer clearToolExecutionHookFunc(js.Undefined(), nil)

	out, err := tools.ExecuteShellCommand(context.Background(), "echo should-not-run")
	if err != nil {
		t.Fatalf("ExecuteShellCommand returned unexpected error: %v", err)
	}
	if !strings.Contains(out, rejectionMsg) {
		t.Errorf("expected output to contain rejection message %q, got: %q", rejectionMsg, out)
	}
	// The original command should NOT have produced its normal output
	if strings.Contains(out, "should-not-run") {
		t.Errorf("command was executed despite hook rejection — output should not contain 'should-not-run': %q", out)
	}
}

func TestWASMShellExecutor_HookReturnsObject_CustomResult(t *testing.T) {
	// A hook that returns an object {stdout, stderr, exitCode} provides
	// a custom result without executing the actual command.
	customOut := "custom out from hook"
	hook := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		obj := js.Global().Get("Object").New()
		obj.Set("stdout", customOut)
		obj.Set("stderr", "")
		obj.Set("exitCode", 0)
		return obj
	})
	setToolExecutionHookFunc(js.Undefined(), []js.Value{hook.Value})
	defer clearToolExecutionHookFunc(js.Undefined(), nil)

	out, err := tools.ExecuteShellCommand(context.Background(), "echo should-not-run")
	if err != nil {
		t.Fatalf("ExecuteShellCommand returned unexpected error: %v", err)
	}
	if !strings.Contains(out, customOut) {
		t.Errorf("expected output to contain custom stdout %q, got: %q", customOut, out)
	}
	// The original command should NOT have produced its normal output
	if strings.Contains(out, "should-not-run") {
		t.Errorf("command was executed despite hook returning custom result — output should not contain 'should-not-run': %q", out)
	}
}

func TestToolExecJSFuncs_ClearHook(t *testing.T) {
	// Set a rejecting hook, then clear it — commands should execute
	// normally again after the clear.
	rejectHook := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		return js.ValueOf("blocked")
	})
	setToolExecutionHookFunc(js.Undefined(), []js.Value{rejectHook.Value})

	// Verify the hook is active — command should be rejected
	out, err := tools.ExecuteShellCommand(context.Background(), "echo blocked")
	if err != nil {
		t.Fatalf("ExecuteShellCommand returned unexpected error: %v", err)
	}
	if !strings.Contains(out, "blocked") {
		t.Errorf("expected hook to block command, got: %q", out)
	}

	// Clear the hook
	clearToolExecutionHookFunc(js.Undefined(), nil)

	// Command should now execute normally
	out, err = tools.ExecuteShellCommand(context.Background(), "echo unblocked")
	if err != nil {
		t.Fatalf("ExecuteShellCommand returned unexpected error after clear: %v", err)
	}
	if !strings.Contains(out, "unblocked") {
		t.Errorf("expected echo output after clear to contain 'unblocked', got: %q", out)
	}
}

func TestExecuteCommandDirectFunc_BypassesHook(t *testing.T) {
	// With a rejecting hook set, executeCommandDirectFunc should
	// bypass the hook and execute the command directly.
	rejectHook := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		return js.ValueOf("blocked")
	})
	setToolExecutionHookFunc(js.Undefined(), []js.Value{rejectHook.Value})
	defer clearToolExecutionHookFunc(js.Undefined(), nil)

	// Call executeCommandDirectFunc directly — it bypasses the hook
	result := executeCommandDirectFunc(js.Undefined(), []js.Value{js.ValueOf("echo direct-bypass")})

	// The result is a JSON string — parse it to check the content
	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("executeCommandDirectFunc returned non-string: %T", result)
	}
	var cmdRes struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exitCode"`
	}
	if err := json.Unmarshal([]byte(resultStr), &cmdRes); err != nil {
		t.Fatalf("failed to parse JSON result: %v", err)
	}
	if !strings.Contains(cmdRes.Stdout, "direct-bypass") {
		t.Errorf("expected stdout to contain 'direct-bypass', got: %q", cmdRes.Stdout)
	}
	if cmdRes.ExitCode != 0 {
		t.Errorf("expected exit code 0, got: %d", cmdRes.ExitCode)
	}
}

func TestExecuteCommandDirectFunc_MissingArg(t *testing.T) {
	// Calling executeCommandDirectFunc with no arguments should return
	// an error result with a descriptive message.
	result := executeCommandDirectFunc(js.Undefined(), []js.Value{})

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("executeCommandDirectFunc returned non-string: %T", result)
	}
	var cmdRes struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exitCode"`
	}
	if err := json.Unmarshal([]byte(resultStr), &cmdRes); err != nil {
		t.Fatalf("failed to parse JSON result: %v", err)
	}
	if cmdRes.ExitCode == 0 {
		t.Errorf("expected non-zero exit code for missing arg, got 0")
	}
	if !strings.Contains(cmdRes.Stderr, "missing") && !strings.Contains(cmdRes.Stderr, "invalid") {
		t.Errorf("expected stderr to mention missing/invalid argument, got: %q", cmdRes.Stderr)
	}
}

func TestSetToolExecutionHook_NonFunctionArg(t *testing.T) {
	// Calling setToolExecutionHookFunc with a non-function argument
	// should return {ok: false} and not change the hook.
	result := setToolExecutionHookFunc(js.Undefined(), []js.Value{js.ValueOf("not a function")})

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("setToolExecutionHookFunc returned non-string: %T", result)
	}
	var resMap map[string]bool
	if err := json.Unmarshal([]byte(resultStr), &resMap); err != nil {
		t.Fatalf("failed to parse JSON result: %v", err)
	}
	if resMap["ok"] {
		t.Error("expected ok: false when setting hook with non-function argument")
	}
}
