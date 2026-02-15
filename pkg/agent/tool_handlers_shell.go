package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

// Tool handler implementations for shell and git operations

func handleShellCommand(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	command := args["command"].(string)

	// Block git write operations - these must use the git tool for approval
	// Read-only operations (status, log, diff, etc.) are allowed through shell_command
	if isGitWriteCommand(command) {
		return "", fmt.Errorf("git write operations require the git tool for approval. Please use the git tool instead (operation: '%s')", command)
	}

	a.ToolLog("executing command", command)
	return a.executeShellCommandWithTruncation(ctx, command)
}

// handleGitOperation handles git operations with approval for write operations
func handleGitOperation(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Extract operation parameter
	operationParam, err := convertToString(args["operation"], "operation")
	if err != nil {
		return "", err
	}

	// Parse and validate the operation type
	operation := tools.GitOperationType(operationParam)

	// Validate that the operation type is known
	if !isValidGitOperation(operation) {
		validOpNames := []string{"commit", "push", "add", "rm", "mv", "reset", "rebase", "merge", "checkout", "branch_delete", "tag", "clean", "stash", "am", "apply", "cherry_pick", "revert"}
		return "", fmt.Errorf("invalid git operation type '%s'. Valid operations: %s. For read-only operations like status, log, diff, etc., use shell_command instead.",
			operationParam, strings.Join(validOpNames, ", "))
	}

	// Extract args parameter (optional)
	var argsStr string
	if argsParam, exists := args["args"]; exists {
		argsStr, _ = convertToString(argsParam, "args")
	}

	// Log the operation
	a.ToolLog("executing git operation", fmt.Sprintf("%s %s", operation, argsStr))

	// For commit operations, use the commit command directly
	if operation == tools.GitOpCommit {
		return handleGitCommitOperation(a)
	}

	// Create an approval prompter
	approvalPrompter := &gitApprovalPrompterAdapter{agent: a}

	// Execute the git operation
	result, err := tools.ExecuteGitOperation(ctx, tools.GitOperation{
		Operation: operation,
		Args:      argsStr,
	}, "", nil, approvalPrompter)

	if err != nil {
		return "", err
	}

	return result, nil
}

// isValidGitOperation checks if a git operation type is valid
func isValidGitOperation(op tools.GitOperationType) bool {
	// All valid operations are write operations
	validOps := []tools.GitOperationType{
		tools.GitOpCommit, tools.GitOpPush, tools.GitOpAdd, tools.GitOpRm,
		tools.GitOpMv, tools.GitOpReset, tools.GitOpRebase,
		tools.GitOpMerge, tools.GitOpCheckout, tools.GitOpBranchDelete,
		tools.GitOpTag, tools.GitOpClean, tools.GitOpStash,
		tools.GitOpAm, tools.GitOpApply, tools.GitOpCherryPick, tools.GitOpRevert,
	}

	for _, validOp := range validOps {
		if op == validOp {
			return true
		}
	}

	return false
}

// handleGitCommitOperation handles git commit operations
// Note: For the full interactive commit flow, users should use the /commit slash command
func handleGitCommitOperation(a *Agent) (string, error) {
	// Check for staged changes first
	stagedOutput, err := exec.Command("git", "diff", "--staged", "--name-only").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to check for staged changes: %w", err)
	}

	if len(strings.TrimSpace(string(stagedOutput))) == 0 {
		return "No staged changes to commit. Use 'git add' to stage files first.", nil
	}

	// For commit operations, we use a simple git commit with a message prompt
	// This is a simplified version - for full interactive commit flow, users should use /commit
	return "", fmt.Errorf("git commit requires the interactive commit flow. Please use the '/commit' slash command instead for the full interactive experience with message generation")
}

// gitApprovalPrompterAdapter implements the GitApprovalPrompter interface using the Agent
type gitApprovalPrompterAdapter struct {
	agent *Agent
}

// PromptForApproval prompts the user for approval to execute a git write operation
func (a *gitApprovalPrompterAdapter) PromptForApproval(command string) (bool, error) {
	// Build the approval prompt
	prompt := fmt.Sprintf("Execute git command: %s", command)

	// Define choices
	choices := []ChoiceOption{
		{Label: "Approve", Value: "y"},
		{Label: "Cancel", Value: "n"},
	}

	// Show the command to be executed
	fmt.Printf("\n🔒 Git Operation Requires Approval\n")
	fmt.Printf("Command: %s\n", command)
	fmt.Printf("\n")

	// Prompt for choice
	choice, err := a.agent.PromptChoice(prompt, choices)
	if err != nil {
		// If UI is not available, fall back to stdin prompt
		if err == ErrUINotAvailable {
			return tools.PromptForGitApprovalStdin(command)
		}
		return false, err
	}

	return choice == "y", nil
}
