package agent

import (
	"testing"
)

func TestNewTestAgent_SubManagersInitialised(t *testing.T) {
	t.Parallel()

	a := NewTestAgent()

	if a.state == nil {
		t.Error("state sub-manager should be initialised")
	}
	if a.output == nil {
		t.Error("output sub-manager should be initialised")
	}
	if a.security == nil {
		t.Error("security sub-manager should be initialised")
	}
	if a.mcpSub == nil {
		t.Error("mcpSub sub-manager should be initialised")
	}
	if a.shellCommandHistory == nil {
		t.Error("shellCommandHistory should be initialised")
	}
}

func TestNewTestAgent_NoProductionDependencies(t *testing.T) {
	t.Parallel()

	a := NewTestAgent()

	// Production-only fields should be nil/zero — the test agent
	// deliberately avoids config, API clients, and prompts.
	if a.client != nil {
		t.Error("test agent should not have an API client")
	}
	if a.configManager != nil {
		t.Error("test agent should not have a config manager")
	}
	if a.systemPrompt != "" {
		t.Error("test agent should not have a system prompt")
	}
}

func TestNewTestAgent_MutableAfterCreation(t *testing.T) {
	t.Parallel()

	a := NewTestAgent()

	// Tests commonly set debug or swap in a custom state manager.
	a.debug = true
	if !a.debug {
		t.Error("expected debug to be settable after construction")
	}

	customState := NewAgentStateManager(true)
	a.state = customState
	if a.state != customState {
		t.Error("expected state to be swappable after construction")
	}
}

func TestNewTestAgent_InitSubManagersIdempotent(t *testing.T) {
	t.Parallel()

	// Calling initSubManagers on a NewTestAgent should be safe
	// (the guards are nil-check based).
	a := NewTestAgent()
	originalState := a.state

	a.initSubManagers()

	if a.state != originalState {
		t.Error("initSubManagers should not replace already-set sub-managers")
	}
}
