package agent

import (
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// NewTestAgent creates a minimal Agent suitable for unit tests.
//
// Tests that create bare &Agent{} structs must remember to call
// initSubManagers() to avoid nil-pointer panics.  NewTestAgent()
// eliminates that two-step dance by returning an Agent whose
// sub-managers (state, output, security, mcpSub) and basic fields
// (shellCommandHistory) are already initialised.
//
// The returned agent has NO API client, config manager, or system
// prompt — those are only needed in integration-style tests that
// should use NewAgent() instead.
//
// Callers may freely mutate the returned Agent (e.g. setting debug,
// swapping in a mock state manager) after construction.
func NewTestAgent() *Agent {
	return &Agent{
		state:               NewAgentStateManager(false),
		output:              NewAgentOutputManager(),
		security:            NewAgentSecurityManager(),
		mcpSub:              NewAgentMCPManager(),
		shellCommandHistory: make(map[string]*ShellCommandResult),
	}
}

// NewTestAgentWithConfigManager is NewTestAgent with the configuration
// manager wired in, for tests that exercise config-gated behavior (e.g.
// the SP-149 verification flag) without a full NewAgent.
func NewTestAgentWithConfigManager(mgr *configuration.Manager) *Agent {
	ag := NewTestAgent()
	ag.configManager = mgr
	return ag
}

// PublishTurnProgressComplete exposes the turn-completion progress emit —
// the progress_verification / progress_complete pair built from the stored
// turn-end verification result (SP-151 §151a) — for cross-package tests
// and embedding turn loops. It is the same path handleQueryResult calls on
// the success path; a no-op when no event bus is wired or the turn is a
// subagent turn.
func (a *Agent) PublishTurnProgressComplete() {
	a.publishTurnProgressComplete()
}
