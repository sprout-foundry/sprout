//go:build !js

package webui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// Switching a client's workspace clears ctx.Agent and ctx.ChatSessions. Before
// this teardown existed, the outgoing agents were only dropped from the map —
// their own goroutines kept them alive, so their embedding managers went on
// building and writing the OLD workspace's index for the life of the daemon.
// With a single browser window, opening workspace A then B was enough to leave
// two workspaces' index builds racing each other.
func TestSetClientWorkspaceRootShutsDownOutgoingAgent(t *testing.T) {
	daemonRoot := t.TempDir()
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	t.Cleanup(func() { credentials.ResetStorageBackend() })
	credentials.ResetStorageBackend()

	first := filepath.Join(daemonRoot, "first")
	second := filepath.Join(daemonRoot, "second")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot
	ws.SetWorkspaceRoot(daemonRoot)

	const clientID = "teardown-window"
	if _, err := ws.setClientWorkspaceRoot(clientID, first); err != nil {
		t.Fatalf("set first workspace: %v", err)
	}

	outgoing, err := ws.getClientAgent(clientID)
	if err != nil {
		t.Skipf("agent creation unavailable in this environment: %v", err)
	}
	if outgoing == nil {
		t.Fatal("expected an agent for the first workspace")
	}

	if _, err := ws.setClientWorkspaceRoot(clientID, second); err != nil {
		t.Fatalf("set second workspace: %v", err)
	}
	ws.waitForAgentTeardown()

	if !outgoing.IsShutdown() {
		t.Error("outgoing agent was dropped without being shut down")
	}

	ws.mutex.RLock()
	clientCtx := ws.clientContexts[clientID]
	ws.mutex.RUnlock()
	if clientCtx == nil {
		t.Fatal("client context disappeared after workspace switch")
	}
	if clientCtx.Agent == outgoing {
		t.Error("client context still references the outgoing agent")
	}
}

// releaseAgents must never shut down the shared-mode CLI agent: the server
// borrows it, the CLI owns it, and tearing it down would kill the user's
// interactive session.
func TestReleaseAgentsSkipsSharedAgent(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// nil entries and a nil shared agent must both be tolerated.
	ws.releaseAgents("test", nil, nil)
	ws.waitForAgentTeardown()
}
