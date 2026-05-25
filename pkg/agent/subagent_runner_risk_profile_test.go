package agent

import (
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestSubagentRunner_InheritsRiskProfileOverrideFromParent locks in the
// SP-058 contract: when a user starts a session with --risk-profile=readonly
// (or any non-default profile), subagents spawned during that session must
// run under the same profile. Without this propagation, the subagent would
// fall back to the config-level setting and the user's intent would silently
// be ignored partway through a delegation chain.
func TestSubagentRunner_InheritsRiskProfileOverrideFromParent(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	parent.SetRiskProfileOverride(configuration.RiskProfileReadonly)

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)

	sub, err := runner.createSubagent(SubagentOptions{})
	if err != nil {
		t.Fatalf("createSubagent failed: %v", err)
	}
	defer sub.Shutdown()

	if got := sub.GetActiveRiskProfile(); got != configuration.RiskProfileReadonly {
		t.Errorf("subagent did not inherit parent's risk profile override: got %q, want %q", got, configuration.RiskProfileReadonly)
	}
}

// TestSubagentRunner_NoRiskProfileOverrideWhenParentHasNone confirms the
// inheritance is purely a passthrough — a parent with no override gives a
// subagent with no override, so resolution falls through to config/default.
func TestSubagentRunner_NoRiskProfileOverrideWhenParentHasNone(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	if parent.riskProfileOverride != "" {
		t.Fatalf("test precondition violated: parent unexpectedly has an override %q", parent.riskProfileOverride)
	}

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)

	sub, err := runner.createSubagent(SubagentOptions{})
	if err != nil {
		t.Fatalf("createSubagent failed: %v", err)
	}
	defer sub.Shutdown()

	if sub.riskProfileOverride != "" {
		t.Errorf("subagent unexpectedly has an override %q when parent had none", sub.riskProfileOverride)
	}
}
