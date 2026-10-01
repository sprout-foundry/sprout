package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/sprout-foundry/sprout/pkg/shelltext"
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

	// Enrich context with workspace root, effective cwd, and session folders
	// BEFORE any dispatch: every execution path below (unified, legacy,
	// background) shells out through runShellCommand, which resolves cmd.Dir
	// from this context and falls back to os.Getwd() when it is missing —
	// the daemon's start directory in daemon mode, or the package source dir
	// inside a real repo during tests.
	workspaceRoot := a.GetWorkspaceRoot()
	effectiveCwd := a.effectiveCwd()
	sessionFolders := a.SnapshotSessionAllowedFolders()
	ctx = filesystem.WithAgentContext(
		filesystem.WithWorkspaceRoot(ctx, workspaceRoot),
		effectiveCwd,
		sessionFolders,
	)

	// When UnifiedRiskResolver is enabled, return the single
	// ResolveToolRisk assessment instead of the individual gates below.
	if cfg := a.GetConfig(); cfg != nil && cfg.UnifiedRiskResolver {
		return a.handleShellCommandUnified(ctx, command, background)
	}

	// Shadow-mode logging: compare old dual-gate decision vs new unified
	// assessment when the flag is off so we can validate parity before
	// flipping the flag.
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
	// Risk cascade for personas / risk profiles.
	// Resolution:
	//   Critical → ALWAYS reject (rm -rf root, fork bomb). No persona,
	//              profile, or interactive prompt can override this.
	//   High     → if EA persona: auto-approve (EA reasons via prompt);
	//              else if interactive: prompt the user;
	//              else: reject (non-interactive can't ask).
	//   Medium   → allow; persona system prompt guides reasoning.
	//   Low      → allow.
	//
	// historyRewriteAlreadyApproved tracks whether the High-tier prompt
	// below already approved this command, so the git history-rewrite
	// gate doesn't re-prompt for the same operation.
	historyRewriteAlreadyApproved := false
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
		historyRewriteAlreadyApproved = true
	}

	// Prompt for git commands that can lose commit history (recoverable
	// via reflog). AllowGitHistoryRewrite=true skips the prompt entirely.
	// If the persona cascade above already prompted and approved (e.g.
	// git reset --hard is HighRiskNever → High), skip the re-prompt.
	if shelltext.IsGitHistoryRewriteCommand(command) && !historyRewriteAlreadyApproved {
		if cfg := a.GetConfig(); cfg == nil || !cfg.AllowGitHistoryRewrite {
			if !a.highRiskApprovedForCommand(ctx, command) {
				return "", agenterrors.NewSecurityError(fmt.Sprintf("git %s can lose commit history and was not approved (command: '%s')", extractGitSubcommand(command), command), nil)
			}
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
				a.PrintLine("Redirecting git commit to 'commit' tool for proper message generation")
				a.PrintLine(fmt.Sprintf("  Original command: %s", command))
				if strings.Contains(command, "--amend") {
					a.PrintLine("  --amend flag detected but commit tool does not support amending; creating a new commit")
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

// handleShellCommandUnified is the single-risk-assessment path for shell
// commands when the UnifiedRiskResolver flag is ON.
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
	// levels (High/Medium/Low). The redundant Gate 2 was removed.
	if background {
		return a.executeShellCommandBackground(ctx, command)
	}
	return a.executeShellCommandWithTruncation(ctx, command)
}
