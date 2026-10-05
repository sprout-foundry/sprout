//go:build !js

// Package webui provides React web server with embedded assets
package webui

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agenttools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	lspproxy "github.com/sprout-foundry/sprout/pkg/lsp/proxy"
	"github.com/sprout-foundry/sprout/pkg/preview"
	"github.com/sprout-foundry/sprout/pkg/security"
)

var webuiLogger = slog.Default().With("component", "webui")

func (ws *ReactWebServer) log() *slog.Logger {
	if ws != nil && ws.logger != nil {
		return ws.logger
	}
	return webuiLogger
}

// ReactWebServer provides the React web UI
type ReactWebServer struct {
	logger              *slog.Logger
	agent               *agent.Agent
	eventBus            *events.EventBus
	daemonRoot          string
	workspaceRoot       string
	sshHostAlias        string
	sshSessionKey       string
	sshLauncherURL      string
	sshHomePath         string
	fileConsents        *fileConsentManager
	clientContexts      map[string]*webClientContext
	chatSubscribers     *chatSubscribersRegistry
	port                int
	bindAddr            string
	server              *http.Server
	listener            net.Listener
	upgrader            websocket.Upgrader
	connections         sync.Map // map[*websocket.Conn]*ConnectionInfo
	fileWatcher         *fileWatcher
	terminalManager     *TerminalManager
	securityPromptMgr   *security.ApprovalManager
	askUserMgr          *agenttools.AskUserManager
	isRunning           bool
	mutex               sync.RWMutex
	startTime           time.Time
	activeWSByUserID    sync.Map         // map[string]*activeWSConn — SP-118 Mode1: tracks single active WS per user (agent mode)
	userConnections     *UserConnections // SP-118 Mode2: tracks N concurrent WS per user (daemon mode)
	queryCount          int
	activeQueries       int
	activeQueryClientID string
	fixReviewJobs       map[string]*gitFixReviewJob
	fixReviewMu         sync.RWMutex
	sshSessions         map[string]*sshWorkspaceSession
	sshSessionsMu       sync.Mutex
	sshInFlight         map[string]chan struct{}
	sshInFlightMu       sync.Mutex
	sshLaunchStatuses   map[string]*sshLaunchStatus
	sshLaunchStatusMu   sync.RWMutex
	workspaceExecMu     sync.Mutex

	// clientWorkspaces remembers the last workspace each non-default client
	// explicitly selected (via setClientWorkspaceRoot or worktree switches).
	// When the idle-eviction worker removes a client context, the tab is
	// still alive on the other end; the next request recreates the context,
	// and without this map that recreated context silently falls back to
	// the daemon's launch directory — every new terminal session spawned
	// in the wrong cwd while the UI still displayed the chosen workspace.
	clientWorkspaces map[string]string

	// agentTeardownWg tracks in-flight releaseAgents goroutines so Shutdown
	// (and tests) can wait for agent teardown — which writes history — to
	// finish rather than racing it.
	agentTeardownWg sync.WaitGroup

	lastClientContextCleanupAt      time.Time
	lastClientContextCleanupRemoved int
	totalClientContextsRemoved      int
	lspManager                      *lspproxy.Manager
	normalizedAllowedOrigins        []string     // Pre-normalized from SPROUT_ALLOWED_ORIGINS env var
	trustedUserHeader               string       // Header name for user ID extraction in service mode
	serviceMode                     bool         // true when running as a managed service (SPROUT_SERVICE=1)
	agentEnforceSingleSession       bool         // true: single-active WS (sprout agent/CWS mode); false: multi-session (daemon) — SP-118
	authToken                       string       // Auth token for write endpoint protection (SPROUT_AUTH_TOKEN)
	socketPath                      string       // Unix domain socket path (when non-empty, listen on socket instead of TCP)
	startOnce                       sync.Once    // Ensures background workers are started exactly once
	serverCtx                       atomic.Value // context.Context — safe to read without ws.mutex

	// previewManagers caches one preview.Manager (SP-155 §155a, TODO
	// 155.4: the dev-server manager behind /api/preview/*) per project
	// root, so a worktree switch to a different root gets its own manager
	// (and dev server) rather than clobbering the running one.
	previewManagers   map[string]*preview.Manager
	previewManagersMu sync.Mutex

	// hostedPreview is the active platform-registered preview (SP-155
	// §155a, TODO 155.5): the URL the agent's register_preview_port call
	// returned, recorded by the event-bus subscriber when it fires. While
	// set, /api/preview/status reports it (running, hosted) in preference
	// to any local dev server. nil means no hosted preview is registered.
	// Guarded by hostedPreviewMu.
	hostedPreview   *hostedPreviewInfo
	hostedPreviewMu sync.RWMutex
}

// IsSharedMode reports whether the server is in "shared agent" mode —
// where a CLI process launched the web server with a live agent. In this
// mode, the WebUI shares the CLI's agent instance (same conversation,
// same session) instead of creating its own per-chat agents.
//
// This is the non-daemon interactive case: `sprout` started with a TTY
// passes its agent to NewReactWebServer, while `sprout daemon` passes nil.
// SPROUT_DAEMON=1 marks the daemon (set in RunAgent when daemonMode is
// true) even when the daemon carries a live agent — a daemon with a
// provider still serves per-client sessions, so it must NOT report shared
// mode or every chat-session create/switch 403s (SP-142 regression).
func (ws *ReactWebServer) IsSharedMode() bool {
	if ws.agent == nil || ws.serviceMode {
		return false
	}
	return configuration.GetEnvSimple("DAEMON") != "1"
}

// IsRunning returns true if the web server is running
func (ws *ReactWebServer) IsRunning() bool {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	return ws.isRunning
}

// GetPort returns the port the web server is running on
func (ws *ReactWebServer) GetPort() int {
	return ws.port
}

// GetWorkspaceRoot returns the current workspace root.
func (ws *ReactWebServer) GetWorkspaceRoot() string {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	return ws.workspaceRoot
}

// looksLikeUserHome heuristically reports whether dir resembles a real
// per-user home directory rather than a system path that launchd or systemd
// might leak in via $HOME (e.g., "/", "/var/root", "/var/empty", "/nonexistent",
// or a "/usr/..." system dir). Conservative: a true positive triggers a
// /etc/passwd lookup; a false positive only loses the chance to recover.
func looksLikeUserHome(dir string) bool {
	if dir == "" {
		return false
	}
	clean := strings.TrimRight(dir, "/")
	// Empty after trim means dir was "/" — definitely not a user home.
	if clean == "" {
		return false
	}
	// Obviously-not-user system paths.
	systemRoots := []string{"/var/root", "/var/empty", "/nonexistent", "/usr", "/etc", "/tmp", "/var", "/private"}
	for _, sys := range systemRoots {
		if clean == sys {
			return false
		}
	}
	// Typical user-home prefixes across platforms.
	for _, prefix := range []string{"/Users/", "/home/", "/root"} {
		if strings.HasPrefix(clean+"/", prefix) || clean == strings.TrimRight(prefix, "/") {
			return true
		}
	}
	// Anything else (custom mount, container, NFS share) — give it the benefit
	// of the doubt rather than triggering a fallback that could be wrong.
	return true
}

// GetDaemonRoot returns the daemon-scoped filesystem root.
func (ws *ReactWebServer) GetDaemonRoot() string {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	return ws.daemonRoot
}

// ActiveQueryCount returns the current number of active queries.
func (ws *ReactWebServer) ActiveQueryCount() int {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	return ws.activeQueries
}

// ActiveClientCount returns the number of client contexts whose LastSeenAt
// is within staleAfter of now. Used by the daemon's idle-reaper (SP-136 P2)
// to decide when no client has connected recently. A client counts as
// "active" if it performed any request (health ping, chat message, API call)
// inside the window — browsers that merely hold a WebSocket open without
// traffic are NOT counted, which is the correct signal for reaping an
// unattended daemon.
func (ws *ReactWebServer) ActiveClientCount(staleAfter time.Duration) int {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	cutoff := time.Now().Add(-staleAfter)
	n := 0
	for _, ctx := range ws.clientContexts {
		if ctx != nil && ctx.LastSeenAt.After(cutoff) {
			n++
		}
	}
	return n
}

// SetAgentEnforceSingleSession configures whether the WebSocket dispatcher
// should route connections through the single-active-session (Mode 1) path
// or the multi-session (Mode 2) path. SP-118 Phase 1.
//
//   - true  → Mode 1: only one browser window active per user at a time.
//     Conflicts trigger session_conflict and a takeover prompt. This is
//     sprout agent / CWS interactive mode.
//   - false → Mode 2: N parallel browser windows per user. Currently a
//     stub that accepts connections without enforcement; the full
//     implementation lands in SP-118-2. This is sprout service / daemon.
//
// Cmd should call this immediately after NewReactWebServer returns:
//
//	sprout agent path     → SetAgentEnforceSingleSession(true)
//	sprout service path   → leave false (or explicitly call with false)
//
// Dispatch uses this flag, NOT serviceMode. Tests in pkg/webui flip
// serviceMode=true to exercise the takeover flow under the Mode 1 path
// (e.g., TestSessionConflict_Takeover_UserMode); using serviceMode as
// the dispatch key would break them.
func (ws *ReactWebServer) SetAgentEnforceSingleSession(v bool) {
	ws.agentEnforceSingleSession = v
}

// HasActiveWebUIClients returns true if one or more WebSocket connections
// of type "webui" are currently connected.  The security prompt routing
// logic uses this to decide whether to route prompts through the WebUI
// event bus or fall back to CLI-based prompting.
func (ws *ReactWebServer) HasActiveWebUIClients() bool {
	hasWebUI := false
	ws.connections.Range(func(_, value interface{}) bool {
		if info, ok := value.(*ConnectionInfo); ok && info.Type == "webui" {
			hasWebUI = true
			return false // stop iterating
		}
		return true
	})
	return hasWebUI
}

// SetWorkspaceRoot updates the active workspace root, changes the process cwd,
// and resets terminal state.
func (ws *ReactWebServer) SetWorkspaceRoot(path string) (string, error) {
	return ws.setClientWorkspaceRoot(defaultWebClientID, path)
}

// countConnections returns the current number of WebSocket connections
func (ws *ReactWebServer) countConnections() int {
	count := 0
	ws.connections.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// GetSecurityPromptMgr returns the security approval manager used by this web server.
func (ws *ReactWebServer) GetSecurityPromptMgr() *security.ApprovalManager {
	return ws.securityPromptMgr
}

// GetAskUserMgr returns the ask user manager used by this web server.
func (ws *ReactWebServer) GetAskUserMgr() *agenttools.AskUserManager {
	return ws.askUserMgr
}
