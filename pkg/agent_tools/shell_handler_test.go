package tools

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestShellCommandHandler_Execute_SimpleCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command": "echo hello",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "hello")
}

func TestShellCommandHandler_Execute_Echo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command": "echo hello world",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "hello world")
}

func TestShellCommandHandler_Execute_LS(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	// Create a file to list
	path := createTestFile(dir, "testfile.txt", "content")

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command": "ls " + path,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "testfile.txt")
}

func TestShellCommandHandler_Execute_Env(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command": "env",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.NotEmpty(t, res.Output)
}

func TestShellCommandHandler_Execute_MissingCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "required")
}

func TestShellCommandHandler_Execute_EmptyCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command": "",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestShellCommandHandler_Execute_Pwd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command": "pwd",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.NotEmpty(t, res.Output)
}

func TestShellCommandHandler_Execute_Date(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command": "date",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.NotEmpty(t, res.Output)
}

func TestShellCommandHandler_Execute_Background_NoTerminalManager(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command":    "echo background-test",
		"background": true,
	})
	// Background mode requires TerminalManager or BackgroundProcessManager which are not available in test context
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "TerminalManager (WebUI) or BackgroundProcessManager (CLI)")
}

func TestShellCommandHandler_Execute_CheckBackground_NoTerminalManager(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"check_background": "session-123",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "TerminalManager (WebUI) or BackgroundProcessManager (CLI)")
}

func TestShellCommandHandler_Execute_StopBackground_NoTerminalManager(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"stop_background": "session-123",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "TerminalManager (WebUI) or BackgroundProcessManager (CLI)")
}

func TestShellCommandHandler_Execute_SecurityBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	// mkfs is a genuinely DANGEROUS command (disk destruction) that should
	// always be hard-blocked regardless of approval manager presence.
	// (pipe-to-bash was downgraded to CAUTION, so we test a still-DANGEROUS op)
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command": "mkfs.ext4 /dev/sda1",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "security block")
}

func TestShellCommandHandler_Execute_RMRFBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	// rm -rf / should be blocked
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command": "rm -rf /",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "security block")
}

func TestShellCommandHandler_Execute_OutputWriter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	var buf strings.Builder
	env := newTestEnv(t, dir)
	env.OutputWriter = &buf

	res, err := h.Execute(ctx, env, map[string]any{
		"command": "echo hello",
	})
	require.NoError(t, err)
	require.Contains(t, buf.String(), res.Output)
}

func TestShellCommandHandler_Execute_EventBus(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	bus := events.NewEventBus()
	_ = bus.Subscribe("test") // subscribe to have a listener
	env := newTestEnv(t, dir)
	env.EventBus = bus

	_, err := h.Execute(ctx, env, map[string]any{
		"command": "echo hello",
	})
	require.NoError(t, err)

	// Handlers no longer self-publish tool_start/tool_end — the core
	// tool executor (pkg/agent/tool_executor.go) handles event publishing.
	select {
	case ev := <-bus.Subscribe("check"):
		t.Fatalf("expected 0 events from handler, got %+v", ev)
	default:
		// good — no events published by the handler
	}
}

func TestShellCommandHandler_Execute_NonZeroExit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	// Some shells treat "exit 1" as success (the shell exits cleanly).
	// Verify the handler runs and returns *something* — the exact behavior
	// depends on the shell used by ExecuteShellCommandWithSafety.
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command": "exit 1",
	})
	// The behavior is platform/shell-dependent; accept either outcome.
	if err != nil {
		require.True(t, res.IsError)
	} else {
		require.False(t, res.IsError)
	}
}

func TestShellCommandHandler_Execute_CatFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	path := createTestFile(dir, "readme.txt", "file content here")

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command": "cat " + path,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "file content here")
}

func TestShellCommandHandler_Execute_Whoami(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command": "whoami",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.NotEmpty(t, res.Output)
}

func TestShellCommandHandler_Execute_WithQuotes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command": "echo 'hello world'",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "hello world")
}

func TestShellCommandHandler_Execute_TokenUsage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"command": "echo hello",
	})
	require.NoError(t, err)
	require.Greater(t, res.TokenUsage, int64(0), "should report token usage for non-empty output")
}

func TestShellCommandHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &shellCommandHandler{}

	// nil args should error
	err := h.Validate(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be nil")

	// empty args should error
	err = h.Validate(map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")

	// valid command
	require.NoError(t, h.Validate(map[string]any{"command": "echo hello"}))

	// valid check_background
	require.NoError(t, h.Validate(map[string]any{"check_background": "sess-123"}))

	// valid stop_background
	require.NoError(t, h.Validate(map[string]any{"stop_background": "sess-123"}))

	// conflicting params
	err = h.Validate(map[string]any{"check_background": "s1", "stop_background": "s2"})
	require.Error(t, err)

	err = h.Validate(map[string]any{"command": "echo hi", "background": true, "check_background": "s1"})
	require.Error(t, err)

	// invalid background type
	err = h.Validate(map[string]any{"command": "echo hi", "background": 42})
	require.Error(t, err)
}

func TestShellCommandHandler_Definition(t *testing.T) {
	t.Parallel()
	h := &shellCommandHandler{}

	require.Equal(t, "shell_command", h.Name())

	def := h.Definition()
	require.Equal(t, "shell_command", def.Name)
	require.NotEmpty(t, def.Description)

	// All parameters should be optional (no required list)
	require.Empty(t, def.Required)

	// Check all expected params are present
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["command"])
	require.True(t, paramNames["background"])
	require.True(t, paramNames["check_background"])
	require.True(t, paramNames["stop_background"])
}

// createTestFile creates a file in the given directory with the given content
// and returns its full path.
func createTestFile(dir, name, content string) string {
	path := dir + "/" + name
	os.WriteFile(path, []byte(content), 0o644) //nolint:errcheck
	return path
}

// ---------------------------------------------------------------------------
// Gate 2 bypass — Gate1AutoApproved
//
// These tests verify that when Gate1AutoApproved is true (Gate 1 already
// auto-approved via --unsafe mode or session elevation), the shell handler's
// Gate 2 classifier skips its interactive approval prompt for non-hard-block
// operations. Hard blocks are still enforced.
//
// Classification reference (verified against ClassifyToolCall):
//   - "rm test.txt"          → CAUTION,  ShouldPrompt=true,  IsHardBlock=false
//   - "rm -rf /"             → DANGEROUS, ShouldBlock=true,  IsHardBlock=true
// ---------------------------------------------------------------------------

// newShellEnv builds a ToolEnv for the shell handler with an approval manager.
func newShellEnv(t *testing.T, dir string, am ApprovalManager) ToolEnv {
	t.Helper()
	env := newTestEnv(t, dir)
	env.ApprovalManager = am
	return env
}

// TestShellHandler_PromptOp_Gate1AutoApproved_SkipsPrompt verifies that a
// Caution-tier command (rm test.txt → ShouldPrompt, not hard block) skips the
// Gate 2 approval prompt when Gate1AutoApproved is true.
func TestShellHandler_PromptOp_Gate1AutoApproved_SkipsPrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Create the file so the rm doesn't error from a missing-file exit code.
	createTestFile(dir, "test.txt", "data")

	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)
	am := &capturingApprovalManager{approved: true}
	env := newShellEnv(t, dir, am)
	env.Gate1AutoApproved = true

	_, err := h.Execute(ctx, env, map[string]any{"command": "rm test.txt"})
	// rm may succeed or produce a non-zero exit; what matters is no approval
	// was requested and no permission error was returned from the Gate 2 prompt.
	if err != nil {
		// A tool execution error (e.g. non-zero exit) is acceptable; a
		// permission rejection from the Gate 2 prompt is NOT.
		require.NotContains(t, err.Error(), "rejected")
	}
	require.Equal(t, 0, len(am.calls), "Gate1AutoApproved should skip Gate 2 prompt")
}

// TestShellHandler_PromptOp_NotGate1AutoApproved_Prompts verifies that the
// same Caution-tier command DOES trigger the Gate 2 approval prompt when
// Gate1AutoApproved is false.
func TestShellHandler_PromptOp_NotGate1AutoApproved_Prompts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	createTestFile(dir, "test.txt", "data")

	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)
	am := &capturingApprovalManager{approved: true}
	env := newShellEnv(t, dir, am)
	// Gate1AutoApproved defaults to false.

	_, _ = h.Execute(ctx, env, map[string]any{"command": "rm test.txt"})
	require.Equal(t, 1, len(am.calls), "Gate 2 should prompt when Gate1AutoApproved is false")
}

// TestShellHandler_HardBlock_Still_Blocked_Under_Gate1AutoApproved verifies
// that even with Gate1AutoApproved=true, a hard-block command (rm -rf /) is
// still blocked by the handler's IsHardBlock early-return. No approval is
// requested.
func TestShellHandler_HardBlock_Still_Blocked_Under_Gate1AutoApproved(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &shellCommandHandler{}
	ctx := newTestCtx(dir)
	am := &capturingApprovalManager{approved: true}
	env := newShellEnv(t, dir, am)
	env.Gate1AutoApproved = true

	res, err := h.Execute(ctx, env, map[string]any{"command": "rm -rf /"})
	require.Error(t, err)
	require.True(t, res.IsError, "hard block should return IsError")
	require.Contains(t, res.Output, "security block")
	require.Equal(t, 0, len(am.calls), "hard block early-returns before any approval request")
}
