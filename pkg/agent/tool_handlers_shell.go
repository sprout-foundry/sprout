package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
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

	// Block git checkout/switch commands from shell_command for ALL personas.
	// These must go through the git tool which requires explicit user approval.
	// This prevents repo_orchestrator and other autonomous personas from
	// switching branches without user consent.
	if isGitCheckoutSubcommand(command) {
		return "", fmt.Errorf("git checkout/switch/restore operations are not allowed via shell_command. Use the git tool to require explicit user approval (command: '%s')", command)
	}

	// Block git commands that discard changes (restore, reset) from shell_command
	// for ALL personas. These must go through the git tool which requires
	// explicit user approval. This prevents accidental data loss even for the
	// repo_orchestrator persona.
	if isGitDiscardCommand(command) {
		return "", fmt.Errorf("git %s operations are not allowed via shell_command. Use the git tool with operation='restore' or operation='reset' to require explicit user approval (command: '%s')", extractGitSubcommand(command), command)
	}

	// Block git write operations unless the orchestrator persona has permission.
	// Staging operations (git add) are always allowed per policy.
	// Read-only operations (status, log, diff, etc.) are always allowed through shell_command.
	if isGitWriteCommand(command) {
		if isBroadGitAdd(command) {
			// Always block broad git add patterns regardless of persona.
			// Use the git tool with specific file paths for staging.
			return "", fmt.Errorf("broad git add patterns (., -A, --all) are not allowed via shell_command. Use the git tool with operation='add' and specific file paths, or use 'git add <filepath>' via shell_command (command: '%s')", command)
		}
		if !a.isOrchestratorGitWriteAllowed() {
			if a.GetActivePersona() == "orchestrator" {
				return "", fmt.Errorf("git write operations are disabled for the orchestrator. Enable 'Allow orchestrator git write' in settings, or use the commit tool instead (operation: '%s')", command)
			}
			// For commit operations, redirect to the commit tool — this ensures
			// commits go through the proper message generation code path regardless
			// of whether the agent used shell_command or the commit tool.
			if isGitCommitSubcommand(command) {
				a.PrintLine("")
				a.PrintLine("[redirect] Redirecting git commit to 'commit' tool for proper message generation")
				a.PrintLine(fmt.Sprintf("  Original command: %s", command))
				if strings.Contains(command, "--amend") {
					a.PrintLine("  [warning] --amend flag detected but commit tool does not support amending; creating a new commit")
				}
				a.PrintLine("")
				message := extractGitCommitArgs(command)
				commitArgs := map[string]interface{}{}
				if message != "" {
					commitArgs["message"] = message
				}
				return handleCommitTool(ctx, a, commitArgs)
			}
			return "", fmt.Errorf("git write operations use shell_command for read-only operations (status, log, diff, branch, show). Use the git tool with operation='add' for staging, and the commit tool for commits (operation: '%s')", command)
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

	// repo_orchestrator can stage files and push without approval.
	// Other operations (reset, checkout, clean, rm, merge, etc.) always require
	// user approval regardless of persona.
	isRepoOrchestrator := a.GetActivePersona() == "repo_orchestrator"
	allowWithoutApproval := isRepoOrchestrator && (operation == tools.GitOpAdd || operation == tools.GitOpPush)

	var approvalPrompter tools.GitApprovalPrompter
	if !allowWithoutApproval {
		approvalPrompter = &gitApprovalPrompterAdapter{agent: a}
	}

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
	return "", errors.New("git commit operations should use the dedicated 'commit' tool or the '/commit' slash command")
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
		return false, fmt.Errorf("failed to prompt for git approval: %w", err)
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

	// Extract optional notes parameter (for context to integrate into auto-generated commit message)
	var notes string
	if n, exists := args["notes"]; exists {
		var err error
		notes, err = convertToString(n, "notes")
		if err != nil {
			return "", fmt.Errorf("failed to convert notes parameter: %w", err)
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

	// Auto-approve commits for repo_orchestrator — this persona is explicitly
	// opted into by the user and is designed for autonomous commit workflows.
	// Also auto-approve subagents (no interactive UI available).
	// All other personas still require interactive approval.
	persona := a.GetActivePersona()
	isRepoOrchestrator := persona == "repo_orchestrator"
	isSubagent := os.Getenv("LEDIT_FROM_AGENT") == "1" || os.Getenv("LEDIT_SUBAGENT") == "1"

	if !isRepoOrchestrator && !isSubagent {
		// Prompt user for approval before committing (only in interactive mode)
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
	}

	// Execute the commit using the shared helper function
	commitHash, err := executeCommit(message, notes, configManager)
	if err != nil {
		return "", fmt.Errorf("failed to execute commit: %w", err)
	}

	return fmt.Sprintf("Committed successfully: %s", commitHash), nil
}

// executeCommit performs the actual commit operation using the shared git.CommitExecutor
func executeCommit(userMessage, notes string, configManager configManagerInterface) (string, error) {
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

	// Use the shared commit executor — notes are passed as userInstructions to
	// provide context for generating a better commit message (ignored if userMessage is set)
	executor := git.NewCommitExecutor(client, userMessage, notes)
	return executor.ExecuteCommit()
}
