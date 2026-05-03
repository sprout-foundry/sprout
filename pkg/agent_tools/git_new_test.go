package tools

import (
	"context"
	"testing"
)

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
