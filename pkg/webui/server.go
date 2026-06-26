//go:build !js

// Package webui provides React web server with embedded assets
package webui

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
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
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// ReactWebServer provides the React web UI
type ReactWebServer struct {
	agent                           *agent.Agent
	eventBus                        *events.EventBus
	daemonRoot                      string
	workspaceRoot                   string
	sshHostAlias                    string
	sshSessionKey                   string
	sshLauncherURL                  string
	sshHomePath                     string
	fileConsents                    *fileConsentManager
	clientContexts                  map[string]*webClientContext
	chatSubscribers                 *chatSubscribersRegistry
	port                            int
	bindAddr                        string
	server                          *http.Server
	listener                        net.Listener
	upgrader                        websocket.Upgrader
	connections                     sync.Map // map[*websocket.Conn]*ConnectionInfo
	fileWatcher                     *fileWatcher
	terminalManager                 *TerminalManager
	securityPromptMgr               *security.ApprovalManager
	askUserMgr                      *agenttools.AskUserManager
	isRunning                       bool
	mutex                           sync.RWMutex
	startTime                       time.Time
	activeWSByUserID                sync.Map // map[string]*activeWSConn — SP-046: tracks single active WS per user
	queryCount                      int
	activeQueries                   int
	activeQueryClientID             string
	fixReviewJobs                   map[string]*gitFixReviewJob
	fixReviewMu                     sync.RWMutex
	sshSessions                     map[string]*sshWorkspaceSession
	sshSessionsMu                   sync.Mutex
	sshInFlight                     map[string]chan struct{}
	sshInFlightMu                   sync.Mutex
	sshLaunchStatuses               map[string]*sshLaunchStatus
	sshLaunchStatusMu               sync.RWMutex
	workspaceExecMu                 sync.Mutex
	lastClientContextCleanupAt      time.Time
	lastClientContextCleanupRemoved int
	totalClientContextsRemoved      int
	lspManager                      *lspproxy.Manager
	normalizedAllowedOrigins        []string     // Pre-normalized from SPROUT_ALLOWED_ORIGINS env var
	trustedUserHeader               string       // Header name for user ID extraction in service mode
	serviceMode                     bool         // true when running as a managed service (SPROUT_SERVICE=1)
	authToken                       string       // Auth token for write endpoint protection (SPROUT_AUTH_TOKEN)
	socketPath                      string       // Unix domain socket path (when non-empty, listen on socket instead of TCP)
	startOnce                       sync.Once    // Ensures background workers are started exactly once
	serverCtx                       atomic.Value // context.Context — safe to read without ws.mutex
}

// IsSharedMode reports whether the server is in "shared agent" mode —
// where a CLI process launched the web server with a live agent. In this
// mode, the WebUI shares the CLI's agent instance (same conversation,
// same session) instead of creating its own per-chat agents.
//
// This is the non-daemon interactive case: `sprout` started with a TTY
// passes its agent to NewReactWebServer, while `sprout daemon` passes nil.
func (ws *ReactWebServer) IsSharedMode() bool {
	return ws.agent != nil && !ws.serviceMode
}

// NewReactWebServer creates a new React web server
func NewReactWebServer(agent *agent.Agent, eventBus *events.EventBus, port int, bindAddr string, socketPath string, authToken string) (*ReactWebServer, error) {
	// Socket mode is mutually exclusive with TCP
	if socketPath != "" {
		// In socket mode, port and bindAddr are irrelevant
		port = 0
		bindAddr = ""
	} else {
		if port == 0 {
			port = DaemonPort
		}
		if bindAddr == "" {
			bindAddr = "127.0.0.1"
		}
	}

	workspaceRoot, err := os.Getwd()
	if err != nil {
		workspaceRoot = "."
	}
	workspaceRoot, err = filepathAbsEval(workspaceRoot)
	if err != nil {
		workspaceRoot = "."
	}

	// daemonRoot is the user's home directory — it scopes daemon-level
	// storage (sessions, SSH tunnels, config) AND the workspace browser
	// (handleAPIWorkspaceBrowse) to the user rather than a specific project.
	//
	// Resolution, most authoritative first:
	//   1. SPROUT_DAEMON_ROOT — baked into the launchd/systemd unit at install
	//      time when $HOME is reliable. Source of truth for managed services.
	//   2. os.UserHomeDir() ($HOME) — the normal interactive case.
	//   3. user.Current().HomeDir (`/etc/passwd`) — bypasses $HOME, used in
	//      service mode when the plist/unit is stale and $HOME may be wrong
	//      (e.g., a launchd cache from a pre-SPROUT_DAEMON_ROOT install).
	//   4. workspaceRoot (the CWD) — last resort only.
	//
	// Falling back to the CWD is dangerous under a service manager: launchd /
	// systemd can start the daemon in "/" or another system dir, and because
	// the workspace browser is scoped to daemonRoot the user would be unable
	// to reach their projects. Warn loudly when we have to fall back.
	serviceMode := configuration.GetEnvSimple("SERVICE") == "1"
	daemonRoot := configuration.GetEnvSimple("DAEMON_ROOT")
	rootSource := "SPROUT_DAEMON_ROOT"
	if daemonRoot == "" {
		if home, homeErr := os.UserHomeDir(); homeErr == nil && home != "" {
			daemonRoot = home
			rootSource = "$HOME"
		}
	}
	// In service mode, always reconcile against the OS user database.
	// SPROUT_DAEMON_ROOT is baked into the plist/unit at install time and can
	// go stale — e.g. a unit generated on Linux (/home/user) copied to macOS
	// where the real home is /Users/user, or a renamed user account. The
	// previous looksLikeUserHome heuristic failed because /home/user passes
	// both the heuristic AND the disk-existence check on macOS (synthetic
	// firmlinks). user.Current().HomeDir reads /etc/passwd (or its platform
	// equivalent), which is always authoritative for the current user.
	if serviceMode {
		if u, uErr := user.Current(); uErr == nil && u.HomeDir != "" && u.HomeDir != daemonRoot {
			log.Printf("[web] SPROUT_DAEMON_ROOT=%q disagrees with user.Current().HomeDir=%q; "+
				"using %q (reinstall the service to update the plist: sprout service uninstall && sprout service install)",
				daemonRoot, u.HomeDir, u.HomeDir)
			daemonRoot = u.HomeDir
			rootSource = "user.Current().HomeDir"
		}
	}
	if daemonRoot == "" {
		daemonRoot = workspaceRoot
		if serviceMode {
			log.Printf("[web] WARNING: could not resolve user home (SPROUT_DAEMON_ROOT, $HOME, and /etc/passwd all unavailable); "+
				"workspace browser is scoped to the daemon working dir %q and may not reach your projects — "+
				"reinstall the service (sprout service uninstall && sprout service install) to regenerate the unit", workspaceRoot)
		}
	}

	log.Printf("[web] startup: cwd=%s home=%s service=%v source=%s", workspaceRoot, daemonRoot, serviceMode, rootSource)
	if serviceMode {
		workspaceRoot = daemonRoot
	}

	// Initialize recent workspace tracking.
	initRecentWorkspaces()

	providercatalog.RefreshFromRemoteAsync("")

	securityPromptMgr := security.NewApprovalManager()

	askUserMgr := agenttools.NewAskUserManager()

	// Run startup permission check
	if configDir, err := configuration.GetConfigDir(); err == nil {
		// Check for symlinks pointing outside the config directory
		symlinkWarnings := security.CheckAllSymlinks(configDir)
		if len(symlinkWarnings) > 0 {
			log.Printf("[security] Symlink warnings:")
			for _, warn := range symlinkWarnings {
				log.Printf("  %s", warn)
			}
		}

		// Run the full permission check
		security.RunStartupCheck(configDir)
	}

	// Parse allowed origins from SPROUT_ALLOWED_ORIGINS env var
	// This is a comma-separated list of origin URLs to allow.
	// Origins are pre-normalized at startup so CheckOrigin can do
	// simple string comparisons without re-parsing on every request.
	allowedOriginsStr := strings.TrimSpace(configuration.GetEnvSimple("ALLOWED_ORIGINS"))
	var normalizedAllowedOrigins []string
	if allowedOriginsStr != "" {
		parts := strings.Split(allowedOriginsStr, ",")
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				parsed, err := url.Parse(trimmed)
				if err != nil {
					log.Printf("[web] WARNING: skipping malformed allowed origin %q: %v", trimmed, err)
					continue
				}
				normalizedAllowedOrigins = append(normalizedAllowedOrigins, normalizeOriginForCompare(parsed))
			}
		}
	}
	if len(normalizedAllowedOrigins) > 0 {
		log.Printf("[web] Allowed origins: %v", normalizedAllowedOrigins)
	}

	// Parse trusted user header (serviceMode already resolved above)
	trustedUserHeader := strings.TrimSpace(configuration.GetEnvSimple("TRUSTED_USER_HEADER"))
	if serviceMode {
		if trustedUserHeader != "" {
			log.Printf("[web] Trusted user header: %s (service mode)", trustedUserHeader)
		} else {
			log.Printf("[web] Service mode enabled but no trusted user header configured")
		}
	}

	// Parse auth token: explicit parameter takes precedence over env var
	resolvedAuthToken := authToken
	if resolvedAuthToken == "" {
		resolvedAuthToken = strings.TrimSpace(configuration.GetEnvSimple("AUTH_TOKEN"))
	}
	if resolvedAuthToken != "" {
		log.Printf("[web] Auth token configured: write endpoints require authentication")
	}

	// Security: refuse to start if bound to a non-localhost address without
	// an auth token.  Exposing the web UI on a public interface without any
	// authentication is a serious security risk.
	// Skip this check for Unix socket mode (socketPath is non-empty) since
	// Unix sockets are inherently local-only.
	if socketPath == "" && !isLocalhostAddr(bindAddr) && resolvedAuthToken == "" {
		return nil, fmt.Errorf("Refusing to start: SPROUT_BIND_ADDR=%s requires SPROUT_AUTH_TOKEN to be set.", bindAddr)
	}

	// Resolve symlinks on both roots so that path comparisons are consistent
	// (macOS /var → /private/var is the common case; without this, any path
	// that goes through filepath.EvalSymlinks will fail prefix checks).
	if evaled, err := filepath.EvalSymlinks(daemonRoot); err == nil {
		daemonRoot = evaled
	}
	if evaled, err := filepath.EvalSymlinks(workspaceRoot); err == nil {
		workspaceRoot = evaled
	}

	return &ReactWebServer{
		agent:             agent,
		eventBus:          eventBus,
		daemonRoot:        daemonRoot,
		workspaceRoot:     workspaceRoot,
		sshHostAlias:      strings.TrimSpace(configuration.GetEnvSimple("SSH_HOST_ALIAS")),
		sshSessionKey:     strings.TrimSpace(configuration.GetEnvSimple("SSH_SESSION_KEY")),
		sshLauncherURL:    strings.TrimSpace(configuration.GetEnvSimple("SSH_LAUNCHER_URL")),
		sshHomePath:       strings.TrimSpace(configuration.GetEnvSimple("SSH_HOME")),
		fileConsents:      newFileConsentManager(),
		fileWatcher:       newFileWatcher(eventBus),
		securityPromptMgr: securityPromptMgr,
		askUserMgr:        askUserMgr,
		clientContexts:    make(map[string]*webClientContext),
		chatSubscribers:   newChatSubscribersRegistry(),
		port:              port,
		bindAddr:          bindAddr,
		socketPath:        socketPath,
		upgrader: websocket.Upgrader{
			CheckOrigin: newCheckOriginFunc(bindAddr, normalizedAllowedOrigins),
		},
		terminalManager:          NewTerminalManager(workspaceRoot),
		startTime:                time.Now(),
		fixReviewJobs:            make(map[string]*gitFixReviewJob),
		sshSessions:              make(map[string]*sshWorkspaceSession),
		sshInFlight:              make(map[string]chan struct{}),
		sshLaunchStatuses:        make(map[string]*sshLaunchStatus),
		normalizedAllowedOrigins: normalizedAllowedOrigins,
		trustedUserHeader:        trustedUserHeader,
		serviceMode:              serviceMode,
		authToken:                resolvedAuthToken,
	}, nil
}

// isLocalhostAddr returns true if the given bind address is a safe local-only
// address that cannot be reached from external networks.
func isLocalhostAddr(addr string) bool {
	switch addr {
	case "", "127.0.0.1", "localhost", "[::1]", "::1":
		return true
	}
	return false
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

// getActiveQueryCount returns the current number of active queries.
func (ws *ReactWebServer) getActiveQueryCount() int {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	return ws.activeQueries
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
