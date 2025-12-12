// Package webui provides React web server with embedded assets
package webui

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/gorilla/websocket"
)

//go:embed static/*
var staticFiles embed.FS

// ReactWebServer provides the React web UI
type ReactWebServer struct {
	agent            *agent.Agent
	eventBus         *events.EventBus
	port             int
	server           *http.Server
	upgrader         websocket.Upgrader
	connections      sync.Map // map[*websocket.Conn]bool
	terminalManager  *TerminalManager
	isRunning        bool
	mutex            sync.RWMutex
	startTime        time.Time
	queryCount       int
}

// NewReactWebServer creates a new React web server
func NewReactWebServer(agent *agent.Agent, eventBus *events.EventBus, port int) *ReactWebServer {
	if port == 0 {
		port = 54321
	}

	return &ReactWebServer{
		agent:           agent,
		eventBus:        eventBus,
		port:            port,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// Allow localhost connections
				origin := r.Header.Get("Origin")
				return origin == "http://localhost:"+strconv.Itoa(port) ||
					origin == "" // Allow same-origin and direct connections
			},
		},
		terminalManager: NewTerminalManager(),
		startTime:       time.Now(),
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

	// Serve Service Worker with proper MIME type
	mux.HandleFunc("/sw.js", ws.handleServiceWorker)

	// Serve static files (React build assets) with proper MIME types
	mux.HandleFunc("/static/", ws.handleStaticFiles)

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