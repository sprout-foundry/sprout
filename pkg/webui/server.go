// Package webui provides React web server with embedded assets
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/gorilla/websocket"
)

//go:embed static/*
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
	port            int
	server          *http.Server
	upgrader        websocket.Upgrader
	connections     sync.Map // map[*websocket.Conn]*ConnectionInfo
	terminalManager *TerminalManager
	isRunning       bool
	mutex           sync.RWMutex
	startTime       time.Time
	queryCount      int
}

// NewReactWebServer creates a new React web server
func NewReactWebServer(agent *agent.Agent, eventBus *events.EventBus, port int) *ReactWebServer {
	if port == 0 {
		port = 54321
	}

	return &ReactWebServer{
		agent:    agent,
		eventBus: eventBus,
		port:     port,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// Allow localhost connections
				origin := r.Header.Get("Origin")
				return origin == "http://localhost:"+strconv.Itoa(port) ||
					origin == "" // Allow same-origin and direct connections
			},
		},
		terminalManager: NewTerminalManager(),
		startTime:        time.Now(),
	}
}

// Start starts the web server
func (ws *ReactWebServer) Start(ctx context.Context) error {
	ws.mutex.Lock()
	if ws.isRunning {
		ws.mutex.Unlock()
		return fmt.Errorf("web server is already running")
	}
	ws.isRunning = true
	ws.mutex.Unlock()

	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/", ws.handleIndex)
	mux.HandleFunc("/ws", ws.handleWebSocket)
	mux.HandleFunc("/terminal", ws.handleTerminalWebSocket)
	mux.HandleFunc("/api/query", ws.handleAPIQuery)
	mux.HandleFunc("/api/stats", ws.handleAPIStats)
	mux.HandleFunc("/api/files", ws.handleAPIFiles)
	mux.HandleFunc("/api/browse", ws.handleAPIBrowse)
	mux.HandleFunc("/api/file", ws.handleAPIFile)
	mux.HandleFunc("/api/config", ws.handleAPIConfig)
	mux.HandleFunc("/api/terminal/history", ws.handleTerminalHistory)
	mux.HandleFunc("/api/git/status", ws.handleAPIGitStatus)
	mux.HandleFunc("/api/git/stage", ws.handleAPIGitStage)
	mux.HandleFunc("/api/git/unstage", ws.handleAPIGitUnstage)
	mux.HandleFunc("/api/git/discard", ws.handleAPIGitDiscard)
	mux.HandleFunc("/api/git/commit", ws.handleAPIGitCommit)
	mux.HandleFunc("/api/git/stage-all", ws.handleAPIGitStageAll)
	mux.HandleFunc("/api/git/unstage-all", ws.handleAPIGitUnstageAll)
	mux.HandleFunc("/api/terminal/sessions", ws.handleAPITerminalSessions)
	mux.HandleFunc("/static/", ws.handleStaticFiles)
	mux.HandleFunc("/sw.js", ws.handleServiceWorker)

	// Additional asset files
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		ws.handleStaticAsset(w, r, "manifest.json", "application/json")
	})
	mux.HandleFunc("/browserconfig.xml", func(w http.ResponseWriter, r *http.Request) {
		ws.handleStaticAsset(w, r, "browserconfig.xml", "application/xml")
	})
	mux.HandleFunc("/asset-manifest.json", func(w http.ResponseWriter, r *http.Request) {
		ws.handleStaticAsset(w, r, "asset-manifest.json", "application/json")
	})

	// Icon patterns - try .png first, then .svg, then .ico
	mux.HandleFunc("/icon-192.png", func(w http.ResponseWriter, r *http.Request) {
		ws.handleStaticAsset(w, r, "static/icon-192.png", "image/png", false, true)
	})
	mux.HandleFunc("/icon-512.png", func(w http.ResponseWriter, r *http.Request) {
		ws.handleStaticAsset(w, r, "static/icon-512.png", "image/png", false, true)
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		ws.handleStaticAsset(w, r, "favicon.ico", "image/x-icon", false, true)
	})

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
		Addr:    fmt.Sprintf(":%d", ws.port),
		Handler: mux,
	}

	// Start server in goroutine
	go func() {
		log.Printf("🌐 Web UI starting at http://localhost:%d", ws.port)
		if err := ws.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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

	return ws.server.Shutdown(ctx)
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

// countConnections returns the current number of WebSocket connections
func (ws *ReactWebServer) countConnections() int {
	count := 0
	ws.connections.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// handleStaticAsset handles individual asset files (manifest, icons, etc.)
func (ws *ReactWebServer) handleStaticAsset(w http.ResponseWriter, r *http.Request, embeddedPath string, contentType string, optional ...bool) {
	// Check if this file is optional (don't return 404)
	isOptional := len(optional) > 0 && optional[0]
	// Check if we should look in the static subdirectory
	lookInStatic := len(optional) > 1 && optional[1]

	// Try embedded filesystem first
	var data []byte
	var err error

	// Try the primary path
	data, err = staticFiles.ReadFile(embeddedPath)
	if err != nil && lookInStatic && !strings.HasPrefix(embeddedPath, "static/") {
		// Try with static/ prefix
		data, err = staticFiles.ReadFile("static/"+embeddedPath)
	}

	if err != nil {
		// Fallback to filesystem
		var fsPath string
		if lookInStatic {
			fsPath = "./pkg/webui/static/" + embeddedPath
		} else {
			fsPath = "./pkg/webui/static/" + embeddedPath
		}

		data, err = os.ReadFile(fsPath)
		if err != nil {
			if isOptional {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("404 Not Found"))
			return
		}
	}

	// Set content type
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	// Enable caching for assets
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(data)
}

// CheckPortAvailable checks if a port is available to bind to
func CheckPortAvailable(port int) bool {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false // Port is in use
	}
	listener.Close()
	return true // Port is free
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
