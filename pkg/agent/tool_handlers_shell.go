// Package agent provides the shell command handler with a unified security model.
//
// When UnifiedRiskResolver is ON (the default, set by config_migration.go),
// a single ResolveToolRisk assessment gates every shell command. The unified
// gate (unifiedSecurityGate in tool_security.go) runs once per tool call —
// no Gate 1/Gate 2 bridge or suppression plumbing is needed.
//
// When the flag is OFF (legacy fallback), the older dual-gate model applies:
// Gate 1 (ClassifyToolCall static classifier) + Gate 2 (EvaluateOperationRisk
// persona cascade). Note that the legacy path may double-prompt because the
// suppression bridge was removed in SP-068 Phase 3; the unified resolver is
// the recommended and default path.
package agent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/sprout-foundry/sprout/pkg/git"
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

	// Extract wait_seconds parameter (optional, defaults to 0)
	var waitSeconds int
	if wsParam, exists := args["wait_seconds"]; exists {
		switch v := wsParam.(type) {
		case float64:
			waitSeconds = int(v)
		case int:
			waitSeconds = v
		case string:
			if n, err := strconv.Atoi(v); err == nil {
				waitSeconds = n
			}
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

	// If check_background is set, return output for that session
	if checkBackground != "" {
		return a.checkBackgroundOutput(ctx, checkBackground, waitSeconds)
	}

	command, err := convertToString(args["command"], "command")
	if err != nil {
		return "", agenterrors.NewInvalidInputError("failed to convert command parameter", err)
	}

	// Validate that we have a command to execute (required when not checking background)
	if command == "" {
		return "", agenterrors.NewInvalidInputError("command parameter is required when check_background is not provided", nil)
	}

	// SP-068 Phase 2: when UnifiedRiskResolver is enabled, use the single
	// ResolveToolRisk assessment instead of the individual gates below.
	if cfg := a.GetConfig(); cfg != nil && cfg.UnifiedRiskResolver {
		return a.handleShellCommandUnified(ctx, command, background)
	}

	// Shadow-mode logging: compare old dual-gate decision vs new unified
	// assessment when the flag is off so we can validate parity before
	// flipping the flag (SP-068 Phase 2).
	if a.debug {
		secResult := tools.ClassifyToolCall("shell_command", map[string]interface{}{"command": command})
		unified := a.ResolveToolRisk("shell_command", map[string]interface{}{"command": command})

		// Derive old decision from the static classifier (Gate 1) which is
		// the actual first line of defense in the pre-execute hook
		oldDecision := resolveOldDecision(secResult)

		newDecision := "allow"
		if unified.IsHardBlock || unified.Level == configuration.RiskLevelCritical {
			newDecision = "block"
		} else if unified.Level == configuration.RiskLevelHigh || unified.Level == configuration.RiskLevelMedium {
			newDecision = "prompt"
		}

		match := "true"
		if oldDecision != newDecision {
			match = "false"
		}

		a.debugLog("[shadow-risk] shell_command: old=%s, new=%s, match=%s — %s\n", oldDecision, newDecision, match, unified.Explain())
	}

	// — Legacy dual-gate path (flag OFF) —
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
	if isGitHistoryRewriteCommand(command) {
		if cfg := a.GetConfig(); cfg == nil || !cfg.AllowGitHistoryRewrite {
			return "", agenterrors.NewSecurityError(fmt.Sprintf("git %s can lose commit history and is blocked by default. Use the git tool for explicit user approval, or set allow_git_history_rewrite=true in config to opt in (command: '%s')", extractGitSubcommand(command), command), nil)
		}
	}

	// Block git stash operations. `git stash` saves the working tree and
	// reverts it to HEAD; `git stash pop`/`apply` restores via a 3-way
	// merge that can silently revert files when conflicts arise (the
	// exact bug that caused normalizeGitArgs, staleness guards, and the
	// read.go context refactor to disappear from the working tree).
	// stash list/show are read-only and allowed via shellLooksReadOnly.
	if isGitStashCommand(command) {
		return "", agenterrors.NewSecurityError(
			fmt.Sprintf("git stash operations are blocked via shell_command — stash pop/apply can silently revert files via merge conflicts. Use 'git stash' manually in your terminal if needed (command: '%s')", command), nil)
	}

	// Block git write operations unless the active persona has CapabilityGitWrite.
	// Staging operations (git add) are always allowed per policy.
	// Read-only operations (status, log, diff, etc.) are always allowed through shell_command.
	if isGitWriteCommand(command) {
		if !a.isGitWriteAllowed() {
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

	// Enrich context with workspace root so executeGitCommand runs in the
	// correct directory. The seed execution path passes a bare context
	// without workspace metadata, so we inject it from the agent's config.
	if wsRoot := a.effectiveCwd(); wsRoot != "" {
		ctx = filesystem.WithWorkspaceRoot(ctx, wsRoot)
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

	// Auto-approve commits when the persona has CapabilityGitWrite.
	// Subagents also auto-approve because they have no interactive UI to prompt with.
	isSubagent := a.IsSubagent()
	canGitWrite := a.isGitWriteAllowed()

	if !canGitWrite && !isSubagent {
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

// handleShellCommandUnified is the single-risk-assessment path for shell
// commands when the UnifiedRiskResolver flag is ON (SP-068 Phase 2).
// It folds all security gates into one RiskAssessment via ResolveToolRisk
// and acts on the result.
func (a *Agent) handleShellCommandUnified(ctx context.Context, command string, background bool) (string, error) {
	// Get the unified risk assessment
	assessment := a.ResolveToolRisk("shell_command", map[string]interface{}{"command": command})

	// Log the assessment for diagnostics
	if a.debug {
		a.debugLog("[risk] shell_command unified: %s\n", assessment.Explain())
	}

	// Hard-block / Critical: unconditional deny. This is defense-in-depth —
	// Gate 1 (unifiedSecurityGate) already blocks Critical operations before
	// this handler runs. The check is retained because the handler can be
	// called directly in edge cases, and hard-blocks must always be enforced.
	if assessment.IsHardBlock || assessment.Level == configuration.RiskLevelCritical {
		return "", agenterrors.NewSecurityError(
			fmt.Sprintf("critical operation blocked (cannot be approved by any profile or persona): '%s'", command), nil,
		)
	}

	// High risk: Gate 1 (unifiedSecurityGate) already ran the
	// highRiskApprovedForCommand check before this handler was invoked.
	// Re-checking here is redundant — Gate 1's approval is authoritative for
	// the unified path. Proceed directly to execution for all non-Critical
	// levels (High/Medium/Low). SP-068 Phase 3 removed the redundant Gate 2.
	if background {
		return a.executeShellCommandBackground(ctx, command)
	}
	return a.executeShellCommandWithTruncation(ctx, command)
}

// handleCreatePullRequest handles the create_pull_request tool, creating a
// pull request on GitHub via the git.CreatePullRequest backend. Gated as a
// git-write operation — the persona must have CapabilityGitWrite (or the
// user must approve interactively).
func handleCreatePullRequest(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Extract required title parameter
	title, err := convertToString(args["title"], "title")
	if err != nil {
		return "", agenterrors.NewInvalidInputError("failed to convert title parameter", err)
	}
	if title == "" {
		return "", agenterrors.NewInvalidInputError("title parameter is required and must not be empty", nil)
	}

	// Extract optional body parameter
	var body string
	if b, exists := args["body"]; exists {
		body, err = convertToString(b, "body")
		if err != nil {
			return "", agenterrors.NewInvalidInputError("failed to convert body parameter", err)
		}
	}

	// Extract optional base parameter
	var base string
	if ba, exists := args["base"]; exists {
		base, err = convertToString(ba, "base")
		if err != nil {
			return "", agenterrors.NewInvalidInputError("failed to convert base parameter", err)
		}
	}

	// Extract optional head parameter
	var head string
	if h, exists := args["head"]; exists {
		head, err = convertToString(h, "head")
		if err != nil {
			return "", agenterrors.NewInvalidInputError("failed to convert head parameter", err)
		}
	}

	// Extract optional draft parameter
	var draft bool
	if d, exists := args["draft"]; exists {
		if dBool, ok := d.(bool); ok {
			draft = dBool
		}
	}

	// Extract optional repo_dir parameter
	var repoDir string
	if rd, exists := args["repo_dir"]; exists {
		repoDir, err = convertToString(rd, "repo_dir")
		if err != nil {
			return "", agenterrors.NewInvalidInputError("failed to convert repo_dir parameter", err)
		}
	}

	// Default repoDir to the agent's workspace root
	if repoDir == "" {
		repoDir = a.effectiveCwd()
	}

	// Git-write gate: mirror the handleCommitTool approval pattern.
	isSubagent := a.IsSubagent()
	canGitWrite := a.isGitWriteAllowed()

	if !canGitWrite && !isSubagent {
		// Prompt user for approval before creating PR
		choices := []ChoiceOption{
			{Label: "Approve", Value: "approve"},
			{Label: "Deny", Value: "deny"},
		}

		choice, err := a.PromptChoice("Allow agent to create a pull request?", choices)
		if err != nil {
			if errors.Is(err, ErrUINotAvailable) {
				// Fall back to allowing when UI is not available,
				// since this tool is designed for autonomous agents and was explicitly called
			} else {
				return "", agenterrors.NewTransientError("approval prompt failed", err)
			}
		} else if choice != "approve" {
			return "Pull request creation cancelled by user.", nil
		}
	}

	// Call the backend
	result, err := git.CreatePullRequest(ctx, repoDir, git.PullRequestRequest{
		Title: title,
		Body:  body,
		Base:  base,
		Head:  head,
		Draft: draft,
	})
	if err != nil {
		return "", agenterrors.NewTransientError("failed to create pull request", err)
	}

	return fmt.Sprintf("Pull request created successfully!\n\nURL: %s\nNumber: #%d\nState: %s", result.URL, result.Number, result.State), nil
}
