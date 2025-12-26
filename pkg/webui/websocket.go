package webui

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// SafeConn wraps a WebSocket connection with write mutex and panic recovery
type SafeConn struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
	closed  bool
}

// NewSafeConn creates a new safe connection wrapper
func NewSafeConn(conn *websocket.Conn) *SafeConn {
	return &SafeConn{
		conn:   conn,
		closed: false,
	}
}

// WriteJSON safely writes JSON to the WebSocket connection
func (sc *SafeConn) WriteJSON(v interface{}) error {
	if sc.closed {
		return nil // Silently ignore writes to closed connections
	}

	sc.writeMu.Lock()
	defer sc.writeMu.Unlock()

	if sc.closed {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("WebSocket write panic recovered: %v", r)
			sc.closed = true
		}
	}()

	return sc.conn.WriteJSON(v)
}

// Close closes the underlying connection
func (sc *SafeConn) Close() error {
	sc.writeMu.Lock()
	sc.closed = true
	sc.writeMu.Unlock()
	return sc.conn.Close()
}

// Underlying returns the underlying websocket.Conn for read operations (still need to be careful)
func (sc *SafeConn) Underlying() *websocket.Conn {
	return sc.conn
}

// handleWebSocket handles WebSocket connections for real-time events
func (ws *ReactWebServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("WebSocket handler panic: %v", r)
		}
	}()

	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// Wrap connection in SafeConn to prevent concurrent write panics
	safeConn := NewSafeConn(conn)
	defer safeConn.Close()

	// Generate unique session ID for this connection
	sessionID := fmt.Sprintf("ws_%d", time.Now().UnixNano())

	// Store the underlying connection with metadata
	ws.connections.Store(conn, &ConnectionInfo{
		SessionID:   sessionID,
		Type:        "webui",
		ConnectedAt: time.Now(),
	})
	defer ws.connections.Delete(conn)

	log.Printf("WebSocket client connected: %s", sessionID)

	// Send initial connection status
	safeConn.WriteJSON(map[string]interface{}{
		"type": "connection_status",
		"data": map[string]interface{}{"connected": true, "session_id": sessionID},
	})

	// Set up close handler to send disconnect status
	conn.SetCloseHandler(func(code int, text string) error {
		log.Printf("WebSocket %s closing with code %d: %s", sessionID, code, text)
		return nil
	})

	// Subscribe to events with unique session ID to support multiple clients
	eventCh := ws.eventBus.Subscribe(sessionID)
	defer ws.eventBus.Unsubscribe(sessionID)

	// Use separate goroutines for reading and writing
	// This is the standard pattern for bidirectional WebSocket communication
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Read goroutine - handles incoming messages
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer func() {
			if r := recover(); r != nil {
				log.Printf("WebSocket read goroutine panic recovered: %v", r)
			}
		}()

		conn.SetReadLimit(512 * 1024) // 512KB max message size
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Set read deadline for heartbeat (60 seconds)
				conn.SetReadDeadline(time.Now().Add(60 * time.Second))

				var msg map[string]interface{}
				if err := conn.ReadJSON(&msg); err != nil {
					if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) ||
						websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						log.Printf("WebSocket %s closed: %v", sessionID, err)
					} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						// Heartbeat timeout, send ping
						if err := safeConn.WriteJSON(map[string]interface{}{
							"type": "ping",
							"data": map[string]interface{}{"timestamp": time.Now().Unix()},
						}); err != nil {
							log.Printf("WebSocket %s ping failed: %v", sessionID, err)
							return
						}
						continue
					} else {
						log.Printf("WebSocket %s read error: %v", sessionID, err)
					}
					return
				}

				// Handle incoming WebSocket messages
				ws.handleWebSocketMessage(safeConn, msg)
			}
		}
	}()

	// Write loop - handles outgoing events
	for {
		select {
		case <-ctx.Done():
			log.Printf("WebSocket %s context cancelled", sessionID)
			return

		case event := <-eventCh:
			if err := safeConn.WriteJSON(event); err != nil {
				log.Printf("WebSocket %s write error: %v", sessionID, err)
				return
			}

		case <-readDone:
			// Read goroutine has exited
			return
		}
	}
}

// handleWebSocketMessage processes incoming WebSocket messages
func (ws *ReactWebServer) handleWebSocketMessage(safeConn *SafeConn, msg map[string]interface{}) {
	msgType, ok := msg["type"].(string)
	if !ok {
		return
	}

	switch msgType {
	case "ping":
		// Respond to ping with pong
		safeConn.WriteJSON(map[string]interface{}{
			"type": "pong",
			"data": map[string]interface{}{"timestamp": time.Now().Unix()},
		})

	case "subscribe":
		// Handle subscription requests for specific event types
		if data, ok := msg["data"].(map[string]interface{}); ok {
			if eventTypes, ok := data["events"].([]interface{}); ok {
				// This could be used to filter events at the source level
				log.Printf("WebSocket client subscribed to events: %v", eventTypes)
			}
		}

	case "request_stats":
		// Send current stats immediately
		go func() {
			stats := ws.gatherStats()
			safeConn.WriteJSON(map[string]interface{}{
				"type": "stats_update",
				"data": stats,
			})
		}()
	}
}

// handleTerminalWebSocket handles terminal WebSocket connections
func (ws *ReactWebServer) handleTerminalWebSocket(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Terminal WebSocket handler panic: %v", r)
		}
	}()

	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Terminal WebSocket upgrade error: %v", err)
		return
	}

	// Wrap connection in SafeConn to prevent concurrent write panics
	safeConn := NewSafeConn(conn)
	defer safeConn.Close()

	// Generate session ID
	sessionID := fmt.Sprintf("terminal_%d", time.Now().UnixNano())

	log.Printf("Terminal WebSocket connection starting: %s", sessionID)

	// Create terminal session
	session, err := ws.terminalManager.CreateSession(sessionID)
	if err != nil {
		log.Printf("Failed to create terminal session: %v", err)
		safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Failed to create terminal session"},
		})
		return
	}

	// Store the underlying connection with metadata
	ws.connections.Store(conn, &ConnectionInfo{
		SessionID:   sessionID,
		Type:        "terminal",
		ConnectedAt: time.Now(),
	})
	defer ws.connections.Delete(conn)

	// Send session created message
	safeConn.WriteJSON(map[string]interface{}{
		"type": "session_created",
		"data": map[string]string{"session_id": sessionID},
	})

	// Use context for proper cleanup coordination
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Write done channel for output sync
	writeDone := make(chan struct{})

	// Output writer goroutine - reads from terminal session and writes to WebSocket
	go func() {
		defer close(writeDone)
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Terminal output writer panic: %v", r)
			}
		}()

		if session.OutputCh != nil {
			for {
				select {
				case <-ctx.Done():
					log.Printf("Terminal %s output writer stopped (context cancelled)", sessionID)
					return

				case output, ok := <-session.OutputCh:
					if !ok {
						// Output channel closed
						log.Printf("Terminal %s output channel closed", sessionID)
						return
					}

					if err := safeConn.WriteJSON(map[string]interface{}{
						"type": "output",
						"data": map[string]string{
							"session_id": sessionID,
							"output":     string(output),
						},
					}); err != nil {
						log.Printf("Terminal %s WebSocket write error: %v", sessionID, err)
						cancel() // Signal other goroutines to stop
						return
					}
				}
			}
		}
	}()

	// Read loop - handles incoming messages from WebSocket
	conn.SetReadLimit(512 * 1024) // 512KB max message size
	for {
		select {
		case <-ctx.Done():
			log.Printf("Terminal %s read loop stopped (context cancelled)", sessionID)
			return

		case <-writeDone:
			// Output writer stopped
			log.Printf("Terminal %s read loop stopped (writer finished)", sessionID)
			return

		default:
			// Set read deadline for heartbeat
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))

			var msg map[string]interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) ||
					websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("Terminal %s WebSocket closed: %v", sessionID, err)
				} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// Heartbeat timeout, continue
					continue
				} else {
					log.Printf("Terminal %s read error: %v", sessionID, err)
				}
				return
			}

			// Process message
			msgType, ok := msg["type"].(string)
			if !ok {
				continue
			}

			switch msgType {
			case "input":
				data, ok := msg["data"].(map[string]interface{})
				if !ok {
					continue
				}

				input, ok := data["input"].(string)
				if !ok {
					continue
				}

				if err := ws.terminalManager.ExecuteCommand(sessionID, input); err != nil {
					safeConn.WriteJSON(map[string]interface{}{
						"type": "error",
						"data": map[string]string{
							"session_id": sessionID,
							"message":    err.Error(),
						},
					})
				}

			case "resize":
				if data, ok := msg["data"].(map[string]interface{}); ok {
					if rows, ok := data["rows"].(float64); ok {
						if cols, ok := data["cols"].(float64); ok {
							err := ws.terminalManager.ResizeTerminal(sessionID, uint16(rows), uint16(cols))
							if err != nil {
								log.Printf("Failed to resize terminal %s: %v", sessionID, err)
							} else {
								log.Printf("Terminal %s resized to %dx%d", sessionID, int(rows), int(cols))
							}
						}
					}
				}

				safeConn.WriteJSON(map[string]interface{}{
					"type": "resize_ack",
					"data": map[string]interface{}{
						"session_id": sessionID,
						"timestamp":  time.Now().Unix(),
					},
				})

			case "focus":
				safeConn.WriteJSON(map[string]interface{}{
					"type": "focus_ack",
					"data": map[string]interface{}{
						"session_id": sessionID,
						"focused":    true,
						"timestamp":  time.Now().Unix(),
					},
				})

			case "blur":
				safeConn.WriteJSON(map[string]interface{}{
					"type": "blur_ack",
					"data": map[string]interface{}{
						"session_id": sessionID,
						"focused":    false,
						"timestamp":  time.Now().Unix(),
					},
				})

			case "close":
				log.Printf("Terminal %s close requested", sessionID)
				cancel() // Ensure goroutines stop
				ws.terminalManager.CloseSession(sessionID)
				return
			}
		}
	}

	// Clean up session when connection closes
	ws.terminalManager.CloseSession(sessionID)
}

