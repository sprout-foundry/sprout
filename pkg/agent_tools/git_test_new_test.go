package tools

import (
	"context"
	"testing"
)

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
