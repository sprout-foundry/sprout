// Package agent: approval broker forwarders (SP-141 phase 3, increment 5).
// The broker implementation (RequestApproval) and EvaluateCommandPolicy
// live in pkg/agent/approvals; these keep the *Agent method surface used
// by the tool-security gates, the seed-time security checks, and the
// destructive-approval prompter.
package agent

import (
	"github.com/sprout-foundry/sprout/pkg/agent/approvals"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// BrokerDecision is the typed verdict returned by RequestApproval.
type BrokerDecision = approvals.BrokerDecision

// RequestApproval performs the unified approval flow for a RiskAssessment.
// Low-risk auto-approves. Critical/hard-blocks deny unconditionally.
// Medium/High/IntentConfirmation checks bypass paths then tries WebUI, CLI,
// or falls back to permissive auto-approve in non-interactive mode.
func (a *Agent) RequestApproval(assessment RiskAssessment, toolName string, args map[string]interface{}) (BrokerDecision, error) {
	return approvals.RequestApproval(a, assessment, toolName, args)
}

// EvaluateCommandPolicy checks user-defined command policies against a
// shell command (implementation in pkg/agent/approvals).
func EvaluateCommandPolicy(command string, policies *configuration.CommandPolicies) (configuration.CommandPolicyAction, string, bool) {
	return approvals.EvaluateCommandPolicy(command, policies)
}
