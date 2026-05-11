// Package webui provides React web server with embedded assets

package webui

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	clientContextCleanupInterval = 5 * time.Minute
	clientContextMaxIdle         = 30 * time.Minute
)

// Start starts the web server
func (ws *ReactWebServer) Start(ctx context.Context) error {
	mux := ws.setupRoutes(ctx)

	// Wrap mux with security headers middleware (applies to all responses)
	var handler http.Handler = securityHeadersMiddleware(mux)

	// Wrap with user ID extraction middleware for service mode
	if ws.serviceMode && ws.trustedUserHeader != "" {
		inner := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := ws.contextWithUserID(r.Context(), r)
			r = r.WithContext(ctx)
			inner.ServeHTTP(w, r)
		})
	}

	// Wrap with auth token middleware for write endpoints
	handler = authTokenMiddleware(ws.authToken)(handler)

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
