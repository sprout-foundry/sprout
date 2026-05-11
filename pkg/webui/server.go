// Package webui provides React web server with embedded assets
package webui

import (
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	lspproxy "github.com/sprout-foundry/sprout/pkg/lsp/proxy"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
	"github.com/sprout-foundry/sprout/pkg/security"
	agenttools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/gorilla/websocket"
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
	port                            int
	bindAddr                        string
	server                          *http.Server
	listener                        net.Listener
	upgrader                        websocket.Upgrader
	connections                     sync.Map // map[*websocket.Conn]*ConnectionInfo
	fileWatcher                     *fileWatcher
	terminalManager                 *TerminalManager
	securityPromptMgr               *security.ApprovalManager
	isRunning                       bool
	mutex                           sync.RWMutex
	startTime                       time.Time
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
	normalizedAllowedOrigins        []string // Pre-normalized from SPROUT_ALLOWED_ORIGINS env var
	trustedUserHeader               string   // Header name for user ID extraction in service mode
	serviceMode                     bool     // true when running as a managed service (SPROUT_SERVICE=1)
	authToken                       string   // Auth token for write endpoint protection (SPROUT_AUTH_TOKEN)
}

// NewReactWebServer creates a new React web server
func NewReactWebServer(agent *agent.Agent, eventBus *events.EventBus, port int, bindAddr string) *ReactWebServer {
	if port == 0 {
		port = DaemonPort
	}
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}

	workspaceRoot, err := os.Getwd()
	if err != nil {
		workspaceRoot = "."
	}
	workspaceRoot, err = filepathAbsEval(workspaceRoot)
	if err != nil {
		workspaceRoot = "."
	}

	// daemonRoot is the user's home directory — this scopes daemon-level
	// storage (sessions, SSH tunnels, config) to the user rather than a
	// specific project workspace.
	daemonRoot, err := os.UserHomeDir()
	if err != nil {
		daemonRoot = workspaceRoot
	}

	providercatalog.RefreshFromRemoteAsync("")

	securityPromptMgr := security.NewApprovalManager()
	security.SetGlobalApprovalManager(securityPromptMgr)

	askUserMgr := agenttools.NewAskUserManager()
	agenttools.SetGlobalAskUserManager(askUserMgr)

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

	// Parse service mode and trusted user header
	serviceMode := configuration.GetEnvSimple("SERVICE") == "1"
	trustedUserHeader := strings.TrimSpace(configuration.GetEnvSimple("TRUSTED_USER_HEADER"))
	if serviceMode {
		if trustedUserHeader != "" {
			log.Printf("[web] Trusted user header: %s (service mode)", trustedUserHeader)
		} else {
			log.Printf("[web] Service mode enabled but no trusted user header configured")
		}
	}

	// Parse auth token for write endpoint protection
	authToken := strings.TrimSpace(configuration.GetEnvSimple("AUTH_TOKEN"))
	if authToken != "" {
		log.Printf("[web] Auth token configured: write endpoints require authentication")
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
		clientContexts:    make(map[string]*webClientContext),
		port:              port,
		bindAddr:          bindAddr,
		upgrader: websocket.Upgrader{
			CheckOrigin: newCheckOriginFunc(bindAddr, normalizedAllowedOrigins),
		},
		terminalManager:   NewTerminalManager(workspaceRoot),
		startTime:         time.Now(),
		fixReviewJobs:     make(map[string]*gitFixReviewJob),
		sshSessions:       make(map[string]*sshWorkspaceSession),
		sshInFlight:       make(map[string]chan struct{}),
		sshLaunchStatuses: make(map[string]*sshLaunchStatus),
		normalizedAllowedOrigins: normalizedAllowedOrigins,
		trustedUserHeader:        trustedUserHeader,
		serviceMode:              serviceMode,
		authToken:                authToken,
	}
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

