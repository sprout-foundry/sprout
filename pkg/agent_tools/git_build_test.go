package tools

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// buildGitCommand tests
// ---------------------------------------------------------------------------

func TestBuildGitCommand_AllOperationsWithArgs(t *testing.T) {
	tests := []struct {
		name     string
		op       GitOperationType
		args     string
		expected string
	}{
		// All GitOp constants with args
		{"commit with args", GitOpCommit, "-m 'test'", "git commit -m 'test'"},
		{"push with args", GitOpPush, "--force", "git push --force"},
		{"add with args", GitOpAdd, "file.go", "git add file.go"},
		{"rm with args", GitOpRm, "file.go", "git rm file.go"},
		{"mv with args", GitOpMv, "a.go b.go", "git mv a.go b.go"},
		{"reset with args", GitOpReset, "--hard HEAD~1", "git reset --hard HEAD~1"},
		{"rebase with args", GitOpRebase, "main", "git rebase main"},
		{"merge with args", GitOpMerge, "feature", "git merge feature"},
		{"checkout with args", GitOpCheckout, "-b new", "git checkout -b new"},
		{"tag with args", GitOpTag, "v1.0", "git tag v1.0"},
		{"clean with args", GitOpClean, "-fd", "git clean -fd"},
		{"stash with args", GitOpStash, "save", "git stash save"},
		{"am with args", GitOpAm, "msg.patch", "git am msg.patch"},
		{"apply with args", GitOpApply, "patch.diff", "git apply patch.diff"},
		{"cherry-pick with args", GitOpCherryPick, "abc123", "git cherry-pick abc123"},
		{"revert with args", GitOpRevert, "abc123", "git revert abc123"},
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

func TestBuildGitCommand_BranchDeleteUsesBranchNotBranchDelete(t *testing.T) {
	// Special case: branch_delete uses 'branch' not 'branch-delete'
	t.Run("with args", func(t *testing.T) {
		got := buildGitCommand(GitOpBranchDelete, "-d feature")
		if got != "git branch -d feature" {
			t.Errorf("got = %q; want %q", got, "git branch -d feature")
		}
	})
	t.Run("no args", func(t *testing.T) {
		got := buildGitCommand(GitOpBranchDelete, "")
		if got != "git branch" {
			t.Errorf("got = %q; want %q", got, "git branch")
		}
	})
}

func TestBuildGitCommand_UnderscoreToHyphenConversion(t *testing.T) {
	// cherry_pick becomes cherry-pick
	got := buildGitCommand(GitOpCherryPick, "ref")
	if got != "git cherry-pick ref" {
		t.Errorf("got = %q; want %q", got, "git cherry-pick ref")
	}
	// branch_delete is special-cased to 'branch' (not branch-delete)
	got = buildGitCommand(GitOpBranchDelete, "")
	if got == "git branch-delete" {
		t.Errorf("branch_delete should produce 'git branch', not 'git branch-delete', got = %q", got)
	}
}

func TestBuildGitCommand_EmptyArgs(t *testing.T) {
	tests := []struct {
		name     string
		op       GitOperationType
		expected string
	}{
		{"commit no args", GitOpCommit, "git commit"},
		{"push no args", GitOpPush, "git push"},
		{"stash no args", GitOpStash, "git stash"},
		{"am no args", GitOpAm, "git am"},
		{"add no args", GitOpAdd, "git add"},
		{"rm no args", GitOpRm, "git rm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildGitCommand(tt.op, "")
			if got != tt.expected {
				t.Errorf("buildGitCommand(%q, \"\") = %q; want %q", tt.op, got, tt.expected)
			}
		})
	}
}

func TestBuildGitCommand_MultiWordArgs(t *testing.T) {
	tests := []struct {
		name     string
		op       GitOperationType
		args     string
		expected string
	}{
		{"multi-word commit message", GitOpCommit, "-m 'feat: initial commit'", "git commit -m 'feat: initial commit'"},
		{"multi-word mv", GitOpMv, "src/a.go src/b.go", "git mv src/a.go src/b.go"},
		{"multi-word reset", GitOpReset, "--hard HEAD~1", "git reset --hard HEAD~1"},
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
// ExecuteGitOperation – commit delegates to GitCommitFlowExecutor
// ---------------------------------------------------------------------------

type mockCommitFlowExecutorGitBuild struct {
	output string
	err    error
}

func (m *mockCommitFlowExecutorGitBuild) ExecuteGitCommitFlow() (string, error) {
	return m.output, m.err
}

func TestExecuteGitOperation_CommitDelegatesToExecutor(t *testing.T) {
	op := GitOperation{Operation: GitOpCommit, Args: "-m 'test'"}
	executor := &mockCommitFlowExecutorGitBuild{output: "commit ok", err: nil}

	got, err := ExecuteGitOperation(context.Background(), op, "", executor, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "commit ok" {
		t.Errorf("got = %q; want %q", got, "commit ok")
	}
}

func TestExecuteGitOperation_CommitExecutorError(t *testing.T) {
	op := GitOperation{Operation: GitOpCommit, Args: ""}
	executor := &mockCommitFlowExecutorGitBuild{output: "", err: context.Canceled}

	_, err := ExecuteGitOperation(context.Background(), op, "", executor, nil)
	if err == nil {
		t.Fatal("expected error from commit flow executor, got nil")
	}
}

func TestExecuteGitOperation_NilCommitExecutor(t *testing.T) {
	op := GitOperation{Operation: GitOpCommit, Args: "-m 'test'"}
	_, err := ExecuteGitOperation(context.Background(), op, "", nil, nil)
	if err == nil {
		t.Fatal("expected error when commit operation has no executor, got nil")
	}
	if !strings.Contains(err.Error(), "commit operation requires") {
		t.Errorf("error = %q; want error containing %q", err.Error(), "commit operation requires")
	}
}

// ---------------------------------------------------------------------------
// ExecuteGitOperation – other ops require GitApprovalPrompter approval
// ---------------------------------------------------------------------------

type mockApprovalPrompterGitBuild struct {
	approve bool
	err     error
}

func (m *mockApprovalPrompterGitBuild) PromptForApproval(_ string) (bool, error) {
	return m.approve, m.err
}

func TestExecuteGitOperation_ApprovalGrantedProceedsToExecution(t *testing.T) {
	op := GitOperation{Operation: GitOpPush, Args: "--force"}
	prompter := &mockApprovalPrompterGitBuild{approve: true, err: nil}

	_, err := ExecuteGitOperation(context.Background(), op, "", nil, prompter)
	// We expect git execution error (not approval error) because approval was granted
	// This proves the approval was passed and execution was attempted
	if err != nil {
		if strings.Contains(err.Error(), "cancelled by user") {
			t.Fatal("approval was granted but operation was cancelled")
		}
		// Expected: git command execution error (git not available or no repo)
	}
	// Success (running in a git repo) is also acceptable
}

func TestExecuteGitOperation_RejectedApproval(t *testing.T) {
	op := GitOperation{Operation: GitOpPush, Args: "--force"}
	prompter := &mockApprovalPrompterGitBuild{approve: false, err: nil}

	_, err := ExecuteGitOperation(context.Background(), op, "", nil, prompter)
	if err == nil {
		t.Fatal("expected error when approval is denied, got nil")
	}
	expected := "git operation cancelled by user"
	if err.Error() != expected {
		t.Errorf("error = %q; want %q", err.Error(), expected)
	}
}

func TestExecuteGitOperation_ApprovalPromptFails(t *testing.T) {
	op := GitOperation{Operation: GitOpPush, Args: "--force"}
	prompter := &mockApprovalPrompterGitBuild{approve: false, err: context.DeadlineExceeded}

	_, err := ExecuteGitOperation(context.Background(), op, "", nil, prompter)
	if err == nil {
		t.Fatal("expected error when approval prompt fails, got nil")
	}
	expected := "get user approval: context deadline exceeded"
	if err.Error() != expected {
		t.Errorf("error = %q; want %q", err.Error(), expected)
	}
}

func TestExecuteGitOperation_NilApprovalPrompterSkipsApproval(t *testing.T) {
	op := GitOperation{Operation: GitOpAdd, Args: "file.go"}

	// nil approvalPrompter skips approval check, goes straight to execution
	_, err := ExecuteGitOperation(context.Background(), op, "", nil, nil)
	// May fail on actual git execution but should NOT fail on approval
	if err != nil {
		if err.Error() == "git operation cancelled by user" {
			t.Fatal("nil approvalPrompter should skip approval but got cancelled error")
		}
		// Expected: git command execution error
	}
}

// ---------------------------------------------------------------------------
// GitOperationType constant values
// ---------------------------------------------------------------------------

func TestGitOperationType_AllConstants(t *testing.T) {
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
