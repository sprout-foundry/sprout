// Package agent: unified risk assessment — the *Agent forwarder
// (SP-141 phase 3, increment 6). The resolver implementation
// (approvals.ResolveToolRisk) and the RiskAgent seam live in
// pkg/agent/approvals (risk_resolver.go, risk_agent.go); this file keeps
// the *Agent method used by the tool-security gates, the seed-time
// security checks, and the shell handler.
//
// Behavior preservation: the pre-move body was nil-*Agent-safe (it
// skipped every agent-coupled input and returned the classifier-only
// assessment). A nil *Agent boxed into the RiskAgent interface is
// non-nil, so that guard stays here.
package agent

import (
	"github.com/sprout-foundry/sprout/pkg/agent/approvals"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// ResolveToolRisk produces the unified risk assessment for a tool call by
// folding all security inputs onto the Low/Medium/High/Critical scale.
func (a *Agent) ResolveToolRisk(toolName string, args map[string]interface{}) RiskAssessment {
	if a == nil {
		return assessmentFromClassifier(tools.ClassifyToolCallWithWorkspace(toolName, args, ""))
	}
	return approvals.ResolveToolRisk(a, toolName, args)
}
