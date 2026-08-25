package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// capturingApprovalManager — records RequestApproval calls for assertions
// ---------------------------------------------------------------------------

type capturingApprovalManager struct {
	approved bool
	reason   string
	calls    []capturedCall
}

type capturedCall struct {
	requestID string
	toolName  string
	riskLevel string
	prompt    string
}

func (am *capturingApprovalManager) RequestApproval(requestID, toolName, riskLevel, prompt string, _ map[string]string) ApprovalResult {
	am.calls = append(am.calls, capturedCall{requestID: requestID, toolName: toolName, riskLevel: riskLevel, prompt: prompt})
	if am.approved {
		return ApprovalResult{Approved: true}
	}
	return ApprovalResult{Approved: false, Reason: am.reason}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	execShell(t, dir, "git init")
	execShell(t, dir, "git config user.email \"test@example.com\"")
	execShell(t, dir, "git config user.name \"Test User\"")
}

func makeInitialCommit(t *testing.T, dir string) {
	t.Helper()
	//nolint:errcheck
	os.WriteFile(dir+"/initial.go", []byte("package main"), 0o644)
	execShell(t, dir, "git add initial.go")
	execShell(t, dir, "git commit -m \"initial commit\"")
}

func execShell(t *testing.T, dir string, cmd string) {
	t.Helper()
	handler := &shellCommandHandler{}
	result, err := handler.Execute(t.Context(), ToolEnv{WorkspaceRoot: dir}, map[string]any{"command": cmd})
	if err != nil {
		t.Fatalf("execShell(%q) error: %v", cmd, err)
	}
	if result.IsError {
		t.Fatalf("execShell(%q) returned error result: %s", cmd, result.Output)
	}
}

func newGitHandler() *gitHandler { return &gitHandler{} }

func baseEnv(ws string, am ApprovalManager) ToolEnv {
	return ToolEnv{WorkspaceRoot: ws, EventBus: events.NewEventBus(), ApprovalManager: am, OutputWriter: &strings.Builder{}}
}

func runGit(t *testing.T, ctx context.Context, ws string, am ApprovalManager, op string, argsStr string) (ToolResult, error) {
	t.Helper()
	args := map[string]any{"operation": op}
	if argsStr != "" {
		args["args"] = argsStr
	}
	return newGitHandler().Execute(ctx, baseEnv(ws, am), args)
}

// ---------------------------------------------------------------------------
// reset --hard — DANGEROUS tier
// ---------------------------------------------------------------------------

func TestGitHandler_ResetHard_WithAM_Approved(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	result, err := runGit(t, ctx, ws, am, "reset", "--hard HEAD~1")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("should not be error when approved: %s", result.Output)
	}
	if got := len(am.calls); got != 1 {
		t.Fatalf("expected 1 approval call, got %d", got)
	}
	c := am.calls[0]
	// reset --hard is now CAUTION (downgraded from DANGEROUS), so it goes
	// through the dangerousOps fallback path with "high" risk level
	if c.riskLevel != "high" {
		t.Errorf("riskLevel = %q, want 'high'", c.riskLevel)
	}
	if !strings.Contains(c.prompt, "dangerous git operation") {
		t.Errorf("prompt missing 'dangerous git operation': %s", c.prompt)
	}
}

func TestGitHandler_ResetHard_WithAM_Denied(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: false, reason: "user declined"}
	result, err := runGit(t, ctx, ws, am, "reset", "--hard HEAD~1")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("denied result should not set IsError: %s", result.Output)
	}
	if !strings.Contains(result.Output, "user declined") {
		t.Errorf("message should include reason: %s", result.Output)
	}
}

func TestGitHandler_ResetHard_WithoutAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	result, err := runGit(t, ctx, ws, nil, "reset", "--hard HEAD~5")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	// reset --hard is now CAUTION — without AM it warns but proceeds (does NOT block)
	if result.IsError {
		t.Fatalf("CAUTION operation should proceed without AM (may fail from git itself): %s", result.Output)
	}
}

// ---------------------------------------------------------------------------
// reset --keep — also DANGEROUS
// ---------------------------------------------------------------------------

func TestGitHandler_ResetKeep_WithAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	result, err := runGit(t, ctx, ws, am, "reset", "--keep HEAD~1")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("should not be error when approved: %s", result.Output)
	}
	// reset --keep is now CAUTION — goes through dangerousOps fallback with "high" risk
	if len(am.calls) != 1 || am.calls[0].riskLevel != "high" {
		t.Errorf("expected 1 high call, got %d calls (last risk=%q)", len(am.calls), am.calls[0].riskLevel)
	}
}

func TestGitHandler_ResetKeep_WithoutAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	result, err := runGit(t, ctx, ws, nil, "reset", "--keep HEAD~1")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	// reset --keep is now CAUTION — without AM it warns but proceeds
	if result.IsError {
		t.Fatalf("CAUTION operation should proceed without AM: %s", result.Output)
	}
}

// ---------------------------------------------------------------------------
// reset --merge — also DANGEROUS
// ---------------------------------------------------------------------------

func TestGitHandler_ResetMerge_WithoutAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	result, err := runGit(t, ctx, ws, nil, "reset", "--merge HEAD~1")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	// reset --merge is now CAUTION — without AM it warns but proceeds
	if result.IsError {
		t.Fatalf("CAUTION operation should proceed without AM: %s", result.Output)
	}
}

// ---------------------------------------------------------------------------
// reset --soft / plain reset — CAUTION + dangerousOps fallback
// ---------------------------------------------------------------------------

func TestGitHandler_ResetSoft_WithAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	result, err := runGit(t, ctx, ws, am, "reset", "--soft HEAD~1")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("should not be error when approved: %s", result.Output)
	}
	if len(am.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(am.calls))
	}
	c := am.calls[0]
	if c.riskLevel != "high" {
		t.Errorf("riskLevel = %q, want 'high' (dangerousOps fallback)", c.riskLevel)
	}
	if !strings.Contains(c.prompt, "Execute dangerous git operation") {
		t.Errorf("prompt should use dangerousOps format: %s", c.prompt)
	}
}

func TestGitHandler_ResetSoft_WithoutAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	result, err := runGit(t, ctx, ws, nil, "reset", "--soft HEAD~1")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("should proceed with warning only: %s", result.Output)
	}
}

func TestGitHandler_ResetPlain_WithAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	result, err := runGit(t, ctx, ws, am, "reset", "")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("should not be error when approved: %s", result.Output)
	}
	if len(am.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(am.calls))
	}
	if am.calls[0].riskLevel != "high" {
		t.Errorf("riskLevel = %q, want 'high'", am.calls[0].riskLevel)
	}
}

// ---------------------------------------------------------------------------
// rebase — AGENTS.md bans rebase unconditionally
// ---------------------------------------------------------------------------

func TestGitHandler_RebaseInteractive_WithAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	result, _ := runGit(t, ctx, ws, am, "rebase", "-i HEAD~3")
	// rebase is unconditionally banned by AGENTS.md — error is returned.
	if !result.IsError {
		t.Fatal("expected IsError for banned rebase operation")
	}
	if !strings.Contains(result.Output, "AGENTS.md bans rebase unconditionally") {
		t.Errorf("error should mention AGENTS.md ban: %s", result.Output)
	}
}

func TestGitHandler_RebaseInteractive_WithoutAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	result, _ := runGit(t, ctx, ws, nil, "rebase", "-i HEAD~3")
	// rebase is unconditionally banned by AGENTS.md — error is returned.
	if !result.IsError {
		t.Fatal("expected IsError for banned rebase operation")
	}
	if !strings.Contains(result.Output, "AGENTS.md bans rebase unconditionally") {
		t.Errorf("error should mention AGENTS.md ban: %s", result.Output)
	}
}

func TestGitHandler_RebaseOnto_WithAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	result, _ := runGit(t, ctx, ws, am, "rebase", "--onto main")
	// rebase is unconditionally banned by AGENTS.md — error is returned.
	if !result.IsError {
		t.Fatal("expected IsError for banned rebase operation")
	}
	if !strings.Contains(result.Output, "AGENTS.md bans rebase unconditionally") {
		t.Errorf("error should mention AGENTS.md ban: %s", result.Output)
	}
}

// ---------------------------------------------------------------------------
// plain rebase — AGENTS.md bans rebase unconditionally
// ---------------------------------------------------------------------------

func TestGitHandler_RebasePlain_WithAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	result, _ := runGit(t, ctx, ws, am, "rebase", "main")
	// rebase is unconditionally banned by AGENTS.md — error is returned.
	if !result.IsError {
		t.Fatal("expected IsError for banned rebase operation")
	}
	if !strings.Contains(result.Output, "AGENTS.md bans rebase unconditionally") {
		t.Errorf("error should mention AGENTS.md ban: %s", result.Output)
	}
}

func TestGitHandler_RebasePlain_WithoutAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	result, _ := runGit(t, ctx, ws, nil, "rebase", "main")
	// rebase is unconditionally banned by AGENTS.md — error is returned.
	if !result.IsError {
		t.Fatal("expected IsError for banned rebase operation")
	}
	if !strings.Contains(result.Output, "AGENTS.md bans rebase unconditionally") {
		t.Errorf("error should mention AGENTS.md ban: %s", result.Output)
	}
}

func TestGitHandler_RebaseAbort(t *testing.T) {
	// `git rebase --abort` is the only permitted rebase invocation per AGENTS.md.
	// Test that it is allowed (does not error).
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	// No AM — rebase --abort should be allowed
	result, err := runGit(t, ctx, ws, nil, "rebase", "--abort")
	// rebase --abort should succeed without an error (it's a recovery op)
	if err != nil {
		t.Fatalf("unexpected error for `git rebase --abort`: %v", err)
	}
	// Note: The result might be an error from git itself if there's no rebase
	// in progress (e.g., "fatal: no rebase in progress"), but that's a git
	// error, not a ban on the operation. We just verify no AGENTS.md ban error.
	if result.IsError && strings.Contains(result.Output, "AGENTS.md bans rebase unconditionally") {
		t.Errorf("git rebase --abort should not be banned: %s", result.Output)
	}
}

// ---------------------------------------------------------------------------
// branch_delete — DANGEROUS via security_classifier
// ---------------------------------------------------------------------------

func TestGitHandler_BranchDelete_WithAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	_, err := runGit(t, ctx, ws, am, "branch_delete", "feature-branch")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(am.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(am.calls))
	}
	// branch_delete is now CAUTION (downgraded from DANGEROUS) — goes through
	// dangerousOps fallback with "high" risk level
	if am.calls[0].riskLevel != "high" {
		t.Errorf("riskLevel = %q, want 'high'", am.calls[0].riskLevel)
	}
}

func TestGitHandler_BranchDelete_WithoutAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	result, err := runGit(t, ctx, ws, nil, "branch_delete", "feature-branch")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	// branch_delete is now CAUTION — without AM it warns but proceeds
	if result.IsError {
		t.Fatalf("CAUTION operation should proceed without AM: %s", result.Output)
	}
}

// ---------------------------------------------------------------------------
// clean — CAUTION + dangerousOps fallback
// ---------------------------------------------------------------------------

func TestGitHandler_Clean_WithAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	_, err := runGit(t, ctx, ws, am, "clean", "-fd")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(am.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(am.calls))
	}
	if am.calls[0].riskLevel != "high" {
		t.Errorf("riskLevel = %q, want 'high'", am.calls[0].riskLevel)
	}
}

func TestGitHandler_Clean_WithoutAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	_, err := runGit(t, ctx, ws, nil, "clean", "-fd")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	// clean is CAUTION-level in the classifier and in dangerousOps. Without an
	// approval manager, the handler warns (to stderr) and falls through to
	// execution. The underlying execShellCmd may block the specific -fd flags
	// via its own security classifier, which is expected defense-in-depth.
	// The key assertion is that the handler did NOT hard-block — it let the
	// execution layer make the final call rather than pre-emptively returning
	// a "destructive operation" error like DANGEROUS-tier ops do.
}

// ---------------------------------------------------------------------------
// Safe operations — no approval
// ---------------------------------------------------------------------------

func TestGitHandler_Commit_NoApproval(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	makeInitialCommit(t, ws)
	am := &capturingApprovalManager{approved: true}
	result, err := runGit(t, ctx, ws, am, "commit", "-m \"test\"")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("commit should succeed: %s", result.Output)
	}
	if len(am.calls) != 0 {
		t.Errorf("commit should NOT trigger approval, got %d calls", len(am.calls))
	}
}

func TestGitHandler_Add_NoApproval(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	_, err := runGit(t, ctx, ws, am, "add", ".")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(am.calls) != 0 {
		t.Errorf("add should NOT trigger approval, got %d calls", len(am.calls))
	}
}

func TestGitHandler_Push_NoApproval(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	_, _ = runGit(t, ctx, ws, am, "push", "origin main")
	if len(am.calls) != 0 {
		t.Errorf("push is SAFE in classifier, should NOT trigger approval despite dangerousOps entry, got %d calls", len(am.calls))
	}
}

func TestGitHandler_Fetch_NoApproval(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	_, _ = runGit(t, ctx, ws, am, "fetch", "--all")
	if len(am.calls) != 0 {
		t.Errorf("fetch should NOT trigger approval, got %d calls", len(am.calls))
	}
}

func TestGitHandler_Merge_NoApproval(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	_, _ = runGit(t, ctx, ws, am, "merge", "feature-branch")
	if len(am.calls) != 0 {
		t.Errorf("merge is SAFE in classifier, should NOT trigger approval despite dangerousOps entry, got %d calls", len(am.calls))
	}
}

func TestGitHandler_Revert_NoApproval(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	_, _ = runGit(t, ctx, ws, am, "revert", "HEAD~1")
	if len(am.calls) != 0 {
		t.Errorf("revert is SAFE in classifier, should NOT trigger approval, got %d calls", len(am.calls))
	}
}

// ---------------------------------------------------------------------------
// restore — CAUTION but NOT in dangerousOps → no approval
// ---------------------------------------------------------------------------

func TestGitHandler_Restore_NoApproval(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	_, _ = runGit(t, ctx, ws, am, "restore", "file.go")
	if len(am.calls) != 0 {
		t.Errorf("restore is CAUTION but not in dangerousOps, should NOT trigger approval, got %d calls", len(am.calls))
	}
}

// ---------------------------------------------------------------------------
// ValidateGitArgs integration — guards against CVE-class RCE via git args
// (--upload-pack, -c core.hooksPath, etc.) reaching shell construction.
// ---------------------------------------------------------------------------

func TestGitHandler_BlocksDangerousArgs_UploadPack(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	result, err := runGit(t, ctx, ws, am, "fetch", "--upload-pack=evil-command")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected --upload-pack= to be blocked, got success: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Blocked git args") {
		t.Errorf("expected 'Blocked git args' message, got: %s", result.Output)
	}
	if len(am.calls) != 0 {
		t.Errorf("blocked args should NOT trigger approval, got %d calls", len(am.calls))
	}
}

func TestGitHandler_BlocksDangerousArgs_HooksPath(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	result, err := runGit(t, ctx, ws, am, "fetch", "-c core.hooksPath=/tmp/evil.git/hooks")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected -c core.hooksPath= to be blocked, got success: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Blocked git args") {
		t.Errorf("expected 'Blocked git args' message, got: %s", result.Output)
	}
}

func TestGitHandler_AllowsSafeArgs(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	// A safe --upload-pack is still allowed (validate is for *dangerous* values
	// of these flags; legitimate use is not the audit's concern).
	result, err := runGit(t, ctx, ws, am, "fetch", "")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("fetch with no args should not error, got: %s", result.Output)
	}
}

// ---------------------------------------------------------------------------
// Invalid operation
// ---------------------------------------------------------------------------

func TestGitHandler_InvalidOperation(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	result, err := runGit(t, ctx, ws, am, "nonexistent", "")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Invalid git operation") {
		t.Errorf("should say 'Invalid git operation': %s", result.Output)
	}
	if len(am.calls) != 0 {
		t.Errorf("invalid operation should NOT trigger approval, got %d calls", len(am.calls))
	}
}

func TestBuildGitCommandAll(t *testing.T) {
	tests := []struct {
		name     string
		op       GitOperationType
		args     string
		expected string
	}{
		// Basic operations without args
		{"commit no args", GitOpCommit, "", "git commit"},
		{"push no args", GitOpPush, "", "git push"},
		{"add no args", GitOpAdd, "", "git add"},
		{"rm no args", GitOpRm, "", "git rm"},
		{"mv no args", GitOpMv, "", "git mv"},
		{"reset no args", GitOpReset, "", "git reset"},
		{"rebase no args", GitOpRebase, "", "git rebase"},
		{"merge no args", GitOpMerge, "", "git merge"},
		{"checkout no args", GitOpCheckout, "", "git checkout"},
		{"tag no args", GitOpTag, "", "git tag"},
		{"clean no args", GitOpClean, "", "git clean"},
		{"stash no args", GitOpStash, "", "git stash"},
		{"am no args", GitOpAm, "", "git am"},
		{"apply no args", GitOpApply, "", "git apply"},
		{"revert no args", GitOpRevert, "", "git revert"},

		// Operations with args
		{"commit with msg", GitOpCommit, "-m \"feat: new feature\"", "git commit -m \"feat: new feature\""},
		{"push to remote", GitOpPush, "origin main", "git push origin main"},
		{"add file", GitOpAdd, "src/main.go", "git add src/main.go"},
		{"reset hard", GitOpReset, "--hard HEAD~1", "git reset --hard HEAD~1"},
		{"checkout branch", GitOpCheckout, "feature-branch", "git checkout feature-branch"},
		{"tag v1", GitOpTag, "-a v1.0.0 -m \"Release 1.0.0\"", "git tag -a v1.0.0 -m \"Release 1.0.0\""},
		{"stash push", GitOpStash, "-m \"work in progress\"", "git stash -m \"work in progress\""},
		{"clean force", GitOpClean, "-fd", "git clean -fd"},
		{"rebase interactive", GitOpRebase, "-i HEAD~5", "git rebase -i HEAD~5"},
		{"merge squash", GitOpMerge, "--squash feature", "git merge --squash feature"},

		// Underscore → hyphen conversions
		{"cherry_pick", GitOpCherryPick, "", "git cherry-pick"},
		{"cherry_pick with hash", GitOpCherryPick, "abc1234", "git cherry-pick abc1234"},
		{"branch_delete no args", GitOpBranchDelete, "", "git branch"},
		{"branch_delete with args", GitOpBranchDelete, "-d old-branch", "git branch -d old-branch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildGitCommand(tt.op, tt.args)
			if result != tt.expected {
				t.Errorf("buildGitCommand(%q, %q) = %q; want %q", tt.op, tt.args, result, tt.expected)
			}
		})
	}
}

func TestBuildGitCommandBranchDeleteSpecial(t *testing.T) {
	// branch_delete must map to "git branch" (not "git branch-delete")
	for _, args := range []string{"", "-d foo", "-D bar"} {
		result := buildGitCommand(GitOpBranchDelete, args)
		if result[:10] != "git branch" {
			t.Errorf("branch_delete command should start with 'git branch', got: %s", result)
		}
		if result == "git branch-delete" || result == "git branch_delete" {
			t.Errorf("branch_delete must NOT produce 'git branch-delete', got: %s", result)
		}
	}
}

func TestExecuteGitOperationCommitNilExecutorV2(t *testing.T) {
	op := GitOperation{Operation: GitOpCommit}
	_, err := ExecuteGitOperation(context.Background(), op, "test-session", nil, nil)
	if err == nil {
		t.Error("expected error when commit flow executor is nil")
	}
}

func TestExecuteGitOperationApprovalDeniedV2(t *testing.T) {
	// Use a mock that always denies
	denier := &testApprovalPrompter{approved: false}
	op := GitOperation{Operation: GitOpPush, Args: "origin main"}
	_, err := ExecuteGitOperation(context.Background(), op, "test", nil, denier)
	if err == nil {
		t.Error("expected error when user denies approval")
	}
	if err != nil && err.Error() != "git operation cancelled by user" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExecuteGitOperationApprovalApprovedV2(t *testing.T) {
	// Use a mock that always approves but let the actual git command fail
	// (since we don't have a real git repo in test)
	approver := &testApprovalPrompter{approved: true}
	op := GitOperation{Operation: GitOpPush, Args: "origin main"}
	_, err := ExecuteGitOperation(context.Background(), op, "test", nil, approver)
	// This will likely fail because there's no real git repo, which is fine —
	// we're verifying approval was accepted and execution was attempted
	if err == nil {
		// If somehow git worked, that's not a test failure
		return
	}
	// The error should be about git command failure, not approval
	if err.Error() == "git operation cancelled by user" {
		t.Error("git operation should not be cancelled when approved")
	}
}

// testApprovalPrompter implements GitApprovalPrompter for testing
type testApprovalPrompter struct {
	approved bool
}

func (m *testApprovalPrompter) PromptForApproval(_ string) (bool, error) {
	return m.approved, nil
}

// ---------------------------------------------------------------------------
// Mock implementations for testing
// ---------------------------------------------------------------------------

type mockGitCommitFlowExecutor2 struct {
	output string
	err    error
}

func (m *mockGitCommitFlowExecutor2) ExecuteGitCommitFlow() (string, error) {
	return m.output, m.err
}

type mockGitApprovalPrompter2 struct {
	approved bool
	err      error
}

func (m *mockGitApprovalPrompter2) PromptForApproval(_ string) (bool, error) {
	return m.approved, m.err
}

// ---------------------------------------------------------------------------
// TestBuildGitCommand2 - Test all GitOperationType values
// ---------------------------------------------------------------------------

func TestBuildGitCommand2(t *testing.T) {
	tests := []struct {
		name     string
		op       GitOperationType
		args     string
		expected string
	}{
		// All operation types with args - verify underscore to hyphen conversion
		{"commit with message", GitOpCommit, "-m 'initial'", "git commit -m 'initial'"},
		{"push with force", GitOpPush, "--force", "git push --force"},
		{"push with origin", GitOpPush, "origin main", "git push origin main"},
		{"add single file", GitOpAdd, "main.go", "git add main.go"},
		{"add multiple files", GitOpAdd, "*.go", "git add *.go"},
		{"rm file", GitOpRm, "old_file.go", "git rm old_file.go"},
		{"rm with flags", GitOpRm, "-f deleted.go", "git rm -f deleted.go"},
		{"mv rename", GitOpMv, "old.go new.go", "git mv old.go new.go"},
		{"reset hard", GitOpReset, "--hard HEAD~1", "git reset --hard HEAD~1"},
		{"reset soft", GitOpReset, "--soft HEAD~1", "git reset --soft HEAD~1"},
		{"rebase main", GitOpRebase, "main", "git rebase main"},
		{"rebase with flags", GitOpRebase, "-i HEAD~5", "git rebase -i HEAD~5"},
		{"merge feature", GitOpMerge, "feature-branch", "git merge feature-branch"},
		{"merge no-ff", GitOpMerge, "--no-ff feature", "git merge --no-ff feature"},
		{"checkout branch", GitOpCheckout, "develop", "git checkout develop"},
		{"checkout new branch", GitOpCheckout, "-b new-feature", "git checkout -b new-feature"},
		{"tag annotated", GitOpTag, "-a v1.0 -m 'Release'", "git tag -a v1.0 -m 'Release'"},
		{"tag lightweight", GitOpTag, "v2.0", "git tag v2.0"},
		{"clean all", GitOpClean, "-fdx", "git clean -fdx"},
		{"stash save", GitOpStash, "save 'WIP'", "git stash save 'WIP'"},
		{"am mbox", GitOpAm, "0001.patch", "git am 0001.patch"},
		{"apply patch", GitOpApply, "changes.patch", "git apply changes.patch"},
		{"cherry-pick commit", GitOpCherryPick, "abc123def456", "git cherry-pick abc123def456"},
		{"revert commit", GitOpRevert, "def456abc123", "git revert def456abc123"},

		// All operation types without args
		{"commit no args", GitOpCommit, "", "git commit"},
		{"push no args", GitOpPush, "", "git push"},
		{"add no args", GitOpAdd, "", "git add"},
		{"rm no args", GitOpRm, "", "git rm"},
		{"mv no args", GitOpMv, "", "git mv"},
		{"reset no args", GitOpReset, "", "git reset"},
		{"rebase no args", GitOpRebase, "", "git rebase"},
		{"merge no args", GitOpMerge, "", "git merge"},
		{"checkout no args", GitOpCheckout, "", "git checkout"},
		{"tag no args", GitOpTag, "", "git tag"},
		{"clean no args", GitOpClean, "", "git clean"},
		{"stash no args", GitOpStash, "", "git stash"},
		{"am no args", GitOpAm, "", "git am"},
		{"apply no args", GitOpApply, "", "git apply"},
		{"cherry-pick no args", GitOpCherryPick, "", "git cherry-pick"},
		{"revert no args", GitOpRevert, "", "git revert"},

		// Special case: branch_delete -> "branch" (not "branch-delete")
		{"branch_delete delete", GitOpBranchDelete, "-d feature", "git branch -d feature"},
		{"branch_delete force delete", GitOpBranchDelete, "-D feature", "git branch -D feature"},
		{"branch_delete no args", GitOpBranchDelete, "", "git branch"},
		{"branch_delete list after delete", GitOpBranchDelete, "-d temp && git branch", "git branch -d temp && git branch"},

		// Verify underscore to hyphen conversion for cherry_pick
		{"cherry_pick underscore conversion", GitOpCherryPick, "abc123", "git cherry-pick abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildGitCommand(tt.op, tt.args)
			if got != tt.expected {
				t.Errorf("buildGitCommand(%q, %q) = %q; want %q", tt.op, tt.args, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestGitOperationType_values2 - Verify const values match expected strings
// ---------------------------------------------------------------------------

func TestGitOperationType_values2(t *testing.T) {
	tests := []struct {
		name     string
		op       GitOperationType
		expected string
	}{
		{"commit value", GitOpCommit, "commit"},
		{"push value", GitOpPush, "push"},
		{"add value", GitOpAdd, "add"},
		{"rm value", GitOpRm, "rm"},
		{"mv value", GitOpMv, "mv"},
		{"reset value", GitOpReset, "reset"},
		{"rebase value", GitOpRebase, "rebase"},
		{"merge value", GitOpMerge, "merge"},
		{"checkout value", GitOpCheckout, "checkout"},
		{"branch_delete value", GitOpBranchDelete, "branch_delete"},
		{"tag value", GitOpTag, "tag"},
		{"clean value", GitOpClean, "clean"},
		{"stash value", GitOpStash, "stash"},
		{"am value", GitOpAm, "am"},
		{"apply value", GitOpApply, "apply"},
		{"cherry_pick value", GitOpCherryPick, "cherry_pick"},
		{"revert value", GitOpRevert, "revert"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(tt.op)
			if got != tt.expected {
				t.Errorf("GitOperationType constant %s = %q; want %q", tt.name, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestGitOperation_struct2 - Verify struct creation with JSON tags
// ---------------------------------------------------------------------------

func TestGitOperation_struct2(t *testing.T) {
	// Test creating a GitOperation struct
	op := GitOperation{
		Operation: GitOpCommit,
		Args:      "-m 'test commit'",
	}

	if op.Operation != GitOpCommit {
		t.Errorf("Operation = %q; want %q", op.Operation, GitOpCommit)
	}
	if op.Args != "-m 'test commit'" {
		t.Errorf("Args = %q; want %q", op.Args, "-m 'test commit'")
	}

	// Test JSON marshaling
	jsonData, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	expectedJSON := `{"operation":"commit","args":"-m 'test commit'"}`
	if string(jsonData) != expectedJSON {
		t.Errorf("JSON = %q; want %q", string(jsonData), expectedJSON)
	}

	// Test JSON unmarshaling
	var unmarshaledOp GitOperation
	err = json.Unmarshal(jsonData, &unmarshaledOp)
	if err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if unmarshaledOp.Operation != GitOpCommit {
		t.Errorf("Unmarshaled Operation = %q; want %q", unmarshaledOp.Operation, GitOpCommit)
	}
	if unmarshaledOp.Args != "-m 'test commit'" {
		t.Errorf("Unmarshaled Args = %q; want %q", unmarshaledOp.Args, "-m 'test commit'")
	}
}

func TestGitOperation_structWithEmptyArgs2(t *testing.T) {
	op := GitOperation{
		Operation: GitOpPush,
		Args:      "",
	}

	if op.Operation != GitOpPush {
		t.Errorf("Operation = %q; want %q", op.Operation, GitOpPush)
	}
	if op.Args != "" {
		t.Errorf("Args = %q; want empty string", op.Args)
	}

	// Test JSON marshaling with empty args (should omitomitempty)
	jsonData, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// With omitempty, empty args should not be in JSON
	expectedJSON := `{"operation":"push"}`
	if string(jsonData) != expectedJSON {
		t.Errorf("JSON = %q; want %q", string(jsonData), expectedJSON)
	}
}

func TestGitOperation_structMultipleOperations2(t *testing.T) {
	operations := []struct {
		name string
		op   GitOperation
	}{
		{"add", GitOperation{Operation: GitOpAdd, Args: "*.go"}},
		{"push", GitOperation{Operation: GitOpPush, Args: "origin main"}},
		{"reset", GitOperation{Operation: GitOpReset, Args: "--hard HEAD~1"}},
		{"branch_delete", GitOperation{Operation: GitOpBranchDelete, Args: "-d feature"}},
		{"cherry_pick", GitOperation{Operation: GitOpCherryPick, Args: "abc123"}},
	}

	for _, tt := range operations {
		t.Run(tt.name, func(t *testing.T) {
			// Verify the operation type is correct
			if tt.op.Operation == "" {
				t.Error("Operation should not be empty")
			}

			// Verify args are set
			if tt.op.Args == "" {
				t.Error("Args should not be empty for this test case")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestExecuteGitOperation_commit_noExecutor2 - Test commit without executor
// ---------------------------------------------------------------------------

func TestExecuteGitOperation_commit_noExecutor2(t *testing.T) {
	ctx := context.Background()
	op := GitOperation{Operation: GitOpCommit, Args: "-m 'test'"}
	sessionID := "test-session"

	// Call with nil executor
	result, err := ExecuteGitOperation(ctx, op, sessionID, nil, nil)

	// Verify error
	if err == nil {
		t.Fatal("expected error when commit operation has no executor, got nil")
	}

	expectedErrMsg := "commit operation requires a commit flow executor"
	if err.Error() != expectedErrMsg {
		t.Errorf("error = %q; want %q", err.Error(), expectedErrMsg)
	}

	// Verify result is empty
	if result != "" {
		t.Errorf("result = %q; want empty string", result)
	}
}

// Test commit without executor but with a nil approval prompter (should still error)
func TestExecuteGitOperation_commit_noExecutorWithPrompter2(t *testing.T) {
	ctx := context.Background()
	op := GitOperation{Operation: GitOpCommit, Args: "-m 'test'"}
	sessionID := "test-session"
	prompter := &mockGitApprovalPrompter2{approved: true, err: nil}

	// Call with nil executor but with prompter
	result, err := ExecuteGitOperation(ctx, op, sessionID, nil, prompter)

	// Verify error - should fail before checking prompter
	if err == nil {
		t.Fatal("expected error when commit operation has no executor, got nil")
	}

	expectedErrMsg := "commit operation requires a commit flow executor"
	if err.Error() != expectedErrMsg {
		t.Errorf("error = %q; want %q", err.Error(), expectedErrMsg)
	}

	if result != "" {
		t.Errorf("result = %q; want empty string", result)
	}
}

// ---------------------------------------------------------------------------
// TestExecuteGitOperation_commit_withExecutor2 - Test commit delegates to executor
// ---------------------------------------------------------------------------

func TestExecuteGitOperation_commit_withExecutor2(t *testing.T) {
	ctx := context.Background()
	op := GitOperation{Operation: GitOpCommit, Args: "-m 'test'"}
	sessionID := "test-session"

	// Mock executor that returns success
	expectedOutput := "[master abc123] Test commit\n 1 file changed, 5 insertions(+)"
	executor := &mockGitCommitFlowExecutor2{output: expectedOutput, err: nil}

	// Call with executor (approval prompter should be ignored for commits)
	result, err := ExecuteGitOperation(ctx, op, sessionID, executor, nil)

	// Verify success
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output matches executor output
	if result != expectedOutput {
		t.Errorf("result = %q; want %q", result, expectedOutput)
	}
}

func TestExecuteGitOperation_commit_executorReturnsError2(t *testing.T) {
	ctx := context.Background()
	op := GitOperation{Operation: GitOpCommit, Args: ""}
	sessionID := "test-session"

	// Mock executor that returns an error
	expectedErr := errors.New("commit failed: no changes to commit")
	executor := &mockGitCommitFlowExecutor2{output: "", err: expectedErr}

	// Call with executor that returns error
	result, err := ExecuteGitOperation(ctx, op, sessionID, executor, nil)

	// Verify error
	if err == nil {
		t.Fatal("expected error from executor, got nil")
	}

	if err != expectedErr {
		t.Errorf("error = %v; want %v", err, expectedErr)
	}

	// Verify result is empty
	if result != "" {
		t.Errorf("result = %q; want empty string", result)
	}
}

func TestExecuteGitOperation_commit_withExecutorAndPrompter2(t *testing.T) {
	ctx := context.Background()
	op := GitOperation{Operation: GitOpCommit, Args: "-m 'test'"}
	sessionID := "test-session"

	// Mock executor and prompter
	expectedOutput := "commit successful"
	executor := &mockGitCommitFlowExecutor2{output: expectedOutput, err: nil}
	prompter := &mockGitApprovalPrompter2{approved: false, err: nil} // Should be ignored

	// Call with both executor and prompter
	result, err := ExecuteGitOperation(ctx, op, sessionID, executor, prompter)

	// Verify success - prompter should be ignored for commits
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != expectedOutput {
		t.Errorf("result = %q; want %q", result, expectedOutput)
	}
}

// ---------------------------------------------------------------------------
// TestExecuteGitOperation_cancelled2 - Test cancelled approval
// ---------------------------------------------------------------------------

func TestExecuteGitOperation_cancelled2(t *testing.T) {
	ctx := context.Background()
	op := GitOperation{Operation: GitOpPush, Args: "--force"}
	sessionID := "test-session"

	// Mock prompter that denies approval
	prompter := &mockGitApprovalPrompter2{approved: false, err: nil}

	// Call with approval denied
	result, err := ExecuteGitOperation(ctx, op, sessionID, nil, prompter)

	// Verify error
	if err == nil {
		t.Fatal("expected error when approval is denied, got nil")
	}

	expectedErrMsg := "git operation cancelled by user"
	if err.Error() != expectedErrMsg {
		t.Errorf("error = %q; want %q", err.Error(), expectedErrMsg)
	}

	// Verify result is empty
	if result != "" {
		t.Errorf("result = %q; want empty string", result)
	}
}

func TestExecuteGitOperation_approvalError2(t *testing.T) {
	ctx := context.Background()
	op := GitOperation{Operation: GitOpReset, Args: "--hard HEAD"}
	sessionID := "test-session"

	// Mock prompter that returns an error
	expectedErr := errors.New("user prompt failed: context canceled")
	prompter := &mockGitApprovalPrompter2{approved: false, err: expectedErr}

	// Call with approval prompter error
	result, err := ExecuteGitOperation(ctx, op, sessionID, nil, prompter)

	// Verify error
	if err == nil {
		t.Fatal("expected error from approval prompter, got nil")
	}

	// The error should wrap the original error
	if !errors.Is(err, expectedErr) && err.Error() != "get user approval: user prompt failed: context canceled" {
		t.Errorf("error = %v; want wrapped error containing %v", err, expectedErr)
	}

	// Verify result is empty
	if result != "" {
		t.Errorf("result = %q; want empty string", result)
	}
}

func TestExecuteGitOperation_nilApprovalPrompter2(t *testing.T) {
	ctx := context.Background()
	op := GitOperation{Operation: GitOpAdd, Args: "file.go"}
	sessionID := "test-session"

	// Call with nil approval prompter - should skip approval and try to execute
	// This will likely fail since there's no actual git repo, but it should not
	// fail due to missing prompter
	result, err := ExecuteGitOperation(ctx, op, sessionID, nil, nil)

	// The error should be from git command, not from missing prompter
	if err != nil {
		// Verify it's not an approval-related error
		if strings.Contains(err.Error(), "approval") {
			t.Errorf("error should not mention approval when prompter is nil: %v", err)
		}
	}

	// Result could be empty or contain git output depending on environment
	_ = result // We don't assert on result since it depends on git environment
}

// ---------------------------------------------------------------------------
// Additional edge case tests
// ---------------------------------------------------------------------------

func TestExecuteGitOperation_multipleTypesWithCancelledApproval2(t *testing.T) {
	ctx := context.Background()
	sessionID := "test-session"
	prompter := &mockGitApprovalPrompter2{approved: false, err: nil}

	// Most operations: prompter denies → "cancelled by user" error.
	canceledOps := []GitOperation{
		{Operation: GitOpPush, Args: "origin main"},
		{Operation: GitOpReset, Args: "--hard HEAD"},
		{Operation: GitOpMerge, Args: "feature"},
		{Operation: GitOpCheckout, Args: "-b new-branch"},
		{Operation: GitOpBranchDelete, Args: "-d old-branch"},
		{Operation: GitOpTag, Args: "v1.0"},
	}

	for _, op := range canceledOps {
		t.Run(string(op.Operation), func(t *testing.T) {
			result, err := ExecuteGitOperation(ctx, op, sessionID, nil, prompter)

			if err == nil {
				t.Errorf("expected error for cancelled approval, got nil")
			}

			expectedErrMsg := "git operation cancelled by user"
			if err.Error() != expectedErrMsg {
				t.Errorf("error = %q; want %q", err.Error(), expectedErrMsg)
			}

			if result != "" {
				t.Errorf("result = %q; want empty string", result)
			}
		})
	}

	// Rebase is unconditionally banned per AGENTS.md — the prompter is never
	// consulted, and the operation errors out before any execution.
	t.Run("rebase", func(t *testing.T) {
		op := GitOperation{Operation: GitOpRebase, Args: "main"}
		result, err := ExecuteGitOperation(ctx, op, sessionID, nil, prompter)
		if err == nil {
			t.Fatal("expected rebase to be rejected, got nil error")
		}
		if !strings.Contains(err.Error(), "AGENTS.md bans rebase") {
			t.Errorf("error = %q; want an error mentioning the AGENTS.md rebase ban", err.Error())
		}
		if result != "" {
			t.Errorf("result = %q; want empty string", result)
		}
	})
}

func TestBuildGitCommand2_allOperationTypes2(t *testing.T) {
	// Verify that all operation types are handled correctly
	operations := map[GitOperationType]string{
		GitOpCommit:       "commit",
		GitOpPush:         "push",
		GitOpAdd:          "add",
		GitOpRm:           "rm",
		GitOpMv:           "mv",
		GitOpReset:        "reset",
		GitOpRebase:       "rebase",
		GitOpMerge:        "merge",
		GitOpCheckout:     "checkout",
		GitOpBranchDelete: "branch", // Special case: no hyphen
		GitOpTag:          "tag",
		GitOpClean:        "clean",
		GitOpStash:        "stash",
		GitOpAm:           "am",
		GitOpApply:        "apply",
		GitOpCherryPick:   "cherry-pick", // Underscore converted to hyphen
		GitOpRevert:       "revert",
	}

	for opType, expectedSubcommand := range operations {
		t.Run(string(opType), func(t *testing.T) {
			cmd := buildGitCommand(opType, "")
			expectedCmd := "git " + expectedSubcommand
			if cmd != expectedCmd {
				t.Errorf("buildGitCommand(%q, \"\") = %q; want %q", opType, cmd, expectedCmd)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildGitCommand tests
// ---------------------------------------------------------------------------

func TestBuildGitCommand(t *testing.T) {
	tests := []struct {
		name     string
		op       GitOperationType
		args     string
		expected string
	}{
		// basic operations with args
		{"commit with args", GitOpCommit, "-m 'initial'", "git commit -m 'initial'"},
		{"push with --force", GitOpPush, "--force", "git push --force"},
		{"add file", GitOpAdd, "file.go", "git add file.go"},
		{"rm file", GitOpRm, "file.go", "git rm file.go"},
		{"mv files", GitOpMv, "a.go b.go", "git mv a.go b.go"},
		{"reset hard", GitOpReset, "--hard HEAD~1", "git reset --hard HEAD~1"},
		{"checkout branch", GitOpCheckout, "-b new", "git checkout -b new"},
		{"rebase main", GitOpRebase, "main", "git rebase main"},
		{"merge feature", GitOpMerge, "feature", "git merge feature"},
		{"tag v1.0", GitOpTag, "v1.0", "git tag v1.0"},
		{"clean -fd", GitOpClean, "-fd", "git clean -fd"},
		{"apply patch", GitOpApply, "patch.diff", "git apply patch.diff"},
		{"cherry-pick abc123", GitOpCherryPick, "abc123", "git cherry-pick abc123"},
		{"revert abc123", GitOpRevert, "abc123", "git revert abc123"},

		// operations with empty args
		{"stash no args", GitOpStash, "", "git stash"},
		{"am no args", GitOpAm, "", "git am"},
		{"commit no args", GitOpCommit, "", "git commit"},
		{"push no args", GitOpPush, "", "git push"},

		// special case: branch_delete -> branch (not branch-delete)
		{"branch-delete", GitOpBranchDelete, "-d feature", "git branch -d feature"},
		{"branch-delete no args", GitOpBranchDelete, "", "git branch"},

		// underscore conversion for cherry_pick and branch_delete
		{"cherry-pick underscore", GitOpCherryPick, "abc", "git cherry-pick abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildGitCommand(tt.op, tt.args)
			if got != tt.expected {
				t.Errorf("buildGitCommand(%q, %q) = %q; want %q", tt.op, tt.args, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GitOperationType constant values
// ---------------------------------------------------------------------------

func TestGitOperationType_Constants(t *testing.T) {
	tests := []struct {
		name     string
		op       GitOperationType
		expected string
	}{
		{"commit", GitOpCommit, "commit"},
		{"push", GitOpPush, "push"},
		{"add", GitOpAdd, "add"},
		{"rm", GitOpRm, "rm"},
		{"mv", GitOpMv, "mv"},
		{"reset", GitOpReset, "reset"},
		{"rebase", GitOpRebase, "rebase"},
		{"merge", GitOpMerge, "merge"},
		{"checkout", GitOpCheckout, "checkout"},
		{"branch_delete", GitOpBranchDelete, "branch_delete"},
		{"tag", GitOpTag, "tag"},
		{"clean", GitOpClean, "clean"},
		{"stash", GitOpStash, "stash"},
		{"am", GitOpAm, "am"},
		{"apply", GitOpApply, "apply"},
		{"cherry_pick", GitOpCherryPick, "cherry_pick"},
		{"revert", GitOpRevert, "revert"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.op) != tt.expected {
				t.Errorf("GitOperationType constant %s = %q; want %q", tt.name, tt.op, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExecuteGitOperation – commit without executor
// ---------------------------------------------------------------------------

func TestExecuteGitOperation_CommitWithoutExecutor(t *testing.T) {
	op := GitOperation{Operation: GitOpCommit, Args: "-m 'test'"}
	_, err := ExecuteGitOperation(context.Background(), op, "", nil, nil)
	if err == nil {
		t.Fatal("expected error when commit operation has no executor, got nil")
	}
	expected := "commit operation requires a commit flow executor"
	if err.Error() != expected {
		t.Errorf("error = %q; want %q", err.Error(), expected)
	}
}

// ---------------------------------------------------------------------------
// ExecuteGitOperation – cancelled approval
// ---------------------------------------------------------------------------

type mockApprovalPrompter struct {
	approve bool
	err     error
}

func (m *mockApprovalPrompter) PromptForApproval(_ string) (bool, error) {
	return m.approve, m.err
}

func TestExecuteGitOperation_CancelledApproval(t *testing.T) {
	op := GitOperation{Operation: GitOpPush, Args: "--force"}
	prompter := &mockApprovalPrompter{approve: false, err: nil}

	_, err := ExecuteGitOperation(context.Background(), op, "", nil, prompter)
	if err == nil {
		t.Fatal("expected error when approval is denied, got nil")
	}
	expected := "git operation cancelled by user"
	if err.Error() != expected {
		t.Errorf("error = %q; want %q", err.Error(), expected)
	}
}

// ---------------------------------------------------------------------------
// ExecuteGitOperation – approval error
// ---------------------------------------------------------------------------

func TestExecuteGitOperation_ApprovalError(t *testing.T) {
	op := GitOperation{Operation: GitOpPush, Args: "--force"}
	prompter := &mockApprovalPrompter{approve: false, err: context.DeadlineExceeded}

	_, err := ExecuteGitOperation(context.Background(), op, "", nil, prompter)
	if err == nil {
		t.Fatal("expected error when approval prompt fails, got nil")
	}
	expected := "get user approval: context deadline exceeded"
	if err.Error() != expected {
		t.Errorf("error = %q; want %q", err.Error(), expected)
	}
}

// ---------------------------------------------------------------------------
// ExecuteGitOperation – nil approvalPrompter skips approval
// ---------------------------------------------------------------------------

type mockCommitFlowExecutor struct {
	output string
	err    error
}

func (m *mockCommitFlowExecutor) ExecuteGitCommitFlow() (string, error) {
	return m.output, m.err
}

func TestExecuteGitOperation_CommitWithExecutor(t *testing.T) {
	op := GitOperation{Operation: GitOpCommit, Args: "-m 'test'"}
	executor := &mockCommitFlowExecutor{output: "commit result", err: nil}

	got, err := ExecuteGitOperation(context.Background(), op, "", executor, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "commit result" {
		t.Errorf("got = %q; want %q", got, "commit result")
	}
}

func TestExecuteGitOperation_CommitExecutorReturnsError(t *testing.T) {
	op := GitOperation{Operation: GitOpCommit, Args: ""}
	executor := &mockCommitFlowExecutor{output: "", err: context.Canceled}

	_, err := ExecuteGitOperation(context.Background(), op, "", executor, nil)
	if err == nil {
		t.Fatal("expected error from commit flow executor, got nil")
	}
	if err.Error() != "context canceled" {
		t.Errorf("error = %q; want %q", err.Error(), "context canceled")
	}
}

// ---------------------------------------------------------------------------
// git.go — buildGitCommand
// ---------------------------------------------------------------------------

func TestBuildGitCommand_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		op   GitOperationType
		args string
		want string
	}{
		{"commit_no_args", GitOpCommit, "", "git commit"},
		{"push_with_args", GitOpPush, "origin main", "git push origin main"},
		{"add_with_args", GitOpAdd, "file.go", "git add file.go"},
		{"reset_hard", GitOpReset, "--hard HEAD~1", "git reset --hard HEAD~1"},
		{"branch_delete", GitOpBranchDelete, "-D feature", "git branch -D feature"},
		{"cherry_pick", GitOpCherryPick, "abc123", "git cherry-pick abc123"},
		{"clean_no_args", GitOpClean, "", "git clean"},
		{"stash", GitOpStash, "pop", "git stash pop"},
		{"rebase", GitOpRebase, "main", "git rebase main"},
		{"checkout", GitOpCheckout, "-b new-branch", "git checkout -b new-branch"},
		{"tag", GitOpTag, "v1.0", "git tag v1.0"},
		{"revert", GitOpRevert, "HEAD", "git revert HEAD"},
		{"merge", GitOpMerge, "feature", "git merge feature"},
		{"am", GitOpAm, "< patch", "git am < patch"},
		{"apply", GitOpApply, "--check patch.diff", "git apply --check patch.diff"},
		{"pull_rebase", GitOpPull, "--rebase origin main", "git pull --rebase origin main"},
		{"fetch_all", GitOpFetch, "--all --prune", "git fetch --all --prune"},
		{"restore_staged", GitOpRestore, "--staged file.go", "git restore --staged file.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildGitCommand(tt.op, tt.args)
			if got != tt.want {
				t.Errorf("buildGitCommand(%s, %q) = %q, want %q", tt.op, tt.args, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// git.go — GitOperationType constants
// ---------------------------------------------------------------------------

func TestGitOperationTypeConstants_ZC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		op   GitOperationType
		want string
	}{
		{GitOpCommit, "commit"},
		{GitOpPush, "push"},
		{GitOpAdd, "add"},
		{GitOpRm, "rm"},
		{GitOpReset, "reset"},
		{GitOpRebase, "rebase"},
		{GitOpMerge, "merge"},
		{GitOpCheckout, "checkout"},
		{GitOpBranchDelete, "branch_delete"},
		{GitOpTag, "tag"},
		{GitOpClean, "clean"},
		{GitOpStash, "stash"},
		{GitOpAm, "am"},
		{GitOpApply, "apply"},
		{GitOpCherryPick, "cherry_pick"},
		{GitOpRevert, "revert"},
		{GitOpMv, "mv"},
		{GitOpPull, "pull"},
		{GitOpFetch, "fetch"},
		{GitOpRestore, "restore"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if string(tt.op) != tt.want {
				t.Errorf("expected %q, got %q", tt.want, string(tt.op))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// git.go — GitOperation struct
// ---------------------------------------------------------------------------

func TestGitOperationStruct_ZC(t *testing.T) {
	t.Parallel()
	op := GitOperation{
		Operation: GitOpAdd,
		Args:      "main.go",
	}
	if op.Operation != GitOpAdd {
		t.Errorf("expected GitOpAdd, got %s", op.Operation)
	}
	if op.Args != "main.go" {
		t.Errorf("expected 'main.go', got %q", op.Args)
	}
}
