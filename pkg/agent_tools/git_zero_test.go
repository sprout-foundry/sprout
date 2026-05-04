package tools

import (
	"testing"
)

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
