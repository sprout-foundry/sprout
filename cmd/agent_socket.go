package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
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
// one-shot queries and tool calls from many different callers, in many
// different project directories, over its lifetime — Query/StreamQuery/
// ExecuteTool must not let one call's state (conversation history,
// workspace root) leak into another's.
//
// So Query/StreamQuery/ExecuteTool build a fresh, throwaway *agent.Agent
// per call rather than reusing a shared one: agent construction resolves
// to the same process-wide local-model singleton either way
// (pkg/factory/factory.go → localmodel.GetLocalProvider()), so this
// doesn't reload the GPU-resident model — it only costs the (cheap) Agent
// object construction, in exchange for correct per-call isolation.
//
// Query/StreamQuery also apply the caller's QueryOptions (provider/model/
// persona/risk-profile/max-iterations) on top of the daemon's defaults,
// so the CLI's flags are honored instead of silently dropped.
//
// `a` is retained only for the (currently client-unused) session RPCs
// below, which predate the fix and don't go through the one-shot path.
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
// (see the type doc for why), scoped to the caller's workDir, with the
// caller's flag overrides applied on top of the daemon's defaults.
func (s *SharedAgentService) Query(_ context.Context, prompt, workDir string, opts daemon.QueryOptions) (string, error) {
	if !s.beginQuery() {
		return "", errors.New("daemon is shutting down")
	}
	callAgent, err := newEphemeralDaemonAgentFn(workDir, opts)
	if err != nil {
		// Constructor failed — release the slot (and the agent, in case a
		// constructor variant ever returns both) instead of leaking either.
		s.releaseAgent(callAgent)
		return "", err
	}
	// Shutdown starts as soon as the query returns, off the response path.
	defer s.releaseAgent(callAgent)
	return callAgent.ProcessQuery(prompt)
}

// StreamQuery implements daemon.AgentService with real token streaming:
// the ephemeral agent's streaming callback forwards each assistant-text
// chunk as a "delta" event as it arrives. ProcessQuery returns "" once
// content has been streamed (seed's no-double-display contract), so a
// non-empty result means nothing streamed — emit it as one final delta so
// the client always receives the full text either way.
func (s *SharedAgentService) StreamQuery(_ context.Context, prompt, workDir string, opts daemon.QueryOptions, emit func(daemon.StreamEvent) error) error {
	if !s.beginQuery() {
		return errors.New("daemon is shutting down")
	}
	callAgent, err := newEphemeralDaemonAgentFn(workDir, opts)
	if err != nil {
		// Constructor failed — release the slot (and the agent, in case a
		// constructor variant ever returns both) instead of leaking either.
		s.releaseAgent(callAgent)
		return err
	}
	// Shutdown starts as soon as the query returns, off the response path.
	defer s.releaseAgent(callAgent)

	// A failed emit means the client is gone — stop the run instead of
	// burning LLM tokens on output nobody will receive.
	var clientGone bool
	callAgent.EnableStreaming(func(chunk string) {
		if clientGone {
			return
		}
		if err := emit(daemon.StreamEvent{Type: "delta", Content: chunk}); err != nil {
			clientGone = true
			callAgent.TriggerInterrupt()
			return
		}
	})
	result, err := callAgent.ProcessQuery(prompt)
	if err != nil {
		if clientGone {
			// The interrupt we triggered surfaces as a query error — the
			// client is gone, so nobody is left to receive it.
			return nil
		}
		return err
	}
	if result != "" {
		return emit(daemon.StreamEvent{Type: "delta", Content: result})
	}
	return nil
}

// newEphemeralDaemonAgentFn is a test seam so cmd tests can substitute a
// capturing constructor and assert teardown of the agents a query creates.
var newEphemeralDaemonAgentFn = newEphemeralDaemonAgent

// daemonModelSelector composes the provider/model flags into the single
// selector string NewAgentWithModel accepts: "provider:model", bare
// provider, bare model, or "" (daemon defaults).
func daemonModelSelector(provider, model string) string {
	switch {
	case provider != "" && model != "":
		return provider + ":" + model
	case provider != "":
		return provider
	default:
		return model
	}
}

// newEphemeralDaemonAgent builds a minimal agent for one daemon-routed
// one-shot query: no onboarding (not appropriate for a background socket
// server) and no local-model preload (the daemon's own top-level agent
// already triggered that at startup, or it happens lazily on first real use
// either way — see LocalProvider.ensureLoaded). opts carry the caller's
// provider/model/persona/risk-profile/iteration flags and are applied
// exactly like createChatAgent does them in-process.
func newEphemeralDaemonAgent(workDir string, opts daemon.QueryOptions) (*agent.Agent, error) {
	if strings.TrimSpace(workDir) == "" {
		return nil, errors.New("daemon: query missing work_dir — refusing to guess a project directory")
	}
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf("daemon: resolve work_dir %q: %w", workDir, err)
	}
	workDir = absWorkDir

	// Layer the caller's workspace config (.sprout under workDir) over the
	// global config, mirroring createChatAgent's auto-detected-workspace
	// path: workspace-defined custom risk profiles and provider settings
	// must resolve the same over the daemon as they do in-process.
	// envutil.ConfigDir honors SPROUT_CONFIG — the daemon process's config
	// root, not whatever directory the calling CLI happened to run from.
	selector := daemonModelSelector(opts.Provider, opts.Model)
	var callAgent *agent.Agent
	if globalDir, gerr := envutil.ConfigDir(); gerr == nil {
		callAgent, err = agent.NewAgentWithLayersInWorkspace(globalDir, workDir, workDir, selector)
	} else {
		callAgent, err = agent.NewAgentWithModel(selector)
	}
	if err != nil {
		return nil, fmt.Errorf("daemon: create per-query agent: %w", err)
	}
	callAgent.SetWorkspaceRoot(workDir)
	// From here on the caller releases the slot (nil) on error, not the
	// agent — an agent abandoned mid-construction still holds the shared
	// embedding-manager refcount and MCP lifetime contexts, so shut it
	// down before returning each error.
	if opts.Persona != "" {
		if err := callAgent.ApplyPersona(opts.Persona); err != nil {
			callAgent.Shutdown()
			return nil, fmt.Errorf("daemon: apply persona %q: %w", opts.Persona, err)
		}
	}
	if opts.RiskProfile != "" {
		// Mirror createChatAgent's --risk-profile handling: accept built-in
		// names OR user-defined profiles from the daemon's config layer.
		var cfg *configuration.Config
		if cm := callAgent.GetConfigManager(); cm != nil {
			cfg = cm.GetConfig()
		}
		if !configuration.IsValidRiskProfileWithConfig(opts.RiskProfile, cfg) {
			callAgent.Shutdown()
			return nil, fmt.Errorf("daemon: invalid risk_profile %q (valid: readonly, cautious, default, permissive, unrestricted, or a config-defined profile)", opts.RiskProfile)
		}
		callAgent.SetRiskProfileOverride(configuration.RiskProfile(opts.RiskProfile))
	}
	if opts.MaxIterations > 0 {
		callAgent.SetMaxIterations(opts.MaxIterations)
	}
	return callAgent, nil
}

// ExecuteTool implements daemon.AgentService: builds a fresh agent scoped
// to the caller's workDir, executes the tool via seed's registry, and
// returns the result. Tool failures travel in ToolResult.Error (not as
// an RPC error) — the RPC itself succeeded.
func (s *SharedAgentService) ExecuteTool(ctx context.Context, name string, args map[string]any, workDir string) (*daemon.ToolResult, error) {
	if !s.beginQuery() {
		return nil, errors.New("daemon is shutting down")
	}
	if strings.TrimSpace(workDir) == "" {
		s.releaseAgent(nil)
		return nil, errors.New("daemon: execute_tool missing work_dir — refusing to guess a project directory")
	}
	callAgent, err := newEphemeralDaemonAgentFn(workDir, daemon.QueryOptions{})
	if err != nil {
		s.releaseAgent(callAgent)
		return nil, fmt.Errorf("daemon: create per-tool agent: %w", err)
	}
	defer s.releaseAgent(callAgent)

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal tool args: %w", err)
	}
	content, toolErr := callAgent.ExecuteToolByName(ctx, name, string(argsJSON))
	if toolErr != "" {
		return &daemon.ToolResult{Error: toolErr}, nil
	}
	return &daemon.ToolResult{Content: content}, nil
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
	// Behavior-changing flags that can't travel the wire protocol must be
	// honored in-process, never silently dropped by daemon routing.
	if agentSkipDaemonRouting() {
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

	opts := daemon.QueryOptions{
		Persona:       agentPersona,
		Provider:      agentProvider,
		Model:         agentModel,
		RiskProfile:   agentRiskProfile,
		MaxIterations: maxIterations,
	}

	console.GlyphAction.Printf("Running via daemon at %s", AgentSocketPath())
	result, err := client.Query(ctx, query, workDir, opts)
	if err != nil {
		// The daemon was up but the query failed — surface it, don't
		// silently re-run in-process (the daemon owns the agent state).
		return true, err
	}

	fmt.Println(result)
	return true, nil
}
