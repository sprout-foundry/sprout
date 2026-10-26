// Package agent: ApprovalAgent seam accessors (SP-141 phase 3, increment 5).
// These exported methods expose the private surface the approval broker
// needs so it can live in pkg/agent/approvals behind the ApprovalAgent
// interface (the changes.AgentView / workflow.LoopAgent pattern). Each
// wrapper delegates to the existing private implementation unchanged; the
// broker (approvals.RequestApproval) is the only consumer.
package agent

import (
	"context"

	"github.com/sprout-foundry/sprout/pkg/agent/approvals"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// Compile-time assertion that *Agent satisfies the broker seam: the
// forwarder in approval_broker.go passes *Agent as approvals.ApprovalAgent,
// and this guard keeps the interface and the method surface from drifting
// apart silently.
var _ approvals.ApprovalAgent = (*Agent)(nil)

// Client returns the agent's provider client under the client lock.
func (a *Agent) Client() api.ClientInterface {
	return a.getClient()
}

// EffectiveCwd returns the shell working directory the broker uses for
// LLM security analysis.
func (a *Agent) EffectiveCwd() string {
	return a.effectiveCwd()
}

// DebugEnabled reports whether verbose debug logging is on.
func (a *Agent) DebugEnabled() bool {
	return a.debug
}

// DebugLogf writes a structured debug-log line (a no-op when debug is off).
func (a *Agent) DebugLogf(format string, args ...interface{}) {
	a.debugLog(format, args...)
}

// IsNonInteractive reports whether the agent runs without an interactive
// approval surface.
func (a *Agent) IsNonInteractive() bool {
	return a.isNonInteractive()
}

// IsWorkflowRun reports whether this agent is executing a workflow/automate
// run, which is non-interactive for approval purposes even when launched from
// a terminal.
func (a *Agent) IsWorkflowRun() bool {
	if a == nil {
		return false
	}
	return a.workflowRun.Load()
}

// ActiveSubagentCount reports the process-wide number of subagents currently
// running (SP-168). The approval broker offers the CLI prompt surface to a
// subagent only when this is <= 1, so parallel subagents never interleave
// prompts on one terminal. Delegates to the package-level GetActiveSubagents
// forwarder (subagent_forwarders.go).
func (a *Agent) ActiveSubagentCount() int {
	return GetActiveSubagents()
}

// Gate1DecidedLastCall reports whether the most recent unified Gate-1
// security decision approved the call (SP-168). Handler-level gates read it
// to skip their own approval prompt when Gate 1 already decided the call.
// Safe on a nil agent (returns false).
func (a *Agent) Gate1DecidedLastCall() bool {
	if a == nil {
		return false
	}
	return a.gate1Decided.Load()
}

// markGate1Decided records that the unified Gate-1 decision approved the
// current tool call (used by unifiedSecurityGate before returning nil).
func (a *Agent) markGate1Decided() {
	a.gate1Decided.Store(true)
}

// resetGate1Decided clears the per-call Gate-1 decision flag at the start of
// a unifiedSecurityGate run so a later call never sees a stale approval.
func (a *Agent) resetGate1Decided() {
	a.gate1Decided.Store(false)
}

// GetSecurityAnalysisCache returns the lazily-created LLM security-analysis
// cache (the nil-agent guard is preserved by the underlying getter).
func (a *Agent) GetSecurityAnalysisCache() *SecurityAnalysisCache {
	return a.getSecurityAnalysisCache()
}

// LogSecurityDecision records an approval outcome in the audit logger.
func (a *Agent) LogSecurityDecision(tool string, args map[string]interface{}, assessment RiskAssessment, action string) {
	a.logSecurityDecision(tool, args, assessment, action)
}

// ApplyApprovalDecision performs the side-effects of the user's approval
// choice (ApproveAlways persists the command; Elevate bumps the session
// risk profile).
func (a *Agent) ApplyApprovalDecision(decision security.ApprovalDecision, command string) {
	a.applyApprovalDecision(decision, command)
}

// ApproveShellCommandParts splits `command` into a classified ShellProposal
// and runs the per-part approval picker, returning the per-part decisions
// and the proposal's part IDs (in order). Callers verify that every part
// was approved; any missing or denied part denies the whole command.
func (a *Agent) ApproveShellCommandParts(ctx context.Context, command string) (map[string]bool, []string, error) {
	if a == nil {
		return nil, nil, agenterrors.NewValidation("shell approval picker: nil agent", nil)
	}
	proposal := NewShellProposal(command)
	decisions, err := a.RequestShellApproval(ctx, proposal)
	if err != nil {
		return nil, nil, err
	}
	partIDs := make([]string, 0, len(proposal.Parts))
	for _, p := range proposal.Parts {
		partIDs = append(partIDs, p.ID)
	}
	return decisions, partIDs, nil
}
