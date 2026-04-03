// Package webui provides React web server with embedded assets
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/providercatalog"
	"github.com/gorilla/websocket"
)

// Embed the entire static tree so root assets like logo-mark.svg and all hashed
// subdirectory assets are always included in go-install and test builds.
//
//go:embed static
var staticFiles embed.FS

// ConnectionInfo stores metadata about a WebSocket connection
type ConnectionInfo struct {
	SessionID   string    // Unique session ID for this connection
	ClientID    string    // WebUI client/window identifier
	Type        string    // "webui" or "terminal"
	ConnectedAt time.Time // When the connection was established
}

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
	server                          *http.Server
	listener                        net.Listener
	upgrader                        websocket.Upgrader
	connections                     sync.Map // map[*websocket.Conn]*ConnectionInfo
	terminalManager                 *TerminalManager
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
}

const (
	clientContextCleanupInterval = 5 * time.Minute
	clientContextMaxIdle         = 30 * time.Minute
)

// NewReactWebServer creates a new React web server
func NewReactWebServer(agent *agent.Agent, eventBus *events.EventBus, port int) *ReactWebServer {
	if port == 0 {
		port = DaemonPort
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

	return &ReactWebServer{
		agent:          agent,
		eventBus:       eventBus,
		daemonRoot:     daemonRoot,
		workspaceRoot:  workspaceRoot,
		sshHostAlias:   strings.TrimSpace(os.Getenv("LEDIT_SSH_HOST_ALIAS")),
		sshSessionKey:  strings.TrimSpace(os.Getenv("LEDIT_SSH_SESSION_KEY")),
		sshLauncherURL: strings.TrimSpace(os.Getenv("LEDIT_SSH_LAUNCHER_URL")),
		sshHomePath:    strings.TrimSpace(os.Getenv("LEDIT_SSH_HOME")),
		fileConsents:   newFileConsentManager(),
		clientContexts: make(map[string]*webClientContext),
		port:           port,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// Allow localhost connections only.
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true // Allow same-origin and direct connections
				}

				parsed, err := url.Parse(origin)
				if err != nil {
					return false
				}
				host := strings.ToLower(parsed.Hostname())
				return host == "localhost" || host == "127.0.0.1"
			},
		},
		terminalManager:   NewTerminalManager(workspaceRoot),
		startTime:         time.Now(),
		fixReviewJobs:     make(map[string]*gitFixReviewJob),
		sshSessions:       make(map[string]*sshWorkspaceSession),
		sshInFlight:       make(map[string]chan struct{}),
		sshLaunchStatuses: make(map[string]*sshLaunchStatus),
	}
}

// Start starts the web server
func (ws *ReactWebServer) Start(ctx context.Context) error {
	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/", ws.handleIndex)
	// /ssh/{encodedSessionKey}/ is a reverse proxy to the SSH tunnel backend.
	// Registered before /ws and /terminal so the ServeMux prefix match works.
	mux.HandleFunc("/ssh/", ws.handleSSHProxy)
	mux.HandleFunc("/ws", ws.handleWebSocket)
	mux.HandleFunc("/terminal", ws.handleTerminalWebSocket)
	mux.HandleFunc("/api/query", ws.handleAPIQuery)
	mux.HandleFunc("/api/query/steer", ws.handleAPIQuerySteer)
	mux.HandleFunc("/api/query/stop", ws.handleAPIQueryStop)
	mux.HandleFunc("/api/stats", ws.handleAPIStats)
	mux.HandleFunc("/api/providers", ws.handleAPIProviders)
	mux.HandleFunc("/api/onboarding/status", ws.handleAPIOnboardingStatus)
	mux.HandleFunc("/api/onboarding/complete", ws.handleAPIOnboardingComplete)
	mux.HandleFunc("/api/files", ws.handleAPIFiles)
	mux.HandleFunc("/api/create", ws.handleAPICreateFile)
	mux.HandleFunc("/api/delete", ws.handleAPIDeleteItem)
	mux.HandleFunc("/api/rename", ws.handleAPIRenameItem)
	mux.HandleFunc("/api/browse", ws.handleAPIBrowse)
	mux.HandleFunc("/api/file", ws.handleAPIFile)
	mux.HandleFunc("/api/file/consent", ws.handleAPIFileConsent)
	mux.HandleFunc("/api/config", ws.handleAPIConfig)
	mux.HandleFunc("/api/workspace", ws.handleAPIWorkspace)
	mux.HandleFunc("/api/workspace/browse", ws.handleAPIWorkspaceBrowse)
	// Settings API
	mux.HandleFunc("/api/settings", ws.handleAPISettings)
	mux.HandleFunc("/api/settings/mcp", ws.handleAPISettingsMCP)
	mux.HandleFunc("/api/settings/mcp/servers/", ws.handleAPISettingsMCPServers)
	mux.HandleFunc("/api/settings/providers", ws.handleAPISettingsProviders)
	mux.HandleFunc("/api/settings/providers/", ws.handleAPISettingsProviders)
	mux.HandleFunc("/api/settings/skills", ws.handleAPISettingsSkills)
	mux.HandleFunc("/api/settings/subagent-types", ws.handleAPISettingsSubagentTypes)
	mux.HandleFunc("/api/settings/subagent-types/", ws.handleAPISettingsSubagentTypes)
	// Hotkeys API
	mux.HandleFunc("/api/hotkeys", ws.handleAPIHotkeys)
	mux.HandleFunc("/api/hotkeys/validate", ws.handleAPIHotkeysValidate)
	mux.HandleFunc("/api/hotkeys/preset", ws.handleAPIHotkeysPreset)
	mux.HandleFunc("/api/terminal/history", ws.handleTerminalHistory)
	mux.HandleFunc("/api/git/status", ws.handleAPIGitStatus)
	mux.HandleFunc("/api/git/stage", ws.handleAPIGitStage)
	mux.HandleFunc("/api/git/unstage", ws.handleAPIGitUnstage)
	mux.HandleFunc("/api/git/discard", ws.handleAPIGitDiscard)
	mux.HandleFunc("/api/git/commit", ws.handleAPIGitCommit)
	mux.HandleFunc("/api/git/commit-message", ws.handleAPIGitCommitMessage)
	mux.HandleFunc("/api/git/deep-review", ws.handleAPIGitDeepReview)
	mux.HandleFunc("/api/git/deep-review/fix", ws.handleAPIGitDeepReviewFix)
	mux.HandleFunc("/api/git/deep-review/fix/start", ws.handleAPIGitDeepReviewFixStart)
	mux.HandleFunc("/api/git/deep-review/fix/status", ws.handleAPIGitDeepReviewFixStatus)
	mux.HandleFunc("/api/git/stage-all", ws.handleAPIGitStageAll)
	mux.HandleFunc("/api/git/unstage-all", ws.handleAPIGitUnstageAll)
	mux.HandleFunc("/api/git/diff", ws.handleAPIGitDiff)
	mux.HandleFunc("/api/git/branches", ws.handleAPIGitBranches)
	mux.HandleFunc("/api/git/checkout", ws.handleAPIGitCheckout)
	mux.HandleFunc("/api/git/revert", ws.handleAPIGitRevert)
	mux.HandleFunc("/api/git/branch/create", ws.handleAPIGitCreateBranch)
	mux.HandleFunc("/api/git/pull", ws.handleAPIGitPull)
	mux.HandleFunc("/api/git/push", ws.handleAPIGitPush)
	mux.HandleFunc("/api/git/log", ws.handleAPIGitLog)
	mux.HandleFunc("/api/git/commit/show", ws.handleAPIGitCommitShow)
	mux.HandleFunc("/api/git/commit/show/file", ws.handleAPIGitCommitFileDiff)
	mux.HandleFunc("/api/instances", ws.handleAPIInstances)
	mux.HandleFunc("/api/instances/select", ws.handleAPIInstanceSelect)
	mux.HandleFunc("/api/instances/ssh-hosts", ws.handleAPISSHHosts)
	mux.HandleFunc("/api/instances/ssh-open", ws.handleAPISSHOpen)
	mux.HandleFunc("/api/instances/ssh-launch-status", ws.handleAPISSHLaunchStatus)
	mux.HandleFunc("/api/instances/ssh-browse", ws.handleAPISSHBrowse)
	mux.HandleFunc("/api/instances/ssh-sessions", ws.handleAPISSHSessions)
	mux.HandleFunc("/api/instances/ssh-close", ws.handleAPISSHSessionDelete)
	mux.HandleFunc("/api/history/changelog", ws.handleAPIHistoryChangelog)
	mux.HandleFunc("/api/history/revision", ws.handleAPIHistoryRevision)
	mux.HandleFunc("/api/history/rollback", ws.handleAPIHistoryRollback)
	mux.HandleFunc("/api/history/changes", ws.handleAPIHistoryChanges)
	mux.HandleFunc("/api/terminal/sessions", ws.handleAPITerminalSessions)
	// Session API
	mux.HandleFunc("/api/sessions", ws.handleAPISessions)
	mux.HandleFunc("/api/sessions/restore", ws.handleAPIRestoreSession)
	// Chat sessions API (multi-chat support within a tab)
	mux.HandleFunc("/api/chat-sessions", ws.handleAPIChatSessions)
	mux.HandleFunc("/api/chat-sessions/create", ws.handleAPIChatSessionsCreate)
	mux.HandleFunc("/api/chat-sessions/delete", ws.handleAPIChatSessionsDelete)
	mux.HandleFunc("/api/chat-sessions/rename", ws.handleAPIChatSessionsRename)
	mux.HandleFunc("/api/chat-sessions/switch", ws.handleAPIChatSessionsSwitch)
	mux.HandleFunc("/api/chat-sessions/compact", ws.handleAPIChatSessionsCompact)
	// Search API
	mux.HandleFunc("/api/search", ws.handleAPIQuerySearch)
	mux.HandleFunc("/api/search/replace", ws.handleAPIQuerySearchReplace)
	mux.HandleFunc("/api/upload/image", ws.handleUploadImage)
	mux.HandleFunc("/static/", ws.handleStaticFiles)
	mux.HandleFunc("/sw.js", ws.handleServiceWorker)
	mux.HandleFunc("/manifest.json", ws.handleManifest)
	mux.HandleFunc("/browserconfig.xml", ws.handleBrowserConfig)
	mux.HandleFunc("/asset-manifest.json", ws.handleAssetManifest)
	mux.HandleFunc("/icon-192.png", ws.handleIcon192)
	mux.HandleFunc("/icon-512.png", ws.handleIcon512)
	mux.HandleFunc("/logo-mark.svg", ws.handleLogoMark)
	mux.HandleFunc("/favicon.ico", ws.handleFavicon)

	// Health check endpoint for connectivity verification
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"port":   ws.port,
			"uptime": time.Since(ws.startTime).String(),
		})
	})

	ws.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", ws.port),
		Handler: mux,
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
		ws.server.Addr = fmt.Sprintf("127.0.0.1:%d", actualPort)
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
		log.Printf("[web] Web UI starting at http://localhost:%d", ws.port)
		if err := ws.server.Serve(listener); err != nil && !isExpectedServerCloseError(err) {
			log.Printf("Web server error: %v", err)
		}
	}()

	go ws.startClientContextCleanupWorker(ctx, clientContextCleanupInterval, clientContextMaxIdle)

	// Start terminal session cleanup worker (every 5 minutes, timeout 30 minutes)
	ws.terminalManager.StartCleanupWorker(ctx, 5*time.Minute, 30*time.Minute)

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
		return err
	}
	return nil
}

func isExpectedServerCloseError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return true
	}
	// Go stdlib may wrap this in plain text depending on call path.
	return strings.Contains(strings.ToLower(err.Error()), "use of closed network connection")
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

// CheckPortAvailable checks if a port is available to bind to
func CheckPortAvailable(port int) bool {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false // Port is in use
	}
	listener.Close()
	return true // Port is free
}

// expandHomeVar expands only $HOME and ${HOME} references in a path string.
// This is more restrictive than os.ExpandEnv (which expands all env vars)
// and avoids surprising behavior from arbitrary environment variable expansion.
func expandHomeVar(path string) string {
	home := os.Getenv("HOME")
	if home == "" {
		return path
	}
	path = strings.ReplaceAll(path, "${HOME}", home)
	path = strings.ReplaceAll(path, "$HOME", home)
	return path
}

func filepathAbsEval(path string) (string, error) {
	// Expand $HOME / ${HOME} and tilde in the path.
	expanded := expandHomeVar(path)
	if strings.HasPrefix(expanded, "~/") || expanded == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if expanded == "~" {
			expanded = home
		} else {
			expanded = filepath.Join(home, expanded[2:])
		}
	}

	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			// Fallback to unresolved absolute path. This is safe because callers
			// (e.g., SetWorkspaceRoot via isWithinWorkspace) validate the path
			// is within the workspace before it's used.
			return abs, nil
		}
		return "", err
	}
	return resolved, nil
}

// FindAvailablePort finds an available port starting from a base port
func FindAvailablePort(basePort int) (int, error) {
	port := basePort
	for port < basePort+100 {
		if CheckPortAvailable(port) {
			return port, nil
		}
		port++
	}
	return 0, fmt.Errorf("no available port found in range %d-%d", basePort, basePort+99)
}
