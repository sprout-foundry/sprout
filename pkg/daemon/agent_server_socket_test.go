package daemon

import (
	"context"
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentServerLeavesLiveSocketAlone(t *testing.T) {
	sockPath := shortSocketPath(t, "agent-live")
	first := &AgentServer{SocketPath: sockPath, Service: newStubAgentService()}
	require.NoError(t, first.Start(context.Background()))
	t.Cleanup(func() { first.Close() })

	second := &AgentServer{SocketPath: sockPath, Service: newStubAgentService()}
	require.Error(t, second.Start(context.Background()))

	conn, err := net.Dial("unix", sockPath)
	require.NoError(t, err, "the first daemon must still be reachable")
	_ = conn.Close()
}

func TestAgentServerReplacesStaleSocket(t *testing.T) {
	sockPath := shortSocketPath(t, "agent-stale")
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	// Closing a unix listener unlinks the file; recreate a dead one.
	require.NoError(t, ln.Close())
	require.NoError(t, os.WriteFile(sockPath, nil, 0o600))

	srv := &AgentServer{SocketPath: sockPath, Service: newStubAgentService()}
	require.NoError(t, srv.Start(context.Background()))
	t.Cleanup(func() { srv.Close() })
}
