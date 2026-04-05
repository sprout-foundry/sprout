package agent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/git"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

// configManagerInterface defines the interface for accessing config
type configManagerInterface interface {
	GetProvider() (api.ClientType, error)
	GetModelForProvider(provider api.ClientType) string
}

func handleShellCommand(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	command, err := convertToString(args["command"], "command")
	if err != nil {
		return "", fmt.Errorf("failed to convert command parameter: %w", err)
	}

	// Block git write operations unless the orchestrator persona has permission.
	// Staging operations (git add) are always allowed per policy.
	// Read-only operations (status, log, diff, etc.) are always allowed through shell_command.
	if isGitWriteCommand(command) {
		if !a.isOrchestratorGitWriteAllowed() {
			if a.GetActivePersona() == "orchestrator" {
				return "", fmt.Errorf("git write operations are disabled for the orchestrator. Enable 'Allow orchestrator git write' in settings, or use the git tool instead (operation: '%s')", command)
			}
			return "", fmt.Errorf("git write operations require the git tool for approval. Please use the git tool instead (operation: '%s')", command)
		}
	}

	return a.executeShellCommandWithTruncation(ctx, command)
}

// handleGitOperation handles git operations with approval for write operations
func handleGitOperation(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Extract operation parameter
	operationParam, err := convertToString(args["operation"], "operation")
	if err != nil {
		return "", fmt.Errorf("failed to convert operation parameter: %w", err)
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
		var err error
		argsStr, err = convertToString(argsParam, "args")
		if err != nil {
			return "", fmt.Errorf("failed to convert args parameter: %w", err)
		}
	}

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
		return "", fmt.Errorf("failed to execute git operation %s: %w", operation, err)
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

	// For commit operations, we use the dedicated commit tool that handles
	// the automated commit flow with message generation and security approval.
	return "", fmt.Errorf("git commit operations should use the dedicated 'commit' tool or the '/commit' slash command")
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
	fmt.Printf("\n[LOCK] Git Operation Requires Approval\n")
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

// handleCommitTool handles the dedicated commit tool
// This tool allows committing without requiring user interaction,
// using the automated commit flow with message generation
func handleCommitTool(_ context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Extract optional message parameter
	var message string
	if msg, exists := args["message"]; exists {
		var err error
		message, err = convertToString(msg, "message")
		if err != nil {
			return "", fmt.Errorf("failed to convert message parameter: %w", err)
		}
	}

	// Check for staged changes first
	stagedOutput, err := exec.Command("git", "diff", "--staged", "--name-only").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to check for staged changes: %w", err)
	}

	if len(strings.TrimSpace(string(stagedOutput))) == 0 {
		return "No staged changes to commit. Stage files first using 'git add' or the git tool, then use the commit tool.", nil
	}

	// Get the agent's config manager to access provider settings
	var configManager configManagerInterface
	if cm := a.GetConfigManager(); cm != nil {
		configManager = cm
	}

	// Prompt user for approval before committing
	choices := []ChoiceOption{
		{Label: "Approve", Value: "approve"},
		{Label: "Deny", Value: "deny"},
	}

	choice, err := a.PromptChoice("Allow agent to commit staged changes?", choices)
	if err != nil {
		if errors.Is(err, ErrUINotAvailable) {
			// Fall back to allowing the commit when UI is not available,
			// since this tool is designed for autonomous agents and was explicitly called
		} else {
			return "", fmt.Errorf("approval prompt failed: %w", err)
		}
	} else if choice != "approve" {
		return "Commit cancelled by user.", nil
	}

	// Execute the commit using the shared helper function
	commitHash, err := executeCommit(message, configManager)
	if err != nil {
		return "", fmt.Errorf("failed to execute commit: %w", err)
	}

	return fmt.Sprintf("Committed successfully: %s", commitHash), nil
}

// executeCommit performs the actual commit operation using the shared git.CommitExecutor
func executeCommit(userMessage string, configManager configManagerInterface) (string, error) {
	// Create LLM client if config manager is available
	var client api.ClientInterface
	if configManager != nil {
		provider, err := configManager.GetProvider()
		if err == nil {
			model := configManager.GetModelForProvider(provider)
			client, err = factory.CreateProviderClient(api.ClientType(provider), model)
			if err != nil {
				client = nil
			}
		}
	}

	// Use the shared commit executor
	executor := git.NewCommitExecutor(client, userMessage, "")
	return executor.ExecuteCommit()
}
