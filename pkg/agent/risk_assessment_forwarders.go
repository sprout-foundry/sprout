// Package agent: risk-assessment vocabulary + pure decision helpers —
// aliases and forwarders into pkg/agent/approvals (SP-141 phase 3,
// increment 4). The implementation (RiskSource, RiskAssessment, the
// Combine/Explain/decision helpers, and the git-command gate detectors)
// lives in approvals; these keep the historical pkg/agent identifiers
// working for ResolveToolRisk, the tool-handler gates, and the
// seed/tool-security call sites.
package agent

import (
	"github.com/sprout-foundry/sprout/pkg/agent/approvals"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// RiskSource and RiskAssessment are aliases to the moved types.
type RiskSource = approvals.RiskSource
type RiskAssessment = approvals.RiskAssessment

// The RiskSource taxonomy, aliased so existing references are unchanged.
const (
	RiskSourceClassifier        = approvals.RiskSourceClassifier
	RiskSourcePersonaCascade    = approvals.RiskSourcePersonaCascade
	RiskSourceCriticalOp        = approvals.RiskSourceCriticalOp
	RiskSourceGitHistoryRewrite = approvals.RiskSourceGitHistoryRewrite
	RiskSourceGitRebase         = approvals.RiskSourceGitRebase
	RiskSourceGitWrite          = approvals.RiskSourceGitWrite
	RiskSourceFSTier            = approvals.RiskSourceFSTier
	RiskSourceWorkspacePolicy   = approvals.RiskSourceWorkspacePolicy
	RiskSourceHandler           = approvals.RiskSourceHandler
	RiskSourcePasswordPrompter  = approvals.RiskSourcePasswordPrompter
)

// assessmentFromClassifier maps a static-classifier SecurityResult onto
// the canonical risk scale (SAFE→Low, CAUTION→Medium, DANGEROUS→High,
// hard-block→Critical).
func assessmentFromClassifier(res tools.SecurityResult) RiskAssessment {
	return approvals.AssessmentFromClassifier(res)
}

// assessmentFromPersonaCascade builds an assessment from the
// persona/risk-profile cascade's RiskLevel verdict for a command.
func assessmentFromPersonaCascade(level configuration.RiskLevel, reason string) RiskAssessment {
	return approvals.AssessmentFromPersonaCascade(level, reason)
}

// resolveOldDecision derives a one-word gating decision from the old
// dual-gate path's SecurityResult for shadow-mode comparison.
func resolveOldDecision(res tools.SecurityResult) string {
	return approvals.ResolveOldDecision(res)
}

// resolveUnifiedDecision derives a one-word gating decision from a
// RiskAssessment for shadow-mode comparison with the old path.
func resolveUnifiedDecision(ra RiskAssessment) string {
	return approvals.ResolveUnifiedDecision(ra)
}

// isGitRebaseCommand reports whether `command` contains a `git rebase`
// invocation that rewrites history (i.e. NOT `git rebase --abort`).
func isGitRebaseCommand(command string) bool {
	return approvals.IsGitRebaseCommand(command)
}

// isGitWriteCommand reports whether `command` contains a git invocation
// whose intent requires the orchestrator git-write flow.
func isGitWriteCommand(command string) bool {
	return approvals.IsGitWriteCommand(command)
}

// isGitStashCommand reports whether `command` contains a non-read-only
// `git stash` invocation.
func isGitStashCommand(command string) bool {
	return approvals.IsGitStashCommand(command)
}
