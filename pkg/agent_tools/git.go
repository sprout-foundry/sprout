package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GitOperationType defines the type of git operation
type GitOperationType string

const (
	GitOpStatus    GitOperationType = "status"
	GitOpLog       GitOperationType = "log"
	GitOpDiff      GitOperationType = "diff"
	GitOpShow      GitOperationType = "show"
	GitOpBranch    GitOperationType = "branch"
	GitOpRemote    GitOperationType = "remote"
	GitOpRevParse  GitOperationType = "rev_parse"
	GitOpLsFiles   GitOperationType = "ls_files"
	GitOpLsTree    GitOperationType = "ls_tree"
	GitOpLsRemote  GitOperationType = "ls_remote"
	GitOpBlame     GitOperationType = "blame"
	GitOpRefLog    GitOperationType = "reflog"
	GitOpConfig    GitOperationType = "config"
	GitOpCommit    GitOperationType = "commit"
	GitOpPush      GitOperationType = "push"
	GitOpAdd       GitOperationType = "add"
	GitOpRm        GitOperationType = "rm"
	GitOpMv        GitOperationType = "mv"
	GitOpReset     GitOperationType = "reset"
	GitOpRebase    GitOperationType = "rebase"
	GitOpMerge     GitOperationType = "merge"
	GitOpCheckout  GitOperationType = "checkout"
	GitOpBranchDelete GitOperationType = "branch_delete"
	GitOpTag       GitOperationType = "tag"
	GitOpClean     GitOperationType = "clean"
	GitOpStash     GitOperationType = "stash"
	GitOpAm        GitOperationType = "am"
	GitOpApply     GitOperationType = "apply"
	GitOpCherryPick GitOperationType = "cherry_pick"
	GitOpRevert    GitOperationType = "revert"
)

// GitOperation defines a git operation request
type GitOperation struct {
	Operation GitOperationType `json:"operation"`
	Args      string            `json:"args,omitempty"`
}

// GitCommitFlowExecutor is an interface for executing the commit flow
// This allows the git tool to delegate commit operations without creating import cycles
type GitCommitFlowExecutor interface {
	ExecuteGitCommitFlow() (string, error)
}

// GitApprovalPrompter is an interface for prompting the user for approval
// This avoids importing the agent package and creating import cycles
type GitApprovalPrompter interface {
	PromptForApproval(command string) (bool, error)
}

// isWriteOperation returns true if the git operation is a write operation
func isWriteOperation(op GitOperationType) bool {
	switch op {
	case GitOpCommit, GitOpPush, GitOpAdd, GitOpRm, GitOpMv, GitOpReset,
		GitOpRebase, GitOpMerge, GitOpCheckout, GitOpBranchDelete, GitOpTag,
		GitOpClean, GitOpStash, GitOpAm, GitOpApply, GitOpCherryPick, GitOpRevert:
		return true
	default:
		return false
	}
}

// GitWriteOperations returns a list of all git write operations
func GitWriteOperations() []GitOperationType {
	return []GitOperationType{
		GitOpCommit, GitOpPush, GitOpAdd, GitOpRm, GitOpMv, GitOpReset,
		GitOpRebase, GitOpMerge, GitOpCheckout, GitOpBranchDelete, GitOpTag,
		GitOpClean, GitOpStash, GitOpAm, GitOpApply, GitOpCherryPick, GitOpRevert,
	}
}

// ExecuteGitOperation executes a git operation with approval for write operations
func ExecuteGitOperation(ctx context.Context, op GitOperation, sessionID string, commitFlowExecutor GitCommitFlowExecutor, approvalPrompter GitApprovalPrompter) (string, error) {
	// For commit operations, delegate to the commit flow executor
	if op.Operation == GitOpCommit {
		if commitFlowExecutor == nil {
			return "", fmt.Errorf("commit operation requires a commit flow executor")
		}
		return commitFlowExecutor.ExecuteGitCommitFlow()
	}

	// Check if this is a write operation
	if isWriteOperation(op.Operation) {
		// Build the full git command for display
		cmd := buildGitCommand(op.Operation, op.Args)

		// Require user approval
		if approvalPrompter != nil {
			approved, err := approvalPrompter.PromptForApproval(cmd)
			if err != nil {
				return "", fmt.Errorf("failed to get user approval: %w", err)
			}
			if !approved {
				return "", fmt.Errorf("git operation cancelled by user")
			}
		}
	}

	// Execute the git command
	return executeGitCommand(op.Operation, op.Args)
}

// PromptForGitApprovalStdin prompts for git approval using stdin
func PromptForGitApprovalStdin(command string) (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("\n🔒 Git Operation Requires Approval\n")
	fmt.Printf("Command: %s\n", command)
	fmt.Printf("\n")
	fmt.Printf("Approve? (y/n): ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	return input == "y" || input == "yes", nil
}

// buildGitCommand builds the full git command string
func buildGitCommand(op GitOperationType, args string) string {
	var subcommand string

	// Handle special case: branch_delete becomes "branch" (with -d or -D in args)
	if op == GitOpBranchDelete {
		subcommand = "branch"
	} else {
		// Convert operation type to git subcommand (replace underscores with hyphens)
		subcommand = strings.ReplaceAll(string(op), "_", "-")
	}

	cmd := fmt.Sprintf("git %s", subcommand)
	if args != "" {
		cmd = fmt.Sprintf("%s %s", cmd, args)
	}

	return cmd
}

// executeGitCommand executes a git command
func executeGitCommand(op GitOperationType, args string) (string, error) {
	var cmdArgs []string

	// Handle special case: branch_delete becomes "branch" (with -d or -D in args)
	if op == GitOpBranchDelete {
		cmdArgs = []string{"branch"}
	} else {
		// Convert operation type to git subcommand (replace underscores with hyphens)
		subcommand := strings.ReplaceAll(string(op), "_", "-")
		cmdArgs = []string{subcommand}
	}

	// Add args if provided
	if args != "" {
		cmdArgs = append(cmdArgs, strings.Fields(args)...)
	}

	// Execute the command
	cmd := exec.Command("git", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git command failed: %w\nOutput: %s", err, string(output))
	}

	return string(output), nil
}
