// Package agent: ApprovalAgent seam accessors (SP-141 phase 3, increment 5).
// These exported methods expose the private surface the approval broker
// needs so it can live in pkg/agent/approvals behind the ApprovalAgent
// interface (the changes.AgentView / workflow.LoopAgent pattern). Each
// wrapper delegates to the existing private implementation unchanged; the
// broker (approvals.RequestApproval) is the only consumer.
package agent

import (
	"context"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/security"
)

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
