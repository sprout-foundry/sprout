//go:build !js

package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// newSteerTestAgent returns an agent with a command registry attached, so the
// steer helpers can resolve commands the way they do in a live session.
func newSteerTestAgent(t *testing.T) *agent.Agent {
	t.Helper()
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	client := agent.NewMockLLMProviderWithLimit(128_000)
	a, err := agent.NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient failed: %v", err)
	}
	t.Cleanup(func() { a.Shutdown() })
	a.SetSlashCommands(agent_commands.NewCommandRegistry())
	return a
}

// TestExecuteSafeSteerCommand_BangResolvesToExec pins the WebUI steer path's
// bang handling: "!cmd" must resolve to the exec command (the registry's own
// ! → exec translation) rather than being looked up as a command literally
// named "!cmd", which would fail and fall through to plain-text steering.
func TestExecuteSafeSteerCommand_BangResolvesToExec(t *testing.T) {
	ws, _ := newTestWebServer(t)
	chatAgent := newSteerTestAgent(t)

	cmd, _, _ := ws.executeSafeSteerCommandStreaming("!echo hi", chatAgent, nil, nil)
	if cmd == nil {
		t.Fatal("bang command did not resolve to a command")
	}
	if cmd.Name() != "exec" {
		t.Fatalf("bang command resolved to %q, want \"exec\"", cmd.Name())
	}
}

// TestExecuteSafeSteerCommand_BangEmptyPayloadRejected verifies a bare "!" is
// not treated as a runnable command.
func TestExecuteSafeSteerCommand_BangEmptyPayloadRejected(t *testing.T) {
	ws, _ := newTestWebServer(t)
	chatAgent := newSteerTestAgent(t)

	if cmd, _, _ := ws.executeSafeSteerCommandStreaming("!", chatAgent, nil, nil); cmd != nil {
		t.Fatalf("bare \"!\" resolved to %q, want nil", cmd.Name())
	}
	if cmd, _, _ := ws.executeSafeSteerCommandStreaming("!   ", chatAgent, nil, nil); cmd != nil {
		t.Fatalf("whitespace-only bang resolved to %q, want nil", cmd.Name())
	}
}

// TestExecuteSafeSteerCommand_UnsafeSlashStillRejected guards the other half:
// widening the branch to include "!" must not let unsafe slash commands
// through mid-turn.
func TestExecuteSafeSteerCommand_UnsafeSlashStillRejected(t *testing.T) {
	ws, _ := newTestWebServer(t)
	chatAgent := newSteerTestAgent(t)

	if cmd, _, _ := ws.executeSafeSteerCommandStreaming("/commit", chatAgent, nil, nil); cmd != nil {
		t.Fatalf("/commit resolved to %q, want nil (unsafe during steer)", cmd.Name())
	}
}

// TestExecCommandIsSteerSafe guards the classification the WebUI steer path
// depends on.
func TestExecCommandIsSteerSafe(t *testing.T) {
	cmd := &agent_commands.ExecCommand{}
	if !agent_commands.IsSafeDuringSteer(cmd, []string{"echo hi"}) {
		t.Fatal("exec should be safe during steer")
	}
}
