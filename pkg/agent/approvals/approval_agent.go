// Package approvals: the ApprovalAgent seam (SP-141 phase 3, increment 5).
// ApprovalAgent is the narrow interface the approval broker operates on —
// the same pattern as changes.AgentView and workflow.LoopAgent. *Agent
// satisfies it directly: most members are existing exported accessors; the
// seam accessors in pkg/agent/approval_seam_accessors.go expose the private
// surface (debug logger, interrupt ctx, security-analysis cache, effective
// cwd, the shell-approval picker). The import arrow is one-way:
// pkg/agent -> approvals.
package approvals

import (
	"context"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// ApprovalAgent is the surface the approval broker (RequestApproval)
// needs from the agent.
type ApprovalAgent interface {
	// Config + approval-policy state.
	GetConfig() *configuration.Config
	IsShellCommandAllowlisted(cmd string) bool
	GetUnsafeMode() bool
	GetUnsafeShellMode() bool
	IsSessionElevated() bool
	IsSubagent() bool
	// IsWorkflowRun reports a workflow/automate run, which is non-interactive
	// for approval purposes even when launched from a terminal.
	IsWorkflowRun() bool

	// Interactive-approval surfaces.
	HasActiveWebUIClients() bool
	GetSecurityApprovalMgr() *security.ApprovalManager
	GetEventBus() *events.EventBus
	GetEventClientID() string
	GetEventUserID() string
	MarkWorkflowApprovedInSession(workflow string)

	// Seam accessors (see pkg/agent/approval_seam_accessors.go).
	Client() api.ClientInterface
	GetModel() string
	InterruptCtx() context.Context
	EffectiveCwd() string
	DebugEnabled() bool
	DebugLogf(format string, args ...interface{})
	IsNonInteractive() bool
	GetSecurityAnalysisCache() *SecurityAnalysisCache
	LogSecurityDecision(tool string, args map[string]interface{}, assessment RiskAssessment, action string)
	ApplyApprovalDecision(decision security.ApprovalDecision, command string)
	ApproveShellCommandParts(ctx context.Context, command string) (map[string]bool, []string, error)
}
