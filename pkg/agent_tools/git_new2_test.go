package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

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
