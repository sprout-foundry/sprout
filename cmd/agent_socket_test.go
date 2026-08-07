//go:build !js

package cmd

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAgentForCmd is a minimal AgentService for cmd-level socket tests.
type stubAgentForCmd struct {
	queries int64
}

func (s *stubAgentForCmd) ListSessions(context.Context) ([]daemon.SessionInfo, error) {
	return []daemon.SessionInfo{{ID: "s1", Name: "default", Active: true}}, nil
}

func (s *stubAgentForCmd) CreateSession(context.Context, string) (*daemon.SessionInfo, error) {
	return &daemon.SessionInfo{ID: "s2", Name: "x", Active: true}, nil
}

func (s *stubAgentForCmd) SwitchSession(context.Context, string) (*daemon.SessionInfo, error) {
	return &daemon.SessionInfo{ID: "s1", Name: "default", Active: true}, nil
}

func (s *stubAgentForCmd) Query(_ context.Context, prompt string) (string, error) {
	atomic.AddInt64(&s.queries, 1)
	return "stub answer: " + prompt, nil
}

func (s *stubAgentForCmd) StreamQuery(context.Context, string, func(daemon.StreamEvent) error) error {
	return nil
}

func (s *stubAgentForCmd) ExecuteTool(context.Context, string, map[string]any) (*daemon.ToolResult, error) {
	return &daemon.ToolResult{Content: "ok"}, nil
}

// startCmdAgentServer starts an agent socket server at a temp path and
// returns the path + cleanup.
func startCmdAgentServer(t *testing.T, svc daemon.AgentService) string {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "agent.sock")
	srv := &daemon.AgentServer{SocketPath: sockPath, Service: svc}
	require.NoError(t, srv.Start(context.Background()))
	t.Cleanup(func() { srv.Close() })
	return sockPath
}

// TestTryDaemonOneShot_RoutesThroughDaemon verifies a one-shot query with a
// live daemon agent socket is served by the daemon (not run in-process).
func TestTryDaemonOneShot_RoutesThroughDaemon(t *testing.T) {
	stub := &stubAgentForCmd{}
	sockPath := startCmdAgentServer(t, stub)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	handled, err := tryDaemonOneShot(context.Background(), "hello", false)
	require.NoError(t, err)
	assert.True(t, handled, "one-shot query must be handled by the daemon")
	assert.Equal(t, int64(1), atomic.LoadInt64(&stub.queries), "daemon served the query")
}

// TestTryDaemonOneShot_FallsBackWhenNoSocket verifies the safety net: no
// daemon socket → the CLI falls back to in-process (handled=false).
func TestTryDaemonOneShot_FallsBackWhenNoSocket(t *testing.T) {
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", filepath.Join(t.TempDir(), "missing.sock"))
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	handled, err := tryDaemonOneShot(context.Background(), "hello", false)
	require.NoError(t, err)
	assert.False(t, handled, "without a daemon socket the CLI must fall back to in-process")
}

// TestTryDaemonOneShot_Disabled verifies SPROUT_DAEMON_AGENT=0 forces
// in-process execution even when a socket exists.
func TestTryDaemonOneShot_Disabled(t *testing.T) {
	stub := &stubAgentForCmd{}
	sockPath := startCmdAgentServer(t, stub)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_DAEMON_AGENT", "0")

	handled, err := tryDaemonOneShot(context.Background(), "hello", false)
	require.NoError(t, err)
	assert.False(t, handled, "SPROUT_DAEMON_AGENT=0 must force in-process execution")
	assert.Zero(t, atomic.LoadInt64(&stub.queries), "daemon must not serve when disabled")
}

// TestTryDaemonOneShot_EmptyQuery verifies empty queries are never routed.
func TestTryDaemonOneShot_EmptyQuery(t *testing.T) {
	stub := &stubAgentForCmd{}
	sockPath := startCmdAgentServer(t, stub)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	handled, err := tryDaemonOneShot(context.Background(), "", false)
	require.NoError(t, err)
	assert.False(t, handled, "empty query must not be routed")
}
