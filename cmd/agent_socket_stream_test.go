//go:build !js

package cmd

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newScriptedStreamDaemonAgent builds a real agent whose LLM client streams
// the given chunks (ScriptedClient with StreamConfig), for exercising the
// daemon StreamQuery path end-to-end without a live provider.
func newScriptedStreamDaemonAgent(t *testing.T, workDir string, chunks []string) (*agent.Agent, error) {
	t.Helper()
	t.Setenv("SPROUT_CONFIG", t.TempDir())

	client := agent.NewScriptedClient(&agent.ScriptedResponse{
		Content:      "final fallback text",
		FinishReason: "stop",
		StreamConfig: &agent.StreamConfig{Chunks: chunks},
	})
	mgr, err := configuration.NewManagerSilent()
	if err != nil {
		return nil, err
	}
	a, err := agent.NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		return nil, err
	}
	a.SetWorkspaceRoot(workDir)
	return a, nil
}

// TestSharedAgentService_StreamQuery_StreamsRealChunks verifies StreamQuery
// delivers per-chunk delta events over the socket as the provider streams
// them — not one synthetic delta after the whole run completes.
func TestSharedAgentService_StreamQuery_StreamsRealChunks(t *testing.T) {
	chunks := []string{"Hello", " ", "from", " ", "the", " ", "daemon"}

	var mu sync.Mutex
	var created []*agent.Agent
	origFn := newEphemeralDaemonAgentFn
	t.Cleanup(func() { newEphemeralDaemonAgentFn = origFn })
	newEphemeralDaemonAgentFn = func(workDir string, _ daemon.QueryOptions) (*agent.Agent, error) {
		a, err := newScriptedStreamDaemonAgent(t, workDir, chunks)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		created = append(created, a)
		mu.Unlock()
		return a, nil
	}

	svc := NewSharedAgentService(nil)
	sockPath := startCmdAgentServer(t, svc)

	client, err := daemon.NewAgentClient(sockPath)
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var deltas []string
	err = client.StreamQuery(ctx, "stream me", t.TempDir(), daemon.QueryOptions{}, func(ev daemon.StreamEvent) error {
		if ev.Type == "delta" {
			deltas = append(deltas, ev.Content)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, deltas, "StreamQuery must deliver streamed chunks")

	joined := joinChunks(deltas)
	assert.Equal(t, "Hello from the daemon", joined,
		"streamed chunks must assemble into the full assistant text in order")

	mu.Lock()
	agents := append([]*agent.Agent(nil), created...)
	mu.Unlock()
	for _, a := range agents {
		require.Eventually(t, func() bool { return a.IsShutdown() },
			10*time.Second, 20*time.Millisecond,
			"ephemeral agent must be shut down after the stream completes")
	}
}

func joinChunks(chunks []string) string {
	s := ""
	for _, c := range chunks {
		s += c
	}
	return s
}
