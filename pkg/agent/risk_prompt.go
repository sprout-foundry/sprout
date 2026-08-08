package agent

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// highRiskApprovedForCommand decides whether a high-risk shell command is permitted.
// Delegates to RequestApproval for surface selection and 4-option outcome.
func (a *Agent) highRiskApprovedForCommand(_ context.Context, command string) bool {
	args := map[string]interface{}{"command": command}
	assessment := a.ResolveToolRisk("shell_command", args)
	decision, err := a.RequestApproval(assessment, "shell_command", args)
	_ = decision.Analysis
	return err == nil
}

// approvalDecisionFromCLIChoice maps the CLI prompt's typed choice onto the shared ApprovalDecision.
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
// ApproveAlways persists the command; Elevate bumps the session profile.
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
