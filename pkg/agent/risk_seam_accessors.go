// Package agent: RiskAgent seam accessors (SP-141 phase 3, increment 6).
// These exported methods expose the private surface the risk resolver
// needs so it can live in pkg/agent/approvals behind the RiskAgent
// interface (the ApprovalAgent / changes.AgentView / workflow.LoopAgent
// pattern). Each wrapper delegates to the existing implementation
// unchanged; the resolver (approvals.ResolveToolRisk) is the only
// consumer. EffectiveCwd, DebugEnabled, and DebugLogf come from
// approval_seam_accessors.go (same package).
package agent

import (
	"github.com/sprout-foundry/sprout/pkg/agent/approvals"
)

// Compile-time assertion that *Agent satisfies the risk seam: the
// forwarder in risk_assessment.go passes *Agent as approvals.RiskAgent,
// and this guard keeps the interface and the method surface from
// drifting apart silently.
var _ approvals.RiskAgent = (*Agent)(nil)

// IsGitWriteAllowed reports whether the active persona carries the
// git-write capability (the persona gate the risk resolver folds into
// the assessment).
func (a *Agent) IsGitWriteAllowed() bool {
	return a.isGitWriteAllowed()
}

// HomeDir returns the user's home directory through the detectHomeDir
// test-override hook, or "" if unresolved. Routing through the hook
// (rather than os.UserHomeDir directly) keeps the pre-move test
// overrides working.
func (a *Agent) HomeDir() string {
	return detectHomeDir()
}
