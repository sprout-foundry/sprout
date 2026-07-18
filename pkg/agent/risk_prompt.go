package agent

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// highRiskApprovedForCommand decides whether a high-risk shell command
// is permitted to execute. Delegates to RequestApproval which owns
// surface selection, fallback, and 4-option outcome (SP-068 Phase 3).
//
// The ctx parameter is kept for signature stability across callers
// (tool_security.go, tool_handlers_shell.go) but is not used.
func (a *Agent) highRiskApprovedForCommand(_ context.Context, command string) bool {
	args := map[string]interface{}{"command": command}
	assessment := a.ResolveToolRisk("shell_command", args)
	decision, err := a.RequestApproval(assessment, "shell_command", args)
	if decision.Analysis != nil {
		// Print LLM analysis to stderr/log when present so CLI users see it.
		console.PrintExternal(fmt.Sprintf("[security analysis] %s\n", renderSecurityAnalysisLine(decision.Analysis)))
	}
	return err == nil
}

// renderSecurityAnalysisLine formats a one-line summary of the analysis.
// SP-124.
func renderSecurityAnalysisLine(sa *SecurityAnalysis) string {
	if sa == nil {
		return ""
	}
	return fmt.Sprintf("%s → %s (%s)", sa.Summary, sa.Recommendation, sa.RiskAssessment)
}

// approvalDecisionFromCLIChoice maps the CLI prompt's typed choice onto
// the shared security.ApprovalDecision so the post-prompt handling is
// the same regardless of input surface.
func approvalDecisionFromCLIChoice(c utils.ApprovalChoice) security.ApprovalDecision {
	switch c {
	case utils.ApprovalChoiceApproveOnce:
		return security.ApprovalApproveOnce
	case utils.ApprovalChoiceApproveAlways:
		return security.ApprovalApproveAlways
	case utils.ApprovalChoiceAlwaysAsk:
		return security.ApprovalAlwaysAsk
	case utils.ApprovalChoiceElevate:
		return security.ApprovalElevate
	default:
		return security.ApprovalDeny
	}
}

// applyApprovalDecision performs the side-effects of the user's choice:
// ApproveAlways persists the command to the allowlist; Elevate bumps the
// session profile to permissive and prints a hint about /risk-profile
// for permanent change. ApproveOnce and Deny have no side-effects beyond
// the caller's approve/reject branching.
func (a *Agent) applyApprovalDecision(decision security.ApprovalDecision, command string) {
	switch decision {
	case security.ApprovalApproveAlways:
		if err := a.PersistShellCommandAllowlist(command); err != nil {
			// Surface but don't block — the user still gets one-time
			// approval; persistence failure just means future runs
			// will re-prompt.
			console.PrintExternal(fmt.Sprintf("[approval] failed to persist allowlist entry: %v\n", err))
		}
	case security.ApprovalElevate:
		a.ElevateSessionToPermissive()
		console.PrintExternal("[approval] session risk profile elevated to 'permissive'. Run /risk-profile permissive to make this persistent across restarts.\n")
	}
}
