package agent

// tool_handlers_shell_git.go — the git-operation tool handler:
// handleGitOperation (read-only git ops + the commit sub-path),
// isValidGitOperation, and handleGitCommitOperation. The commit / PR
// handlers live in tool_handlers_shell_commit.go. Split out of
// tool_handlers_shell.go.
import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// handleGitOperation handles git operations with approval for write operations
func handleGitOperation(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Extract operation parameter
	operationParam, err := convertToString(args["operation"], "operation")
	if err != nil {
		return "", agenterrors.NewInvalidInputError("failed to convert operation parameter", err)
	}

	// Parse and validate the operation type
	operation := tools.GitOperationType(operationParam)

	// Validate that the operation type is known
	if !isValidGitOperation(operation) {
		validOpNames := []string{"commit", "push", "add", "rm", "mv", "reset", "rebase", "merge", "checkout", "branch_delete", "tag", "clean", "stash", "am", "apply", "cherry_pick", "revert", "pull", "fetch", "restore"}
		return "", agenterrors.NewInvalidInputError(fmt.Sprintf("invalid git operation type '%s'. Valid operations: %s. For read-only operations like status, log, diff, etc., use shell_command instead.", operationParam, strings.Join(validOpNames, ", ")), nil)
	}

	// Extract args parameter (optional)
	var argsStr string
	if argsParam, exists := args["args"]; exists {
		var err error
		argsStr, err = convertToString(argsParam, "args")
		if err != nil {
			return "", agenterrors.NewInvalidInputError("failed to convert args parameter", err)
		}
	}

	// For commit operations, use the commit command directly
	if operation == tools.GitOpCommit {
		return handleGitCommitOperation(a)
	}

	// EA risk cascade: check operation + args for high-risk patterns.
	// Build a pseudo-command string for risk evaluation.
	pseudoCmd := "git " + string(operation)
	if argsStr != "" {
		pseudoCmd += " " + argsStr
	}
	// Resolution mirrors the shell_command risk gate (see comment block
	// above this function):
	//   Critical → ALWAYS reject. No persona, profile, or interactive
	//              prompt can override this.
	//   High     → reject unless the user (or a subagent inheriting root
	//              authority) approves via highRiskApprovedForCommand.
	//              The hard-reject that used to live here meant an
	//              approved `git checkout` was still blocked — the
	//              approval prompt below (approvalPrompter) was never
	//              reached because this check returned early.
	if risk := a.EvaluateOperationRisk(pseudoCmd); risk == configuration.RiskLevelCritical {
		return "", agenterrors.NewSecurityError(
			fmt.Sprintf("critical git operation blocked (cannot be approved by any profile or persona): '%s'", pseudoCmd), nil,
		)
	} else if risk == configuration.RiskLevelHigh {
		if !a.highRiskApprovedForCommand(ctx, pseudoCmd) {
			return "", agenterrors.NewSecurityError(
				fmt.Sprintf("high-risk git operation rejected by persona risk cascade: %s (command: '%s')", risk, pseudoCmd), nil,
			)
		}
	}

	// Basic git ops (add/push/pull/fetch) skip the approval prompt for any
	// persona with CapabilityGitWrite that has cleared isGitWriteAllowed
	// (orchestrator, coordinator, or any custom persona declaring the
	// capability). Other operations (reset, checkout, clean, rm, merge, etc.)
	// always require user approval regardless of persona.
	basicGitOps := operation == tools.GitOpAdd || operation == tools.GitOpPush || operation == tools.GitOpPull || operation == tools.GitOpFetch
	allowWithoutApproval := basicGitOps && a.isGitWriteAllowed()

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
		return "", agenterrors.NewTransientError(fmt.Sprintf("failed to execute git operation %s", operation), err)
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
		tools.GitOpPull, tools.GitOpFetch, tools.GitOpRestore,
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
	dir := a.effectiveCwd()
	cmd := exec.Command("git", "diff", "--staged", "--name-only")
	cmd.Dir = dir
	stagedOutput, err := cmd.CombinedOutput()
	if err != nil {
		return "", agenterrors.NewTransientError("failed to check for staged changes", err)
	}

	if len(strings.TrimSpace(string(stagedOutput))) == 0 {
		return "No staged changes to commit. Use 'git add' to stage files first.", nil
	}

	// For commit operations, we use the dedicated commit tool that handles
	// the automated commit flow with message generation and security approval.
	return "", agenterrors.NewSecurityError("git commit operations should use the dedicated 'commit' tool or the '/commit' slash command", nil)
}
