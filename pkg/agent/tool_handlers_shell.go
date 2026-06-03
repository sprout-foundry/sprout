// Package agent provides the shell command handler with a two-gate security model.
//
// Gate 1 (Global Static Classifier): pkg/agent_tools/security_classifier.go:ClassifyToolCall()
// Inspects tool name + arguments using string-based heuristics. Always runs regardless of persona.
// Can block (ShouldBlock) or prompt (ShouldPrompt) for dangerous operations.
//
// Gate 2 (Persona Risk Cascade): pkg/agent/agent_getters.go:EvaluateOperationRisk()
// Evaluates commands against the active persona's auto_approve_rules. Returns Low/Medium/High.
//
// INVARIANT: Neither gate may suppress or bypass the other. Both evaluate independently.
// The more restrictive result always wins.
package agent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/sprout-foundry/sprout/pkg/git"
	"github.com/sprout-foundry/sprout/pkg/personas"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// configManagerInterface defines the interface for accessing config
type configManagerInterface interface {
	GetProvider() (api.ClientType, error)
	GetModelForProvider(provider api.ClientType) string
}

func handleShellCommand(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Extract check_background parameter (optional)
	var checkBackground string
	if cbParam, exists := args["check_background"]; exists {
		var err error
		checkBackground, err = convertToString(cbParam, "check_background")
		if err != nil {
			return "", agenterrors.NewInvalidInputError("failed to convert check_background parameter", err)
		}
	}

	// Extract stop_background parameter (optional)
	var stopBackground string
	if sbParam, exists := args["stop_background"]; exists {
		var err error
		stopBackground, err = convertToString(sbParam, "stop_background")
		if err != nil {
			return "", agenterrors.NewInvalidInputError("failed to convert stop_background parameter", err)
		}
	}

	// Extract background parameter (optional, defaults to false)
	background := false
	if bgParam, exists := args["background"]; exists {
		if bgBool, ok := bgParam.(bool); ok {
			background = bgBool
		}
	}

	// Reject conflicting parameters
	if checkBackground != "" && background {
		return "", agenterrors.NewInvalidInputError("check_background and background=true cannot be used together — check_background retrieves output, background runs a new command", nil)
	}
	if stopBackground != "" && background {
		return "", agenterrors.NewInvalidInputError("stop_background and background=true cannot be used together — stop_background stops a session, background runs a new command", nil)
	}
	if stopBackground != "" && checkBackground != "" {
		return "", agenterrors.NewInvalidInputError("stop_background and check_background cannot be used together", nil)
	}

	// If stop_background is set, terminate the session and return immediately
	if stopBackground != "" {
		return a.stopBackgroundSession(stopBackground)
	}

	// If check_background is set, return output for that session immediately
	if checkBackground != "" {
		return a.checkBackgroundOutput(ctx, checkBackground)
	}

	command, err := convertToString(args["command"], "command")
	if err != nil {
		return "", agenterrors.NewInvalidInputError("failed to convert command parameter", err)
	}

	// Validate that we have a command to execute (required when not checking background)
	if command == "" {
		return "", agenterrors.NewInvalidInputError("command parameter is required when check_background is not provided", nil)
	}

	// Risk cascade for personas / risk profiles (SP-058).
	// Resolution:
	//   Critical → ALWAYS reject (rm -rf root, fork bomb). No persona,
	//              profile, or interactive prompt can override this.
	//   High     → if EA persona: auto-approve (EA reasons via prompt);
	//              else if interactive: prompt the user;
	//              else: reject (non-interactive can't ask).
	//   Medium   → allow; persona system prompt guides reasoning.
	//   Low      → allow.
	if risk := a.EvaluateOperationRisk(command); risk == configuration.RiskLevelCritical {
		return "", agenterrors.NewSecurityError(
			fmt.Sprintf("critical operation blocked (cannot be approved by any profile or persona): '%s'", command), nil,
		)
	} else if risk == configuration.RiskLevelHigh {
		if !a.highRiskApprovedForCommand(ctx, command) {
			return "", agenterrors.NewSecurityError(
				fmt.Sprintf("high-risk operation rejected by persona risk cascade: %s (command: '%s')", risk, command), nil,
			)
		}
	}

	// Block git commands that lose commit history unless the workspace
	// has opted into the more-permissive `AllowGitHistoryRewrite` mode.
	//
	// What this gate now covers:
	//   - `git reset --hard <commit-ish>` (backward ref move)
	//   - `git rebase` (any form — rewrites commits)
	//   - `git branch -d/-D/--delete`
	//   - `git tag -d/--delete`
	//
	// What it deliberately DOESN'T cover anymore (used to be blocked
	// unconditionally): `checkout`, `switch`, `restore`, `reset` without
	// `--hard <commit-ish>`, `clean`, `rm`, `mv`, `stash pop/apply/drop`,
	// `cherry-pick`, `revert`, `am`, `apply`. These mutate only the
	// working tree, which the change tracker captures (shellIsDestructive
	// → walkWorkspace destructive mode → recoverable bulk entries), so
	// recover_file / recover_bulk are the recovery story instead of an
	// up-front block.
	if isGitHistoryRewriteCommand(command) {
		if cfg := a.GetConfig(); cfg == nil || !cfg.AllowGitHistoryRewrite {
			return "", agenterrors.NewSecurityError(fmt.Sprintf("git %s can lose commit history and is blocked by default. Use the git tool for explicit user approval, or set allow_git_history_rewrite=true in config to opt in (command: '%s')", extractGitSubcommand(command), command), nil)
		}
	}

	// Block git write operations unless the orchestrator persona has permission.
	// Staging operations (git add) are always allowed per policy.
	// Read-only operations (status, log, diff, etc.) are always allowed through shell_command.
	if isGitWriteCommand(command) {
		if !a.isOrchestratorGitWriteAllowed() {
			persona := a.GetActivePersona()
			if persona == personas.IDOrchestrator {
				return "", agenterrors.NewSecurityError(fmt.Sprintf("git write operations are disabled for %s. Enable 'Allow orchestrator git write' in settings, or use the commit tool instead (operation: '%s')", persona, command), nil)
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
			return "", agenterrors.NewSecurityError(fmt.Sprintf("git write operations use shell_command for read-only operations (status, log, diff, branch, show). Use the git tool with operation='add' for staging, and the commit tool for commits (operation: '%s')", command), nil)
		}
	}

	// If background mode is requested, use the background execution path
	if background {
		return a.executeShellCommandBackground(ctx, command)
	}

	// Otherwise, use the normal synchronous execution path
	return a.executeShellCommandWithTruncation(ctx, command)
}

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
	if risk := a.EvaluateOperationRisk(pseudoCmd); risk == configuration.RiskLevelHigh {
		return "", agenterrors.NewSecurityError(
			fmt.Sprintf("high-risk git operation rejected by persona risk cascade: %s (command: '%s')", risk, pseudoCmd), nil,
		)
	}

	// Enrich context with workspace root so executeGitCommand runs in the
	// correct directory. The seed execution path passes a bare context
	// without workspace metadata, so we inject it from the agent's config.
	if wsRoot := a.effectiveCwd(); wsRoot != "" {
		ctx = filesystem.WithWorkspaceRoot(ctx, wsRoot)
	}

	// The orchestrator can stage files and push without approval when the user
	// has opted into git-write via AllowOrchestratorGitWrite.
	// Personas with EA auto-approve rules (e.g., executive_assistant) that include
	// git write operations in their medium-risk list can also stage/push/pull/fetch
	// without interactive approval (the EA reasons about these itself).
	// Other operations (reset, checkout, clean, rm, merge, etc.) always require
	// user approval regardless of persona.
	isOrchestratorWithGitWrite := a.GetActivePersona() == personas.IDOrchestrator && a.isOrchestratorGitWriteAllowed()
	basicGitOps := operation == tools.GitOpAdd || operation == tools.GitOpPush || operation == tools.GitOpPull || operation == tools.GitOpFetch
	allowWithoutApproval := isOrchestratorWithGitWrite && basicGitOps
	if !allowWithoutApproval && basicGitOps && a.hasEAGitWriteApproval() {
		allowWithoutApproval = true
	}

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

// gitApprovalPrompterAdapter implements the GitApprovalPrompter interface using the Agent
type gitApprovalPrompterAdapter struct {
	agent *Agent
}

// PromptForApproval prompts the user for approval to execute a git write operation.
// When a WebUI client is connected, the approval request is routed through the event
// bus so a dialog appears in the browser. Otherwise it falls back to the terminal UI
// or stdin.
func (a *gitApprovalPrompterAdapter) PromptForApproval(command string) (bool, error) {
	ag := a.agent

	// Prefer the WebUI approval path when a browser tab is connected and the
	// security approval manager is available. This mirrors the pattern used by
	// the main ExecuteTool security flow in tool_definitions.go.
	if mgr := ag.GetSecurityApprovalMgr(); mgr != nil && ag.GetEventBus() != nil && !ag.IsSubagent() && ag.HasActiveWebUIClients() {
		if ag.debug {
			ag.debugLog("[GIT] Requesting git approval via webui for: %s\n", command)
		}
		clientID := ag.GetEventClientID()
		userID := ag.GetEventUserID()
		approved := mgr.RequestToolApproval(ag.GetEventBus(), clientID, userID, "git", "CAUTION", fmt.Sprintf("Git operation: %s", command), nil)
		return approved, nil
	}

	// Terminal UI or stdin fallback
	prompt := fmt.Sprintf("Execute git command: %s", command)

	choices := []ChoiceOption{
		{Label: "Approve", Value: "y"},
		{Label: "Cancel", Value: "n"},
	}

	fmt.Printf("\n[LOCK] Git Operation Requires Approval\n")
	fmt.Printf("Command: %s\n", command)
	fmt.Printf("\n")

	choice, err := ag.PromptChoice(prompt, choices)
	if err != nil {
		if err == ErrUINotAvailable {
			return tools.PromptForGitApprovalStdin(command)
		}
		return false, agenterrors.NewTransientError("failed to prompt for git approval", err)
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
			return "", agenterrors.NewInvalidInputError("failed to convert message parameter", err)
		}
	}

	// Extract optional notes parameter (for context to integrate into auto-generated commit message)
	var notes string
	if n, exists := args["notes"]; exists {
		var err error
		notes, err = convertToString(n, "notes")
		if err != nil {
			return "", agenterrors.NewInvalidInputError("failed to convert notes parameter", err)
		}
	}

	// Check for staged changes first
	dir := a.effectiveCwd()
	cmd := exec.Command("git", "diff", "--staged", "--name-only")
	cmd.Dir = dir
	stagedOutput, err := cmd.CombinedOutput()
	if err != nil {
		return "", agenterrors.NewTransientError("failed to check for staged changes", err)
	}

	if len(strings.TrimSpace(string(stagedOutput))) == 0 {
		return "No staged changes to commit. Stage files first using 'git add' or the git tool, then use the commit tool.", nil
	}

	// Get the agent's config manager to access provider settings
	var configManager configManagerInterface
	if cm := a.GetConfigManager(); cm != nil {
		configManager = cm
	}

	// EA risk cascade: reject commit if the message or notes contain force flags
	// or other high-risk patterns. This prevents the EA from being tricked into
	// committing messages with embedded shell commands or dangerous patterns.
	// Note: This is a defense-in-depth check; commit messages are not shell commands,
	// but an LLM might construct a message containing patterns that could be
	// misinterpreted by downstream systems.
	if message != "" {
		if risk := a.EvaluateOperationRisk(message); risk == configuration.RiskLevelHigh {
			return "", agenterrors.NewSecurityError(
				fmt.Sprintf("commit rejected by persona risk cascade: high-risk pattern detected in message (message: '%s')", message), nil,
			)
		}
	}

	// Auto-approve commits for the orchestrator when AllowOrchestratorGitWrite
	// is enabled — that flag is the user's explicit opt-in to autonomous commit
	// workflows. Also auto-approve subagents (no interactive UI available)
	// and personas with EA auto-approve rules (executive_assistant). All other
	// personas still require interactive approval.
	persona := a.GetActivePersona()
	isOrchestratorWithGitWrite := persona == personas.IDOrchestrator && a.isOrchestratorGitWriteAllowed()
	isSubagent := a.IsSubagent()
	hasEAAutoApprove := a.hasEAGitWriteApproval()

	if !isOrchestratorWithGitWrite && !isSubagent && !hasEAAutoApprove {
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
				return "", agenterrors.NewTransientError("approval prompt failed", err)
			}
		} else if choice != "approve" {
			return "Commit cancelled by user.", nil
		}
	}

	// Execute the commit using the shared helper function
	commitHash, err := executeCommit(message, notes, configManager, a)
	if err != nil {
		return "", agenterrors.NewTransientError("failed to execute commit", err)
	}

	return fmt.Sprintf("Committed successfully: %s", commitHash), nil
}

// executeCommit performs the actual commit operation using the shared git.CommitExecutor.
// The agent is optional; when non-nil its elevation gate is consulted for secret
// detection before the commit is created.
func executeCommit(userMessage, notes string, configManager configManagerInterface, chatAgent *Agent) (string, error) {
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

	var executor *git.CommitExecutor

	// Get workspace directory from the agent, if available.
	workDir := ""
	if chatAgent != nil {
		workDir = chatAgent.effectiveCwd()
	}

	// Wire the elevation gate into the commit executor if available.
	if chatAgent != nil && chatAgent.GetElevationGate() != nil {
		gate := chatAgent.GetElevationGate()
		secretHandler := func(securityResult git.CommitSecurityResult) bool {
			if !securityResult.HasConcerns {
				return true
			}
			action, err := gate.Evaluate(securityResult.Concerns, "commit")
			if err != nil {
				chatAgent.debugLog("[security] commit elevation error: %v\n", err)
				return false // default to blocking on error
			}
			return action != security.SecretBlock
		}
		executor = git.NewCommitExecutorWithSecurityCheck(client, userMessage, notes, secretHandler)
	} else {
		executor = git.NewCommitExecutor(client, userMessage, notes)
	}
	executor.Dir = workDir

	return executor.ExecuteCommit()
}
