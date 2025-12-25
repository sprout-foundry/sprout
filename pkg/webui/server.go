// Package webui provides React web server with embedded assets
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
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
	agent            *agent.Agent
	eventBus         *events.EventBus
	port             int
	server           *http.Server
	upgrader         websocket.Upgrader
	connections      sync.Map // map[*websocket.Conn]*ConnectionInfo
	terminalManager  *TerminalManager
	isRunning        bool
	mutex            sync.RWMutex
	startTime        time.Time
	queryCount       int
	instanceRegistry *InstanceRegistry
	instanceID       string
}

// NewReactWebServer creates a new React web server
func NewReactWebServer(agent *agent.Agent, eventBus *events.EventBus, port int) *ReactWebServer {
	if port == 0 {
		port = 54321
	}

	// Initialize instance registry
	configDir := getConfigDir()
	instanceRegistry, err := NewInstanceRegistry(configDir)
	if err != nil {
		log.Printf("Warning: failed to initialize instance registry: %v", err)
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
		terminalManager:  NewTerminalManager(),
		instanceRegistry: instanceRegistry,
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

	// Register this instance
	if ws.instanceRegistry != nil {
		pid := os.Getpid()
		wd, _ := os.Getwd()
		if id, err := ws.instanceRegistry.RegisterInstance(ws.port, pid, wd); err != nil {
			log.Printf("Warning: failed to register instance: %v", err)
		} else {
			ws.instanceID = id
			log.Printf("📝 Registered instance: %s (port: %d)", id, ws.port)
		}

		// Start periodic ping
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := ws.instanceRegistry.Ping(); err != nil {
						log.Printf("Warning: failed to ping instance registry: %v", err)
					}
				}
			}
		}()
	}

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
	mux.HandleFunc("/api/discover", ws.handleAPIDiscover)
	mux.HandleFunc("/api/terminal/sessions", ws.handleAPITerminalSessions)

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

	// Unregister instance
	if ws.instanceRegistry != nil {
		if err := ws.instanceRegistry.UnregisterInstance(); err != nil {
			log.Printf("Warning: failed to unregister instance: %v", err)
		}
	}

	return ws.server.Shutdown(ctx)
}

// getConfigDir returns the config directory path
func getConfigDir() string {
	// Try LEDIT_CONFIG env var first
	if dir := os.Getenv("LEDIT_CONFIG"); dir != "" {
		return dir
	}

	// Try XDG_CONFIG_HOME on Unix-like systems
	if runtime.GOOS != "windows" {
		if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
			return filepath.Join(configHome, "ledit")
		}
	}

	// Use user home directory
	homeDir := ""
	if u, err := user.Current(); err == nil {
		homeDir = u.HomeDir
	}

	if homeDir == "" {
		homeDir = os.Getenv("HOME")
	}

	if homeDir == "" {
		// Fallback for Android or special environments
		return "/data/data/com.termux/files/home/.ledit"
	}

	// Use .ledit in home directory
	return filepath.Join(homeDir, ".ledit")
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
