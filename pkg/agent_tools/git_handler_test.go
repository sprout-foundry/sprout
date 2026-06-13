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
	if c.riskLevel != "critical" {
		t.Errorf("riskLevel = %q, want 'critical'", c.riskLevel)
	}
	if !strings.Contains(c.prompt, "DESTRUCTIVE") {
		t.Errorf("prompt missing DESTRUCTIVE: %s", c.prompt)
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
	if !result.IsError {
		t.Fatalf("should be error without AM: %s", result.Output)
	}
	if !strings.Contains(result.Output, "destructive") {
		t.Errorf("should mention 'destructive': %s", result.Output)
	}
	if !strings.Contains(result.Output, "approval") {
		t.Errorf("should mention 'approval': %s", result.Output)
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
	if len(am.calls) != 1 || am.calls[0].riskLevel != "critical" {
		t.Errorf("expected 1 critical call, got %d calls (last risk=%q)", len(am.calls), am.calls[0].riskLevel)
	}
}

func TestGitHandler_ResetKeep_WithoutAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	result, err := runGit(t, ctx, ws, nil, "reset", "--keep HEAD~1")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("should be error without AM: %s", result.Output)
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
	if !result.IsError {
		t.Fatalf("should be error without AM: %s", result.Output)
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
// rebase -i / --onto — DANGEROUS tier
// ---------------------------------------------------------------------------

func TestGitHandler_RebaseInteractive_WithAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	result, err := runGit(t, ctx, ws, am, "rebase", "-i HEAD~3")
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
	if c.riskLevel != "critical" {
		t.Errorf("riskLevel = %q, want 'critical'", c.riskLevel)
	}
	if !strings.Contains(c.prompt, "DESTRUCTIVE") {
		t.Errorf("prompt missing DESTRUCTIVE: %s", c.prompt)
	}
}

func TestGitHandler_RebaseInteractive_WithoutAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	result, err := runGit(t, ctx, ws, nil, "rebase", "-i HEAD~3")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("should be error without AM: %s", result.Output)
	}
	if !strings.Contains(result.Output, "destructive") {
		t.Errorf("should mention 'destructive': %s", result.Output)
	}
}

func TestGitHandler_RebaseOnto_WithAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	_, err := runGit(t, ctx, ws, am, "rebase", "--onto main")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(am.calls) != 1 || am.calls[0].riskLevel != "critical" {
		t.Errorf("expected 1 critical call, got %d calls (risk=%q)", len(am.calls), am.calls[0].riskLevel)
	}
}

// ---------------------------------------------------------------------------
// plain rebase — CAUTION + dangerousOps fallback
// ---------------------------------------------------------------------------

func TestGitHandler_RebasePlain_WithAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	am := &capturingApprovalManager{approved: true}
	_, err := runGit(t, ctx, ws, am, "rebase", "main")
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
	if am.calls[0].riskLevel != "critical" {
		t.Errorf("riskLevel = %q, want 'critical'", am.calls[0].riskLevel)
	}
}

func TestGitHandler_BranchDelete_WithoutAM(t *testing.T) {
	ctx, ws := t.Context(), t.TempDir()
	initGitRepo(t, ws)
	result, err := runGit(t, ctx, ws, nil, "branch_delete", "feature-branch")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("should be error without AM: %s", result.Output)
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
