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

// TestSubagentRunner_InheritsSessionFolderAllowlist verifies that the
// folders the user approved at the root level remain auto-approved for
// any subagent spawned afterwards. Without this propagation, a subagent
// would re-prompt the user for paths already cleared at the root, and
// in non-interactive subagent contexts that re-prompt becomes a hard
// rejection (subagents can't ask).
func TestSubagentRunner_InheritsSessionFolderAllowlist(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	parent.AddSessionAllowedFolder("/tmp/parent-approved-folder")
	parent.AddSessionAllowedFolder("/srv/shared-data")

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

	if !sub.IsFolderSessionAllowed("/tmp/parent-approved-folder/x.txt") {
		t.Error("subagent did not inherit /tmp/parent-approved-folder from parent")
	}
	if !sub.IsFolderSessionAllowed("/srv/shared-data/db.sqlite") {
		t.Error("subagent did not inherit /srv/shared-data from parent")
	}
	if sub.IsFolderSessionAllowed("/etc/passwd") {
		t.Error("subagent should not allow paths not in parent's allowlist")
	}

	// Verify isolation: subagent's own additions don't leak back to
	// parent. This is intentional — temporary approvals inside a
	// delegated task shouldn't outlive the delegation.
	sub.AddSessionAllowedFolder("/sub-only")
	if parent.IsFolderSessionAllowed("/sub-only/leak.txt") {
		t.Error("subagent's allowlist additions must not leak into the parent")
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
