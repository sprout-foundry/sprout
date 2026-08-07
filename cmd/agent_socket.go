package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// SharedAgentService adapts the daemon's shared agent to the SP-136 P4 agent
// socket protocol. The daemon owns the agent; the CLI is a presentation
// layer. Session ops map to the agent's session ID; tool execution via the
// socket is not yet wired (the protocol op exists and is covered by tests) —
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

// Query implements daemon.AgentService — the daemon runs the agent loop.
func (s *SharedAgentService) Query(_ context.Context, prompt string) (string, error) {
	return s.a.ProcessQuery(prompt)
}

// StreamQuery implements daemon.AgentService (one-shot result as a single
// delta; full token streaming is a future protocol refinement).
func (s *SharedAgentService) StreamQuery(_ context.Context, prompt string, emit func(daemon.StreamEvent) error) error {
	result, err := s.a.ProcessQuery(prompt)
	if err != nil {
		return err
	}
	return emit(daemon.StreamEvent{Type: "delta", Content: result})
}

// ExecuteTool implements daemon.AgentService.
func (s *SharedAgentService) ExecuteTool(context.Context, string, map[string]any) (*daemon.ToolResult, error) {
	return nil, errors.New("tool execution over the daemon socket is not wired yet — run in-process for tool workflows")
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

	client, err := daemon.NewAgentClient(AgentSocketPath())
	if err != nil {
		// Daemon not reachable — in-process fallback.
		return false, nil
	}
	defer client.Close()

	console.GlyphAction.Printf("Running via daemon at %s", AgentSocketPath())
	result, err := client.Query(ctx, query)
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
