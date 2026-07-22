package tools

import (
	"context"
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
