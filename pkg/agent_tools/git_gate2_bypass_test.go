package tools

import (
	"os"
	"testing"
)

// ---------------------------------------------------------------------------
// Gate 2 bypass — Gate1AutoApproved
//
// These tests verify that when Gate1AutoApproved is true (Gate 1 already
// auto-approved via --unsafe mode or session elevation), the git handler's
// Gate 2 classifier skips its interactive approval prompt for non-hard-block
// operations. Hard blocks are still enforced.
//
// Baseline (Gate1AutoApproved=false, prompts fire) is already covered by
// the existing tests in git_handler_test.go:
//   - TestGitHandler_ResetHard_WithAM_Approved (Dangerous tier → 1 call)
//   - TestGitHandler_ResetSoft_WithAM         (Caution/dangerousOps → 1 call)
// ---------------------------------------------------------------------------

// runGitGate1 runs a git operation with explicit Gate1AutoApproved on the env.
func runGitGate1(t *testing.T, ws string, am ApprovalManager, gate1 bool, op, argsStr string) (ToolResult, error) {
	t.Helper()
	env := baseEnv(ws, am)
	env.Gate1AutoApproved = gate1
	args := map[string]any{"operation": op}
	if argsStr != "" {
		args["args"] = argsStr
	}
	return newGitHandler().Execute(t.Context(), env, args)
}

// makeSecondCommit creates a second commit in the repo so HEAD~1 resolves.
func makeSecondCommit(t *testing.T, dir string) {
	t.Helper()
	os.WriteFile(dir+"/second.go", []byte("package main"), 0o644) //nolint:errcheck
	execShell(t, dir, "git add second.go")
	execShell(t, dir, "git commit -m \"second commit\"")
}

// TestGitHandler_ResetHard_Gate1AutoApproved_SkipsPrompt verifies that a
// SecurityDangerous-tier operation (reset --hard) skips the Gate 2 prompt
// when Gate1AutoApproved is true, and the reset actually executes.
func TestGitHandler_ResetHard_Gate1AutoApproved_SkipsPrompt(t *testing.T) {
	ws := t.TempDir()
	initGitRepo(t, ws)
	makeInitialCommit(t, ws)
	makeSecondCommit(t, ws)

	am := &capturingApprovalManager{approved: true}
	result, err := runGitGate1(t, ws, am, true, "reset", "--hard HEAD~1")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("reset should succeed when Gate 1 auto-approved: %s", result.Output)
	}
	if got := len(am.calls); got != 0 {
		t.Fatalf("Gate1AutoApproved should skip prompt, got %d calls", got)
	}
}

// TestGitHandler_CautionOp_Gate1AutoApproved_SkipsPrompt verifies that a
// Caution-tier dangerousOps entry (plain reset) skips the Gate 2 prompt
// when Gate1AutoApproved is true.
func TestGitHandler_CautionOp_Gate1AutoApproved_SkipsPrompt(t *testing.T) {
	ws := t.TempDir()
	initGitRepo(t, ws)
	makeInitialCommit(t, ws)

	am := &capturingApprovalManager{approved: true}
	result, err := runGitGate1(t, ws, am, true, "reset", "")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("plain reset should succeed when Gate 1 auto-approved: %s", result.Output)
	}
	if got := len(am.calls); got != 0 {
		t.Fatalf("Gate1AutoApproved should skip Caution prompt, got %d calls", got)
	}
}
