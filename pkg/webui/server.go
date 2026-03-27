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
	Type        string    // "webui" or "terminal"
	ConnectedAt time.Time // When the connection was established
}

// ReactWebServer provides the React web UI
type ReactWebServer struct {
	agent           *agent.Agent
	eventBus        *events.EventBus
	daemonRoot      string
	workspaceRoot   string
	fileConsents    *fileConsentManager
	port            int
	server          *http.Server
	listener        net.Listener
	upgrader        websocket.Upgrader
	connections     sync.Map // map[*websocket.Conn]*ConnectionInfo
	terminalManager *TerminalManager
	isRunning       bool
	mutex           sync.RWMutex
	startTime       time.Time
	queryCount      int
	activeQueries   int
	fixReviewJobs   map[string]*gitFixReviewJob
	fixReviewMu     sync.RWMutex
}

// NewReactWebServer creates a new React web server
func NewReactWebServer(agent *agent.Agent, eventBus *events.EventBus, port int) *ReactWebServer {
	if port == 0 {
		port = 54421
	}

	workspaceRoot, err := os.Getwd()
	if err != nil {
		workspaceRoot = "."
	}
	workspaceRoot, err = filepathAbsEval(workspaceRoot)
	if err != nil {
		workspaceRoot = "."
	}

	return &ReactWebServer{
		agent:         agent,
		eventBus:      eventBus,
		daemonRoot:    workspaceRoot,
		workspaceRoot: workspaceRoot,
		fileConsents:  newFileConsentManager(),
		port:          port,
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
		terminalManager: NewTerminalManager(workspaceRoot),
		startTime:       time.Now(),
		fixReviewJobs:   make(map[string]*gitFixReviewJob),
	}
}

// Start starts the web server
func (ws *ReactWebServer) Start(ctx context.Context) error {
	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/", ws.handleIndex)
	mux.HandleFunc("/ws", ws.handleWebSocket)
	mux.HandleFunc("/terminal", ws.handleTerminalWebSocket)
	mux.HandleFunc("/api/query", ws.handleAPIQuery)
	mux.HandleFunc("/api/query/steer", ws.handleAPIQuerySteer)
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
	mux.HandleFunc("/api/git/branch/create", ws.handleAPIGitCreateBranch)
	mux.HandleFunc("/api/git/pull", ws.handleAPIGitPull)
	mux.HandleFunc("/api/git/push", ws.handleAPIGitPush)
	mux.HandleFunc("/api/instances", ws.handleAPIInstances)
	mux.HandleFunc("/api/instances/select", ws.handleAPIInstanceSelect)
	mux.HandleFunc("/api/history/changelog", ws.handleAPIHistoryChangelog)
	mux.HandleFunc("/api/history/revision", ws.handleAPIHistoryRevision)
	mux.HandleFunc("/api/history/rollback", ws.handleAPIHistoryRollback)
	mux.HandleFunc("/api/history/changes", ws.handleAPIHistoryChanges)
	mux.HandleFunc("/api/terminal/sessions", ws.handleAPITerminalSessions)
	// Session API
	mux.HandleFunc("/api/sessions", ws.handleAPISessions)
	mux.HandleFunc("/api/sessions/restore", ws.handleAPIRestoreSession)
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

// SetWorkspaceRoot updates the active workspace root and resets terminal state.
func (ws *ReactWebServer) SetWorkspaceRoot(path string) (string, error) {
	workspaceRoot, err := filepathAbsEval(path)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}

	info, err := os.Stat(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("stat workspace root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace root must be a directory")
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	if !isWithinWorkspace(workspaceRoot, ws.daemonRoot) && workspaceRoot != ws.daemonRoot {
		return "", fmt.Errorf("workspace root must stay within daemon root %s", ws.daemonRoot)
	}

	if ws.terminalManager != nil {
		if err := ws.terminalManager.CloseAllSessions(); err != nil {
			return "", fmt.Errorf("close terminal sessions: %w", err)
		}
	}

	ws.workspaceRoot = workspaceRoot
	ws.terminalManager = NewTerminalManager(workspaceRoot)

	return workspaceRoot, nil
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

func filepathAbsEval(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return abs, nil
		}
		return "", err
	}
	return resolved, nil
}

// FindAvailablePort finds an available port starting from a base port
func FindAvailablePort(basePort int) int {
	port := basePort
	for port < basePort+100 {
		if CheckPortAvailable(port) {
			return port
		}
		port++
	}
	return basePort + 100 // Return last attempt even if not available
}
