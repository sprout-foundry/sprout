// Package approvals: CLI approval-choice mapping (SP-141 phase 3, increment 5).
package approvals

import (
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// ApprovalDecisionFromCLIChoice maps the CLI prompt's typed choice onto the
// shared ApprovalDecision.
func ApprovalDecisionFromCLIChoice(c utils.ApprovalChoice) security.ApprovalDecision {
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
