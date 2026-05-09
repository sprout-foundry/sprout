// Package webui provides React web server with embedded assets
package webui

import (
	"context"
	"fmt"
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
}

const (
	clientContextCleanupInterval = 5 * time.Minute
	clientContextMaxIdle         = 30 * time.Minute
)

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
			CheckOrigin: func(r *http.Request) bool {
				// Allow localhost connections (IPv4 and IPv6).
				// When binding to 0.0.0.0 (cloud/service mode),
				// accept any origin since the service is explicitly
				// exposed. The SPROUT_ALLOWED_ORIGINS env var
				// provides finer-grained control for specific origins.
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true // Allow same-origin and direct connections
				}

				parsed, err := url.Parse(origin)
				if err != nil {
					return false
				}
				host := strings.ToLower(parsed.Hostname())
				if host == "localhost" {
					return true
				}
				if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
					return true
				}

				// Check against SPROUT_ALLOWED_ORIGINS allowlist.
				// Origins were pre-normalized at startup; only a simple
				// string comparison is needed per request.
				if len(normalizedAllowedOrigins) > 0 {
					normalizedIncoming := normalizeOriginForCompare(parsed)
					for _, allowed := range normalizedAllowedOrigins {
						if normalizedIncoming == allowed {
							return true
						}
					}
				}

				// When binding to all interfaces, accept any origin.
				if bindAddr == "0.0.0.0" || bindAddr == "::" {
					return true
				}
				return false
			},
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
	}
}

// Start starts the web server
func (ws *ReactWebServer) Start(ctx context.Context) error {
	mux := ws.setupRoutes(ctx)

	// Wrap mux with user ID extraction middleware
	var handler http.Handler = mux
	if ws.serviceMode && ws.trustedUserHeader != "" {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := ws.contextWithUserID(r.Context(), r)
			r = r.WithContext(ctx)
			mux.ServeHTTP(w, r)
		})
	}

	ws.server = &http.Server{
		Addr:    formatListenAddr(ws.bindAddr, ws.port),
		Handler: handler,
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", ws.server.Addr)
	if err != nil {
		return fmt.Errorf("failed to bind web server on %s: %w", ws.server.Addr, err)
	}

	// When the configured port is 0, the OS assigns a random free port.
	// Capture the actual port from the listener so GetPort() and logging
	// report the real value.
	if ws.port == 0 {
		actualPort := listener.Addr().(*net.TCPAddr).Port
		ws.port = actualPort
		ws.server.Addr = formatListenAddr(ws.bindAddr, actualPort)
	}

	ws.mutex.Lock()
	if ws.isRunning {
		ws.mutex.Unlock()
		listener.Close()
		return fmt.Errorf("web server is already running")
	}
	ws.listener = listener
	ws.isRunning = true
	ws.mutex.Unlock()

	// Start server in goroutine
	go func() {
		log.Printf("[web] Web UI starting at http://%s:%d", DisplayAddr(ws.bindAddr), ws.port)
		if err := ws.server.Serve(listener); err != nil && !isExpectedServerCloseError(err) {
			log.Printf("Web server error: %v", err)
		}
	}()

	go ws.startClientContextCleanupWorker(ctx, clientContextCleanupInterval, clientContextMaxIdle)

	// Start terminal session cleanup worker (every 5 minutes, timeout 30 minutes, background timeout 2 hours)
	ws.terminalManager.StartCleanupWorker(ctx, 5*time.Minute, 30*time.Minute, 2*time.Hour)

	// Evict idle language server sessions (gopls, TypeScript worker) every 5 minutes.
	startSemanticEviction(ctx)

	// Start file watcher for detecting external changes to open files.
	ws.fileWatcher.start(ctx)

	// Wait for context cancellation
	go func() {
		<-ctx.Done()
		ws.Shutdown()
	}()

	return nil
}

// Shutdown gracefully shuts down the web server
func (ws *ReactWebServer) Shutdown() error {
	ws.mutex.Lock()
	if !ws.isRunning {
		ws.mutex.Unlock()
		return nil
	}
	ws.isRunning = false
	listener := ws.listener
	ws.listener = nil
	ws.mutex.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Stop the file watcher.
	ws.fileWatcher.stop()

	// Clean up LSP manager (closes all language server processes)
	if ws.lspManager != nil {
		ws.lspManager.Cleanup()
	}

	// Close all WebSocket connections
	ws.connections.Range(func(conn, value interface{}) bool {
		if wsConn, ok := conn.(*websocket.Conn); ok {
			wsConn.Close()
		}
		return true
	})

	ws.shutdownSSHSessions()

	if listener != nil {
		_ = listener.Close()
	}

	if err := ws.server.Shutdown(ctx); err != nil && !isExpectedServerCloseError(err) {
		return fmt.Errorf("shutdown web server: %w", err)
	}
	return nil
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

