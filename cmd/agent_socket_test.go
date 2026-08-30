//go:build !js

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAgentForCmd is a minimal AgentService for cmd-level socket tests.
type stubAgentForCmd struct {
	queries     int64
	lastWorkDir atomic.Value // string
	lastOpts    atomic.Value // daemon.QueryOptions
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

func (s *stubAgentForCmd) Query(_ context.Context, prompt, workDir string, opts daemon.QueryOptions) (string, error) {
	atomic.AddInt64(&s.queries, 1)
	s.lastWorkDir.Store(workDir)
	s.lastOpts.Store(opts)
	return "stub answer: " + prompt, nil
}

func (s *stubAgentForCmd) StreamQuery(_ context.Context, _, workDir string, _ daemon.QueryOptions, _ func(daemon.StreamEvent) error) error {
	s.lastWorkDir.Store(workDir)
	return nil
}

func (s *stubAgentForCmd) ExecuteTool(_ context.Context, name string, args map[string]any, workDir string) (*daemon.ToolResult, error) {
	s.lastWorkDir.Store(workDir)
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

	a, err := newEphemeralDaemonAgent(dirA, daemon.QueryOptions{})
	require.NoError(t, err)
	b, err := newEphemeralDaemonAgent(dirB, daemon.QueryOptions{})
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
	_, err := newEphemeralDaemonAgent("", daemon.QueryOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "work_dir")
}

// TestSharedAgentService_Query_EndToEnd wires the real SharedAgentService
// (not the stub) behind a real socket and runs a real query through it —
// closing the loop on the fix: wire protocol → dispatch → per-call ephemeral
// agent → ProcessQuery, all together, not just each piece in isolation.
func TestSharedAgentService_Query_EndToEnd(t *testing.T) {
	// Wrap (not replace) the real constructor so the real layered-config
	// path runs, while capturing the ephemeral agent to await its async
	// shutdown: Shutdown flushes session files into the workspace temp dir,
	// and t.TempDir's RemoveAll races those writes if the test returns first.
	var mu sync.Mutex
	var created []*agent.Agent
	origFn := newEphemeralDaemonAgentFn
	t.Cleanup(func() { newEphemeralDaemonAgentFn = origFn })
	newEphemeralDaemonAgentFn = func(workDir string, opts daemon.QueryOptions) (*agent.Agent, error) {
		a, err := origFn(workDir, opts)
		if a != nil {
			mu.Lock()
			created = append(created, a)
			mu.Unlock()
		}
		return a, err
	}

	svc := NewSharedAgentService(nil)
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

	mu.Lock()
	agents := append([]*agent.Agent(nil), created...)
	mu.Unlock()
	require.NotEmpty(t, agents, "the query must have created an ephemeral agent")
	for _, a := range agents {
		require.Eventually(t, func() bool { return a.IsShutdown() },
			10*time.Second, 20*time.Millisecond,
			"ephemeral agent must finish shutting down before the workspace temp dir is removed")
	}
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

// TestTryDaemonOneShot_JSONOutputNeverRoutes is the pin for SP-136 P4
// option (a): --output-json one-shots must never route through the daemon
// socket, even when one is healthy — the protocol can't carry the envelope's
// metrics or response text, so they run in-process instead.
func TestTryDaemonOneShot_JSONOutputNeverRoutes(t *testing.T) {
	stub := &stubAgentForCmd{}
	sockPath := startCmdAgentServer(t, stub)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	handled, err := tryDaemonOneShot(context.Background(), "hello", true)
	require.NoError(t, err)
	assert.False(t, handled, "jsonOut runs must never route to the daemon, even when reachable")
	assert.Zero(t, atomic.LoadInt64(&stub.queries), "daemon must not serve jsonOut queries")
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

// newTestAgent builds a minimal agent for cmd tests backed by an isolated
// temp config directory (mirrors pkg/agent's newTestAgent — never the real
// user config). The test client is auto-selected under go test.
func newTestAgent(t *testing.T) *agent.Agent {
	t.Helper()
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	a, err := agent.NewAgentWithModel("test:test")
	if err != nil {
		t.Fatalf("newTestAgent: %v", err)
	}
	return a
}

// TestSharedAgentService_ReleasesEphemeralAgents is the regression test for
// the daemon one-shot resource leak: Query/StreamQuery build a fresh agent
// per call and used to drop the reference without Shutdown(), leaking a
// shared embedding-manager refcount (the workspace's HNSW store could never
// close again) plus MCP child processes and lifetime contexts. Every
// ephemeral agent must now be shut down once the query returns.
func TestSharedAgentService_ReleasesEphemeralAgents(t *testing.T) {
	origFn := newEphemeralDaemonAgentFn
	t.Cleanup(func() { newEphemeralDaemonAgentFn = origFn })

	var mu sync.Mutex
	var created []*agent.Agent
	newEphemeralDaemonAgentFn = func(workDir string, _ daemon.QueryOptions) (*agent.Agent, error) {
		a := newTestAgent(t)
		a.SetWorkspaceRoot(workDir)
		mu.Lock()
		created = append(created, a)
		mu.Unlock()
		return a, nil
	}

	svc := NewSharedAgentService(nil) // Query/StreamQuery don't use the wrapped agent
	_, err := svc.Query(context.Background(), "hi", t.TempDir(), daemon.QueryOptions{})
	require.NoError(t, err)

	mu.Lock()
	require.NotEmpty(t, created, "the query must have created an ephemeral agent")
	agents := append([]*agent.Agent(nil), created...)
	mu.Unlock()

	for _, a := range agents {
		require.Eventually(t, func() bool { return a.IsShutdown() },
			10*time.Second, 20*time.Millisecond,
			"ephemeral agent must be shut down after the query returns")
	}
}

// TestSharedAgentService_WaitForTeardown_BlocksUntilShutdown pins the
// teardown ordering contract: once a query returns, WaitForTeardown must
// not return until every agent released by that query has fully shut down
// (it may not return early, before Shutdown finishes). Teardown itself is
// covered by TestSharedAgentService_ReleasesEphemeralAgents; this test
// asserts the wait observes completed shutdowns.
func TestSharedAgentService_WaitForTeardown_BlocksUntilShutdown(t *testing.T) {
	origFn := newEphemeralDaemonAgentFn
	t.Cleanup(func() { newEphemeralDaemonAgentFn = origFn })

	var mu sync.Mutex
	var created []*agent.Agent
	newEphemeralDaemonAgentFn = func(workDir string, _ daemon.QueryOptions) (*agent.Agent, error) {
		a := newTestAgent(t)
		a.SetWorkspaceRoot(workDir)
		mu.Lock()
		created = append(created, a)
		mu.Unlock()
		return a, nil
	}

	svc := NewSharedAgentService(nil) // Query/StreamQuery don't use the wrapped agent
	_, err := svc.Query(context.Background(), "hi", t.TempDir(), daemon.QueryOptions{})
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		svc.WaitForTeardown()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("WaitForTeardown must return once released agents finish shutting down")
	}

	mu.Lock()
	agents := append([]*agent.Agent(nil), created...)
	mu.Unlock()
	require.NotEmpty(t, agents, "the query must have created an ephemeral agent")
	for _, a := range agents {
		assert.True(t, a.IsShutdown(),
			"by the time WaitForTeardown returns every released agent must be shut down")
	}
}

// TestSharedAgentService_WaitForTeardown_ReturnsWhenNothingReleased verifies
// WaitForTeardown does not hang when no ephemeral agents were ever released.
func TestSharedAgentService_WaitForTeardown_ReturnsWhenNothingReleased(t *testing.T) {
	svc := NewSharedAgentService(nil)
	done := make(chan struct{})
	go func() {
		svc.WaitForTeardown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForTeardown must return promptly when nothing was released")
	}
}

// TestSharedAgentService_QueryRejectedAfterTeardownBegins verifies teardown
// is a one-way door: once WaitForTeardown has begun, a late query must be
// rejected before the agent constructor is even called — otherwise it would
// Add to a WaitGroup whose Wait is already running (the narrow crash path
// this restructure eliminates).
func TestSharedAgentService_QueryRejectedAfterTeardownBegins(t *testing.T) {
	origFn := newEphemeralDaemonAgentFn
	t.Cleanup(func() { newEphemeralDaemonAgentFn = origFn })

	var seamCalls atomic.Int64
	newEphemeralDaemonAgentFn = func(workDir string, _ daemon.QueryOptions) (*agent.Agent, error) {
		seamCalls.Add(1)
		return nil, nil
	}

	svc := &SharedAgentService{} // zero value is fine — Query only needs the tracker
	svc.WaitForTeardown()        // nothing tracked → returns fast

	_, err := svc.Query(context.Background(), "x", "/tmp", daemon.QueryOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shutting down")
	assert.Zero(t, seamCalls.Load(), "the query must be rejected before the agent constructor is invoked")
}

// TestAgentServer_OnClose_Invoked pins AgentServer.Close's blocking teardown
// contract: Close must not return while OnClose is still running (it blocks
// until service teardown finishes), and a second Close must never re-invoke
// OnClose (once-only). This is the hook startDaemonAgentServer uses to wait
// for ephemeral agent teardown before the daemon exits.
func TestAgentServer_OnClose_Invoked(t *testing.T) {
	svc := &stubAgentForCmd{}
	sockPath := shortSocketPath(t, "agent-onclose")

	var calls atomic.Int64
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := &daemon.AgentServer{
		SocketPath: sockPath,
		Service:    svc,
		OnClose: func() {
			calls.Add(1)
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
		},
	}
	require.NoError(t, srv.Start(context.Background()))

	closeDone := make(chan error, 1)
	go func() { closeDone <- srv.Close() }()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Close must invoke OnClose")
	}

	select {
	case err := <-closeDone:
		t.Fatalf("Close returned while OnClose was still blocked: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-closeDone, "Close must return once OnClose finishes")

	require.NoError(t, srv.Close(), "a second Close must be a no-op")
	assert.Equal(t, int64(1), calls.Load(), "OnClose must be invoked exactly once")
}

// setEphemeralAgentSeam overrides newEphemeralDaemonAgentFn to build a
// hermetic test agent per workDir, recording every created agent. The
// returned snapshot function lets tests await async shutdown before
// their temp dirs are torn down: releaseAgent runs Shutdown on a
// background goroutine, and Shutdown flushes session/config files into
// dirs created by t.TempDir inside newTestAgent — a test that returns
// first races RemoveAll with those writes ("unlinkat ... directory not
// empty"). Restored via t.Cleanup.
func setEphemeralAgentSeam(t *testing.T) (createdAgents func() []*agent.Agent) {
	t.Helper()
	origFn := newEphemeralDaemonAgentFn
	t.Cleanup(func() { newEphemeralDaemonAgentFn = origFn })
	var mu sync.Mutex
	var created []*agent.Agent
	newEphemeralDaemonAgentFn = func(workDir string, _ daemon.QueryOptions) (*agent.Agent, error) {
		a := newTestAgent(t)
		a.SetWorkspaceRoot(workDir)
		mu.Lock()
		created = append(created, a)
		mu.Unlock()
		return a, nil
	}
	return func() []*agent.Agent {
		mu.Lock()
		defer mu.Unlock()
		return append([]*agent.Agent(nil), created...)
	}
}

// awaitAgentsShutdown blocks until every agent in the snapshot has
// finished Shutdown. Tests using setEphemeralAgentSeam MUST defer this
// (a test-body defer, not t.Cleanup — cleanups run after TempDir's
// RemoveAll, which is exactly the write the wait must precede).
func awaitAgentsShutdown(t *testing.T, agents []*agent.Agent) {
	t.Helper()
	for _, a := range agents {
		require.Eventually(t, func() bool { return a.IsShutdown() },
			10*time.Second, 20*time.Millisecond,
			"ephemeral agent must finish shutting down before the test's temp dirs are removed")
	}
}

// TestSharedAgentService_ExecuteTool_EndToEnd wires the real SharedAgentService
// behind a real socket and runs a real tool call through it — closing the loop
// on ExecuteTool: wire protocol → dispatch → per-call ephemeral agent →
// ExecuteToolByName → seed registry → tool handler, all together.
func TestSharedAgentService_ExecuteTool_EndToEnd(t *testing.T) {
	createdAgents := setEphemeralAgentSeam(t)
	// The agent's Shutdown (async in releaseAgent) flushes config/session
	// files under t.TempDir; wait for it before returning or TempDir's
	// RemoveAll races those writes.
	defer awaitAgentsShutdown(t, createdAgents())

	svc := NewSharedAgentService(nil)
	sockPath := startCmdAgentServer(t, svc)

	workDir := t.TempDir()
	filePath := filepath.Join(workDir, "hello.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("hello from daemon tool"), 0o644))

	client, err := daemon.NewAgentClient(sockPath)
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := client.ExecuteTool(ctx, "read_file", map[string]any{"path": filePath}, workDir)
	require.NoError(t, err, "RPC must succeed")
	require.NotNil(t, result)
	assert.Empty(t, result.Error, "tool must not have returned an error")
	assert.Contains(t, result.Content, "hello from daemon tool", "read_file must return file contents")
}

// TestSharedAgentService_ExecuteTool_RejectsEmptyWorkDir verifies ExecuteTool
// refuses a call without a work_dir before the agent constructor is invoked.
func TestSharedAgentService_ExecuteTool_RejectsEmptyWorkDir(t *testing.T) {
	var seamCalls atomic.Int64
	origFn := newEphemeralDaemonAgentFn
	t.Cleanup(func() { newEphemeralDaemonAgentFn = origFn })
	newEphemeralDaemonAgentFn = func(workDir string, _ daemon.QueryOptions) (*agent.Agent, error) {
		seamCalls.Add(1)
		return nil, nil
	}

	svc := NewSharedAgentService(nil)
	_, err := svc.ExecuteTool(context.Background(), "read_file", map[string]any{"path": "x"}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "work_dir")
	assert.Zero(t, seamCalls.Load(), "the agent constructor must not be invoked for empty work_dir")
}

// TestSharedAgentService_ExecuteTool_UnknownTool verifies that an unknown
// tool name returns a ToolResult with a non-empty Error and nil RPC error.
func TestSharedAgentService_ExecuteTool_UnknownTool(t *testing.T) {
	createdAgents := setEphemeralAgentSeam(t)
	defer awaitAgentsShutdown(t, createdAgents())

	svc := NewSharedAgentService(nil)
	result, err := svc.ExecuteTool(context.Background(), "no_such_tool", map[string]any{}, t.TempDir())
	require.NoError(t, err, "RPC must succeed even when the tool is unknown")
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Error, "unknown tool must return an error in ToolResult.Error")
}

// TestSharedAgentService_ExecuteTool_ReleasesEphemeralAgents verifies that
// an ephemeral agent created by ExecuteTool is shut down after the call
// returns (same pattern as TestSharedAgentService_ReleasesEphemeralAgents).
func TestSharedAgentService_ExecuteTool_ReleasesEphemeralAgents(t *testing.T) {
	createdAgents := setEphemeralAgentSeam(t)

	workDir := t.TempDir()
	filePath := filepath.Join(workDir, "f.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o644))

	svc := NewSharedAgentService(nil)
	_, err := svc.ExecuteTool(context.Background(), "read_file", map[string]any{"path": filePath}, workDir)
	require.NoError(t, err)

	created := createdAgents()
	require.NotEmpty(t, created, "the tool call must have created an ephemeral agent")
	for _, a := range created {
		require.Eventually(t, func() bool { return a.IsShutdown() },
			10*time.Second, 20*time.Millisecond,
			"ephemeral agent must be shut down after the tool call returns")
	}
}
