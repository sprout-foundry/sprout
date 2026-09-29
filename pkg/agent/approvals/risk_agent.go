// Package approvals: the RiskAgent seam (SP-141 phase 3, increment 6).
// RiskAgent is the narrow interface the risk resolver (ResolveToolRisk)
// operates on — the same pattern as ApprovalAgent, changes.AgentView, and
// workflow.LoopAgent. *Agent satisfies it directly: most members are
// pre-existing exported methods; the seam accessors in
// pkg/agent/risk_seam_accessors.go expose the private surface (the
// persona git-write capability check and the test-overridable home-dir
// hook). The import arrow is one-way: pkg/agent -> approvals.
package approvals

import (
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// RiskAgent is the surface the risk resolver (ResolveToolRisk) needs
// from the agent.
type RiskAgent interface {
	// Workspace + session state.
	GetWorkspaceRoot() string
	HasPasswordPrompter() bool
	IsFolderSessionAllowed(absPath string) bool
	GetConfig() *configuration.Config

	// Persona cascade.
	EvaluateOperationRisk(command string) configuration.RiskLevel

	// Seam accessors (see pkg/agent/risk_seam_accessors.go +
	// approval_seam_accessors.go).
	IsGitWriteAllowed() bool
	EffectiveCwd() string
	HomeDir() string
	DebugEnabled() bool
	DebugLogf(format string, args ...interface{})
}
