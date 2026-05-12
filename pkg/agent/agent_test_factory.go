package agent

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
