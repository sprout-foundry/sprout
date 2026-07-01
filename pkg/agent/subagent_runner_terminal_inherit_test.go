package agent

import (
	"context"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestSubagentRunner_InheritsTerminalManagerFromParent locks in the fix for
// the regression where subagents (including the persona/orchestrator chain)
// failed shell_command background=true / check_background / stop_background
// because no TerminalManager or BackgroundProcessManager was attached, even
// when the root agent had a TerminalManager. The Agent struct construction
// in createSubagent was propagating todoMgr / eventBus / embeddingMgr from
// the parent but missing terminalManager.
func TestSubagentRunner_InheritsTerminalManagerFromParent(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	// Wire a sentinel TerminalManager on the parent so we can verify the
	// subagent inherits the same reference. The mock satisfies the
	// tools.TerminalAccess interface used by shell_command.
	sentinel := &mockTerminalAccess{}
	parent.SetTerminalManager(sentinel)

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)

	// createSubagent is the internal constructor exercised by Run /
	// RunParallel. We call it directly so the test stays focused on the
	// inheritance behavior rather than a full subagent run (which would
	// require provider credentials etc).
	sub, err := runner.createSubagent(SubagentOptions{}, context.Background())
	if err != nil {
		t.Fatalf("createSubagent failed: %v", err)
	}
	defer sub.Shutdown()

	got := sub.GetTerminalManager()
	if got == nil {
		t.Fatal("subagent inherited no TerminalManager — background shell tools will fail with the WebUI-required error")
	}
	if got != sentinel {
		t.Errorf("subagent's TerminalManager is not the parent's reference (got %v, want %v)", got, sentinel)
	}
}

// TestSubagentRunner_NoTerminalManagerWhenParentHasNone confirms the
// inheritance is a no-op when the parent itself has no TerminalManager
// (CLI mode). The subagent simply doesn't get one and shell_command
// background features remain unavailable — but the normal synchronous
// shell_command path still works.
func TestSubagentRunner_NoTerminalManagerWhenParentHasNone(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	// Parent has no TerminalManager set — CLI-mode scenario.
	if parent.GetTerminalManager() != nil {
		t.Fatal("test precondition violated: parent unexpectedly has a TerminalManager")
	}

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)

	sub, err := runner.createSubagent(SubagentOptions{}, context.Background())
	if err != nil {
		t.Fatalf("createSubagent failed: %v", err)
	}
	defer sub.Shutdown()

	if sub.GetTerminalManager() != nil {
		t.Errorf("subagent unexpectedly inherited a TerminalManager when parent has none")
	}
}
