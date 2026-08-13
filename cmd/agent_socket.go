package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/daemon"
	"github.com/sprout-foundry/sprout/pkg/envutil"
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
}

// NewSharedAgentService wraps an agent for socket serving.
func NewSharedAgentService(a *agent.Agent) *SharedAgentService {
	return &SharedAgentService{a: a}
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
	callAgent, err := newEphemeralDaemonAgent(workDir)
	if err != nil {
		return "", err
	}
	return callAgent.ProcessQuery(prompt)
}

// StreamQuery implements daemon.AgentService (one-shot result as a single
// delta; full token streaming is a future protocol refinement).
func (s *SharedAgentService) StreamQuery(_ context.Context, prompt, workDir string, emit func(daemon.StreamEvent) error) error {
	callAgent, err := newEphemeralDaemonAgent(workDir)
	if err != nil {
		return err
	}
	result, err := callAgent.ProcessQuery(prompt)
	if err != nil {
		return err
	}
	return emit(daemon.StreamEvent{Type: "delta", Content: result})
}

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
	srv := &daemon.AgentServer{SocketPath: AgentSocketPath(), Service: svc}
	if err := srv.Start(ctx); err != nil {
		console.GlyphWarning.Fprintf(os.Stderr,
			"agent socket server failed to start at %s: %v\n", AgentSocketPath(), err)
		return nil
	}
	console.GlyphDim.Printf("Agent socket serving at %s", AgentSocketPath())
	return srv
}

// tryDaemonOneShot implements SP-136 P4 one-shot CLI-on-daemon:
// `sprout agent "query"` / `sprout agent --json "query"` connects to the
// daemon's agent socket, runs the query there, prints the result, and
// disconnects. Returns (handled=true, err) when the daemon served the query.
// Returns (false, nil) when the daemon socket is unavailable — the caller
// falls back to in-process execution (the safety net).
func tryDaemonOneShot(ctx context.Context, query string, jsonOut bool) (bool, error) {
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
		if jsonOut {
			emitJSONResult(query, time.Now(), err, nil)
			return true, nil
		}
		return true, err
	}

	if jsonOut {
		emitJSONResult(query, time.Now(), nil, nil)
	} else {
		fmt.Println(result)
	}
	return true, nil
}
