//go:build !js

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAgentForCmd is a minimal AgentService for cmd-level socket tests.
type stubAgentForCmd struct {
	queries     int64
	lastWorkDir atomic.Value // string
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

func (s *stubAgentForCmd) Query(_ context.Context, prompt, workDir string) (string, error) {
	atomic.AddInt64(&s.queries, 1)
	s.lastWorkDir.Store(workDir)
	return "stub answer: " + prompt, nil
}

func (s *stubAgentForCmd) StreamQuery(_ context.Context, _, workDir string, _ func(daemon.StreamEvent) error) error {
	s.lastWorkDir.Store(workDir)
	return nil
}

func (s *stubAgentForCmd) ExecuteTool(context.Context, string, map[string]any) (*daemon.ToolResult, error) {
	return &daemon.ToolResult{Content: "ok"}, nil
}

// shortSocketPath returns a Unix socket path short enough for macOS, where
// sun_path is limited to 104 bytes. t.TempDir() paths on CI runners can
// exceed that (e.g. /var/folders/df/<random>/T/TestName.../001/agent.sock),
// which makes listen fail with "bind: invalid argument".
func shortSocketPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(os.TempDir(), fmt.Sprintf("sprout-%s-%d.sock", name, os.Getpid()))
}

// startCmdAgentServer starts an agent socket server at a short temp path and
// returns the path + cleanup.
func startCmdAgentServer(t *testing.T, svc daemon.AgentService) string {
	t.Helper()
	sockPath := shortSocketPath(t, "agent")
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
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", shortSocketPath(t, "missing"))
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	handled, err := tryDaemonOneShot(context.Background(), "hello", false)
	require.NoError(t, err)
	assert.False(t, handled, "without a daemon socket the CLI must fall back to in-process")
}

// TestNewEphemeralDaemonAgent_IsolatedPerCall is the regression test for the
// conversation-bleed bug: the daemon used to reuse a single shared *Agent
// across every one-shot query routed to it (SwitchSession only relabels an
// ID, it never resets a.state.messages), so an unrelated later query would
// see an earlier caller's full conversation history. Each daemon-routed
// query must now get its own fresh Agent.
func TestNewEphemeralDaemonAgent_IsolatedPerCall(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	a, err := newEphemeralDaemonAgent(dirA)
	require.NoError(t, err)
	b, err := newEphemeralDaemonAgent(dirB)
	require.NoError(t, err)

	assert.NotSame(t, a, b, "each call must get its own Agent, not a shared one")
	assert.Equal(t, dirA, a.GetWorkspaceRoot())
	assert.Equal(t, dirB, b.GetWorkspaceRoot())

	// Simulate a's query leaving conversation history behind, the way a real
	// ProcessQuery call would. b must not see it.
	a.SetSessionName("leftover-from-a")
	assert.NotContains(t, fmt.Sprint(b.GetSystemPrompt()), "leftover-from-a")
}

// TestNewEphemeralDaemonAgent_RequiresWorkDir verifies the daemon refuses to
// guess a project directory rather than silently defaulting to its own —
// the direct fix for the wrong-project bug.
func TestNewEphemeralDaemonAgent_RequiresWorkDir(t *testing.T) {
	_, err := newEphemeralDaemonAgent("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "work_dir")
}

// TestSharedAgentService_Query_EndToEnd wires the real SharedAgentService
// (not the stub) behind a real socket and runs a real query through it —
// closing the loop on the fix: wire protocol → dispatch → per-call ephemeral
// agent → ProcessQuery, all together, not just each piece in isolation.
func TestSharedAgentService_Query_EndToEnd(t *testing.T) {
	svc := NewSharedAgentService(nil) // Query/StreamQuery don't use the wrapped agent
	sockPath := startCmdAgentServer(t, svc)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	dir := t.TempDir()
	origWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer os.Chdir(origWd)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	handled, err := tryDaemonOneShot(ctx, "say hello", false)
	require.NoError(t, err)
	assert.True(t, handled, "query must be served by the real SharedAgentService")
}

// TestTryDaemonOneShot_SendsCallerWorkDir is the regression test for the
// wrong-project-directory bug: the daemon is a single long-lived process
// that may serve callers from many different project directories, and the
// wire protocol used to have no way to tell it which one a query was for —
// it would silently run tools against whatever directory the daemon itself
// happened to start in. tryDaemonOneShot must transmit the caller's actual
// cwd so the daemon-side agent scopes tool execution correctly.
func TestTryDaemonOneShot_SendsCallerWorkDir(t *testing.T) {
	stub := &stubAgentForCmd{}
	sockPath := startCmdAgentServer(t, stub)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	wantDir, err := os.Getwd()
	require.NoError(t, err)

	handled, err := tryDaemonOneShot(context.Background(), "hello", false)
	require.NoError(t, err)
	require.True(t, handled)

	gotDir, _ := stub.lastWorkDir.Load().(string)
	assert.Equal(t, wantDir, gotDir, "the daemon must receive the caller's actual working directory")
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
