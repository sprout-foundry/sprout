package proxy

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Bridge handles a single WebSocket client connection, bridging it to an LSP process.
// It handles the JSON-RPC request routing:
// - LSP initialize/initialized flow is handled transparently
// - WebSocket sends raw JSON → bridge frames it and writes to LSP process stdin
// - LSP process stdout → bridge deframes and sends raw JSON to WebSocket
// - When WebSocket disconnects, the bridge unsubscribes from the process
type Bridge struct {
	wsConn *websocket.Conn

	lspProcess  *LSPProcess
	lspCh       <-chan string
	unsubscribe func()

	doneCh    chan struct{}
	doneOnce  sync.Once // guards close(doneCh) — both goroutines race to signal it
	closeOnce sync.Once // guards Close() — both shutdown paths invoke it
}

// NewBridge creates a new bridge for the given WebSocket connection and LSP process.
func NewBridge(wsConn *websocket.Conn, process *LSPProcess) *Bridge {
	return &Bridge{
		wsConn:     wsConn,
		lspProcess: process,
		doneCh:     make(chan struct{}),
	}
}

// Run starts the bridge. It should:
// 1. Subscribe to LSP process messages
// 2. Read from WebSocket in a loop, writing to LSP process
// 3. Read from process subscriber channel, writing to WebSocket
// 4. Handle graceful shutdown when either side closes
// 5. Use two goroutines (ws→lsp and lsp→ws)
func (b *Bridge) Run(ctx context.Context) error {
	// Subscribe to LSP process messages
	ch, unsubscribe, err := b.lspProcess.Subscribe()
	if err != nil {
		return err
	}
	b.lspCh = ch
	b.unsubscribe = unsubscribe

	// Set up WebSocket read deadline (for heartbeat)
	b.wsConn.SetReadDeadline(time.Now().Add(60 * time.Second))
	b.wsConn.SetWriteDeadline(time.Now().Add(10 * time.Second))

	// Use context cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start two goroutines for bidirectional communication
	go b.runWSToLSP(ctx)
	go b.runLSPToWS(ctx)

	// Wait for either goroutine to finish
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.doneCh:
		return nil
	}
}

// runWSToLSP reads from WebSocket and writes to LSP process.
func (b *Bridge) runWSToLSP(ctx context.Context) {
	defer func() {
		// Signal Run() that we're done. Both runWSToLSP and runLSPToWS
		// may race to be the first exiter — sync.Once makes close(doneCh)
		// safe to call from either path.
		b.doneOnce.Do(func() { close(b.doneCh) })
		b.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Set read deadline for heartbeat
		b.wsConn.SetReadDeadline(time.Now().Add(60 * time.Second))

		// Read raw JSON-RPC message from WebSocket
		msgType, reader, err := b.wsConn.NextReader()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) ||
				websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("LSP bridge: WebSocket closed: %v", err)
			} else {
				log.Printf("LSP bridge: WebSocket read error: %v", err)
			}
			return
		}

		// Parse the message to check if it's text
		if msgType != websocket.TextMessage {
			continue
		}

		// Read the message
		var msg json.RawMessage
		if err := json.NewDecoder(reader).Decode(&msg); err != nil {
			log.Printf("LSP bridge: Failed to decode WebSocket message: %v", err)
			continue
		}

		// Write to LSP process (with Content-Length framing)
		if err := b.lspProcess.Send(string(msg)); err != nil {
			log.Printf("LSP bridge: Failed to send to LSP: %v", err)
			return
		}
	}
}

// runLSPToWS reads from LSP process and writes to WebSocket.
func (b *Bridge) runLSPToWS(ctx context.Context) {
	// Mirror runWSToLSP: signal completion + invoke Close on exit so
	// that whichever direction tears down first releases the conn.
	defer func() {
		b.doneOnce.Do(func() { close(b.doneCh) })
		b.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case msg, ok := <-b.lspCh:
			if !ok {
				// Channel closed — LSP process exited. Close() (deferred)
				// will close the WS conn; no direct field access here so
				// runWSToLSP's concurrent Close() can't race us.
				return
			}

			// Set write deadline
			b.wsConn.SetWriteDeadline(time.Now().Add(10 * time.Second))

			// Write raw JSON-RPC message to WebSocket
			if err := b.wsConn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
				log.Printf("LSP bridge: Failed to write to WebSocket: %v", err)
				return
			}
		}
	}
}

// Close cleans up the bridge. Safe to call multiple times and from
// multiple goroutines — both shutdown paths (runWSToLSP defer + the
// BridgeHandler defer) invoke it. The previous version nilled wsConn,
// which raced with the still-running runLSPToWS reading wsConn.
func (b *Bridge) Close() {
	b.closeOnce.Do(func() {
		if b.unsubscribe != nil {
			b.unsubscribe()
		}
		if b.wsConn != nil {
			b.wsConn.Close()
		}
	})
}

// BridgeHandler creates a http.HandlerFunc that handles LSP WebSocket connections.
// It upgrades the WebSocket connection and bridges it to the LSP process from the manager.
// The upgrader parameter should have a proper CheckOrigin function configured by the caller.
func BridgeHandler(manager *Manager, upgrader websocket.Upgrader, workspaceRoot string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse query parameters
		languageID := r.URL.Query().Get("language")
		if languageID == "" {
			http.Error(w, "language parameter is required", http.StatusBadRequest)
			return
		}

		workspacePath := r.URL.Query().Get("workspace")
		if workspacePath == "" {
			http.Error(w, "workspace parameter is required", http.StatusBadRequest)
			return
		}

		// Resolve workspace path
		_, err := filepath.Abs(workspacePath)
		if err != nil {
			http.Error(w, "workspace not allowed", http.StatusForbidden)
			return
		}

		// Normalize language ID
		languageID = NormalizeLanguageID(languageID)

		// Get or create the LSP process
		process, release, err := manager.GetOrCreate(workspacePath, languageID)
		if err != nil {
			log.Printf("LSP bridge: Failed to get process: %v", err)
			http.Error(w, "Failed to start language server: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Upgrade WebSocket connection using the provided upgrader
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("LSP bridge: WebSocket upgrade error: %v", err)
			release()
			return
		}

		// Create bridge
		bridge := NewBridge(wsConn, process)
		defer func() {
			bridge.Close()
			release()
		}()

		// Run the bridge
		if err := bridge.Run(r.Context()); err != nil {
			log.Printf("LSP bridge: Run error: %v", err)
		}
	}
}
