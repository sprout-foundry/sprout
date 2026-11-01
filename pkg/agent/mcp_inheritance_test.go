package agent

import (
	"context"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMCPInheritanceRunner builds a runner whose client factory never needs a
// provider, so the test exercises construction only.
func newMCPInheritanceRunner(t *testing.T, parent *Agent) *SubagentRunner {
	t.Helper()
	runner := NewSubagentRunner(parent, &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	})
	runner.testClientFactory = func(api.ClientType, string) (api.ClientInterface, error) {
		return NewScriptedClient(NewScriptedResponseBuilder().Content("ok").Build()), nil
	}
	return runner
}

// A subagent must see the MCP servers its parent configured. Before the
// manager was shared, every subagent got a fresh one with no servers and
// never ran initializeMCP, so a delegated task could reach none of the
// parent's integrations.
func TestSubagentInheritsParentMCPManager(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	shared := &fakeMCPManager{tools: []mcp.MCPTool{{
		Name:        "create_payment",
		Description: "create a payment",
		ServerName:  "stripe",
		InputSchema: map[string]interface{}{"type": "object"},
	}}}
	parent.mcpSub.SetManager(shared)
	parent.mcpSub.SetInitialized(true)

	runner := newMCPInheritanceRunner(t, parent)
	sub, err := runner.createSubagent(SubagentOptions{Persona: "coder"}, context.Background())
	require.NoError(t, err)

	assert.Same(t, shared, sub.mcpSub.GetManager(), "the subagent must share the parent's manager")
	assert.True(t, sub.mcpSub.IsInherited(), "a shared manager must be marked inherited")
}

// An inherited manager must not re-run the add/start loop: every server is
// already registered by the parent.
func TestInheritedMCPManagerSkipsReinitialization(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	a.mcpSub.SetInherited(true)
	require.NoError(t, a.initializeMCP())
}

// Shutting down a subagent must not stop servers the parent is still using.
func TestInheritedMCPManagerIsNotStoppedOnShutdown(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	stopped := false
	parent.mcpSub.SetManager(&recordingStopManager{onStopAll: func() { stopped = true }})

	runner := newMCPInheritanceRunner(t, parent)
	sub, err := runner.createSubagent(SubagentOptions{Persona: "coder"}, context.Background())
	require.NoError(t, err)

	sub.Shutdown()
	assert.False(t, stopped, "a subagent must not stop the parent's MCP servers")
}

// A subagent must not reconfigure the parent's servers: an mcp_refresh add or
// remove would stop connections the parent is actively using.
func TestInheritedMCPManagerRefusesMutations(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	a.mcpSub.SetInherited(true)
	ctx := context.Background()

	for _, op := range []string{"add", "remove", "refresh", "set-credential", "login", "logout"} {
		_, err := handleMCPRefresh(ctx, a, map[string]any{"operation": op, "name": "stripe"})
		if err == nil {
			t.Fatalf("mcp_refresh %s must be refused on an inherited manager", op)
		}
		if !strings.Contains(err.Error(), "shares the parent's MCP servers") {
			t.Errorf("mcp_refresh %s: unexpected error %v", op, err)
		}
	}

	// Read-only operations still work.
	if _, err := handleMCPRefresh(ctx, a, map[string]any{"operation": "list"}); err != nil {
		t.Fatalf("list must still work on an inherited manager: %v", err)
	}
}

// reconcileMCPServers must not touch the shared manager: the parent owns it.
func TestInheritedMCPManagerSkipsReconcile(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	stopped := false
	a.mcpSub.SetManager(&recordingStopManager{onStopAll: func() { stopped = true }})
	a.mcpSub.SetInherited(true)

	require.NoError(t, a.reconcileMCPServers(context.Background()))
	assert.False(t, stopped, "reconcile must not stop the parent's servers")
}

// recordingStopManager records whether StopAll was called.
type recordingStopManager struct {
	fakeMCPManager
	onStopAll func()
}

func (r *recordingStopManager) StopAll(ctx context.Context) error {
	if r.onStopAll != nil {
		r.onStopAll()
	}
	return nil
}
