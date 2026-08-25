package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/daemon"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// defaultAgentSocketName is the Unix socket the daemon serves the SP-136 P4
// agent protocol on, under the data dir (override with SPROUT_DAEMON_AGENT_SOCKET).
const defaultAgentSocketName = "agent.sock"

// AgentSocketPath returns the daemon agent socket path, honoring the
// SPROUT_DAEMON_AGENT_SOCKET override.
func AgentSocketPath() string {
	if p := os.Getenv("SPROUT_DAEMON_AGENT_SOCKET"); p != "" {
		return p
	}
	dataDir, err := envutil.DataDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "sprout", defaultAgentSocketName)
	}
	return filepath.Join(dataDir, defaultAgentSocketName)
}

// SharedAgentService adapts the daemon to the SP-136 P4 agent socket
// protocol. The daemon is a single long-lived process that may serve
// one-shot queries from many different callers, in many different project
// directories, over its lifetime — Query/StreamQuery must not let one call's
// state (conversation history, workspace root) leak into another's.
//
// So Query/StreamQuery build a fresh, throwaway *agent.Agent per call rather
// than reusing a shared one: agent construction resolves to the same
// process-wide local-model singleton either way (pkg/factory/factory.go →
// localmodel.GetLocalProvider()), so this doesn't reload the GPU-resident
// model — it only costs the (cheap) Agent object construction, in exchange
// for correct per-call isolation.
//
// `a` is retained only for the (currently client-unused) session RPCs below,
// which predate the fix and don't go through the one-shot query path.
// Tool execution via the socket is not yet wired (ExecuteTool below) —
// callers fall back to in-process for full tool workflows.
type SharedAgentService struct {
	a *agent.Agent

	// teardownWg tracks ephemeral agent shutdowns started off the response
	// path so WaitForTeardown can block until they finish (mirrors
	// pkg/webui/agent_teardown.go's agentTeardownWg). teardownMu and
	// teardownClosed gate Adds so an Add can never race a blocked Wait.
	teardownWg     sync.WaitGroup
	teardownMu     sync.Mutex
	teardownClosed bool
}

// NewSharedAgentService wraps an agent for socket serving.
func NewSharedAgentService(a *agent.Agent) *SharedAgentService {
	return &SharedAgentService{a: a}
}

// beginQuery reserves a teardown slot for one in-flight query, returning
// false once WaitForTeardown has begun — Add must not run concurrently with
// a blocked Wait, so every Add happens under teardownMu before the close
// flag is set, or not at all.
func (s *SharedAgentService) beginQuery() bool {
	s.teardownMu.Lock()
	defer s.teardownMu.Unlock()
	if s.teardownClosed {
		return false
	}
	s.teardownWg.Add(1)
	return true
}

// releaseAgent starts shutting down an ephemeral query agent on a background
// goroutine, off the response path. Dropping the pointer is not enough:
// Agent.Shutdown is what releases the shared embedding manager refcount
// (acquired at construction), stops MCP child processes, cancels
// lifetime/interrupt contexts, waits background goroutines, and closes the
// async output channel. Without it each one-shot query leaks a refcount and
// the workspace's HNSW store can never close again.
func (s *SharedAgentService) releaseAgent(a *agent.Agent) {
	if a == nil {
		// Agent never created — release the slot beginQuery reserved.
		s.teardownWg.Done()
		return
	}
	utils.SafeGo(slog.Default(), "daemon-ephemeral-agent-shutdown", func() {
		defer s.teardownWg.Done()
		a.Shutdown()
	})
}

// WaitForTeardown blocks until every ephemeral agent released so far has
// finished shutting down, bounded to 10s so a hung Agent.Shutdown can't
// wedge daemon exit forever. AgentServer.OnClose calls it so the daemon
// does not exit while an agent is still flushing its embedding store.
func (s *SharedAgentService) WaitForTeardown() {
	// The closed flag — not a held lock — is what excludes new Adds: set it
	// under teardownMu so no beginQuery can slip in, then unlock before Wait
	// so in-flight queries can still reach their Done.
	s.teardownMu.Lock()
	s.teardownClosed = true
	s.teardownMu.Unlock()

	done := make(chan struct{})
	go func() {
		s.teardownWg.Wait()
		close(done)
	}()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		slog.Default().Warn("daemon teardown did not complete within 10s; exiting without waiting for agent shutdown")
	}
}

// ListSessions implements daemon.AgentService.
func (s *SharedAgentService) ListSessions(context.Context) ([]daemon.SessionInfo, error) {
	return []daemon.SessionInfo{{ID: s.a.GetSessionID(), Name: "default", Active: true}}, nil
}

// CreateSession implements daemon.AgentService.
func (s *SharedAgentService) CreateSession(_ context.Context, name string) (*daemon.SessionInfo, error) {
	sid := "session_" + fmt.Sprintf("%d", time.Now().UnixNano())
	s.a.SetSessionID(sid)
	return &daemon.SessionInfo{ID: sid, Name: name, Active: true}, nil
}

// SwitchSession implements daemon.AgentService.
func (s *SharedAgentService) SwitchSession(_ context.Context, sessionID string) (*daemon.SessionInfo, error) {
	if sessionID == "" {
		return nil, errors.New("empty session id")
	}
	s.a.SetSessionID(sessionID)
	return &daemon.SessionInfo{ID: sessionID, Name: sessionID, Active: true}, nil
}

// Query implements daemon.AgentService: a fresh, isolated agent per call
// (see the type doc for why), scoped to the caller's workDir.
func (s *SharedAgentService) Query(_ context.Context, prompt, workDir string) (string, error) {
	if !s.beginQuery() {
		return "", errors.New("daemon is shutting down")
	}
	callAgent, err := newEphemeralDaemonAgentFn(workDir)
	if err != nil {
		// Agent never created — release the tracking slot beginQuery reserved.
		s.releaseAgent(nil)
		return "", err
	}
	// Shutdown starts as soon as the query returns, off the response path.
	defer s.releaseAgent(callAgent)
	return callAgent.ProcessQuery(prompt)
}

// StreamQuery implements daemon.AgentService (one-shot result as a single
// delta; full token streaming is a future protocol refinement).
func (s *SharedAgentService) StreamQuery(_ context.Context, prompt, workDir string, emit func(daemon.StreamEvent) error) error {
	if !s.beginQuery() {
		return errors.New("daemon is shutting down")
	}
	callAgent, err := newEphemeralDaemonAgentFn(workDir)
	if err != nil {
		// Agent never created — release the tracking slot beginQuery reserved.
		s.releaseAgent(nil)
		return err
	}
	// Shutdown starts as soon as the query returns, off the response path.
	defer s.releaseAgent(callAgent)
	result, err := callAgent.ProcessQuery(prompt)
	if err != nil {
		return err
	}
	return emit(daemon.StreamEvent{Type: "delta", Content: result})
}

// newEphemeralDaemonAgentFn is a test seam so cmd tests can substitute a
// capturing constructor and assert teardown of the agents a query creates.
var newEphemeralDaemonAgentFn = newEphemeralDaemonAgent

// newEphemeralDaemonAgent builds a minimal agent for one daemon-routed
// one-shot query: no onboarding (not appropriate for a background socket
// server) and no local-model preload (the daemon's own top-level agent
// already triggered that at startup, or it happens lazily on first real use
// either way — see LocalProvider.ensureLoaded). Provider/model/persona
// overrides from the calling CLI's flags aren't transmitted over the wire
// protocol today, so this always uses the daemon process's own defaults —
// consistent with every other field the protocol doesn't carry yet.
func newEphemeralDaemonAgent(workDir string) (*agent.Agent, error) {
	if strings.TrimSpace(workDir) == "" {
		return nil, errors.New("daemon: query missing work_dir — refusing to guess a project directory")
	}
	callAgent, err := agent.NewAgent()
	if err != nil {
		return nil, fmt.Errorf("daemon: create per-query agent: %w", err)
	}
	callAgent.SetWorkspaceRoot(workDir)
	return callAgent, nil
}

// ExecuteTool implements daemon.AgentService.
func (s *SharedAgentService) ExecuteTool(context.Context, string, map[string]any) (*daemon.ToolResult, error) {
	return nil, errors.New("tool execution over the daemon socket is not wired yet — run in-process for tool workflows")
}

// isDaemonReachableForAgentRouting reports whether a daemon is listening on
// the agent socket AND actually responsive — not just that something
// accepted the connection. AgentClient's dial alone doesn't guarantee that:
// a daemon that's spawned but stuck (see maybeAutoStartDaemon) can still
// accept a connection into the OS backlog while never servicing it, so this
// does one cheap round-trip (ListSessions, the lightest read-only op the
// protocol has) with a short timeout, mirroring the real handshake
// NewRemoteEmbeddingProvider already does for the embedding socket.
//
// Used to decide whether createChatAgent can skip its own local-model
// preload because tryDaemonOneShot (called later in the same invocation)
// will actually serve the query. Callers must independently confirm this
// invocation could reach that same tryDaemonOneShot call (see
// shouldPreloadLocalModel) — this function only answers "is a daemon there
// and alive," not "will this specific query be routed to it."
func isDaemonReachableForAgentRouting() bool {
	if v := os.Getenv("SPROUT_DAEMON_AGENT"); v == "0" {
		return false
	}
	client, err := daemon.NewAgentClient(AgentSocketPath())
	if err != nil {
		return false
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_, err = client.ListSessions(ctx)
	return err == nil
}

// startDaemonAgentServer starts the daemon-side agent socket server
// (SP-136 P4). Returns the server or nil when not applicable (non-daemon,
// no shared agent).
func startDaemonAgentServer(ctx context.Context, daemonMode bool, chatAgent *agent.Agent) *daemon.AgentServer {
	if !daemonMode || chatAgent == nil {
		return nil
	}

	svc := NewSharedAgentService(chatAgent)
	srv := &daemon.AgentServer{
		SocketPath: AgentSocketPath(),
		Service:    svc,
		// Track socket traffic so the idle reaper sees agent socket use even
		// with no WebUI activity.
		Activity: daemon.NewDaemonActivity(),
		// Block daemon exit until ephemeral query agents have flushed their
		// embedding stores — mirrors webui's waitForAgentTeardown.
		OnClose: svc.WaitForTeardown,
	}
	if err := srv.Start(ctx); err != nil {
		console.GlyphWarning.Fprintf(os.Stderr,
			"agent socket server failed to start at %s: %v\n", AgentSocketPath(), err)
		return nil
	}
	console.GlyphDim.Printf("Agent socket serving at %s", AgentSocketPath())
	return srv
}

// tryDaemonOneShot implements SP-136 P4 one-shot CLI-on-daemon:
// `sprout agent "query"` connects to the daemon's agent socket, runs the
// query there, prints the result, and disconnects. Returns (handled=true,
// err) when the daemon served the query. Returns (false, nil) when the
// daemon socket is unavailable or the run must not route (see below) —
// the caller falls back to in-process execution (the safety net).
//
// jsonOut runs never route: the socket protocol can't carry the JSON
// envelope's metrics or the response text, so --output-json one-shots run
// in-process instead (the agent_modes.go gate skips this call for them;
// this guard keeps the contract self-contained even if a future caller
// forgets). The daemon path is interactive/plain-text use only.
func tryDaemonOneShot(ctx context.Context, query string, jsonOut bool) (bool, error) {
	if jsonOut {
		return false, nil
	}
	if v := os.Getenv("SPROUT_DAEMON_AGENT"); v == "0" {
		return false, nil
	}
	if query == "" {
		return false, nil
	}

	// The daemon is a single long-lived process that may serve callers from
	// many different project directories over its lifetime — it has no way
	// to know which one this query is for except what we tell it here.
	workDir, err := os.Getwd()
	if err != nil {
		return false, nil // can't determine our own cwd — in-process fallback
	}

	client, err := daemon.NewAgentClient(AgentSocketPath())
	if err != nil {
		// Daemon not reachable — in-process fallback.
		return false, nil
	}
	defer client.Close()

	console.GlyphAction.Printf("Running via daemon at %s", AgentSocketPath())
	result, err := client.Query(ctx, query, workDir)
	if err != nil {
		// The daemon was up but the query failed — surface it, don't
		// silently re-run in-process (the daemon owns the agent state).
		return true, err
	}

	fmt.Println(result)
	return true, nil
}
