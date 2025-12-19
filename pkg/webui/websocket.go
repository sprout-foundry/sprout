package webui

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// handleWebSocket handles WebSocket connections for real-time events
func (ws *ReactWebServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Store connection
	ws.connections.Store(conn, true)
	defer ws.connections.Delete(conn)

	// Send initial connection status
	conn.WriteJSON(map[string]interface{}{
		"type": "connection_status",
		"data": map[string]bool{"connected": true},
	})

	// Set up close handler to send disconnect status
	conn.SetCloseHandler(func(code int, text string) error {
		log.Printf("WebSocket closing with code %d: %s", code, text)
		return nil
	})

	// Subscribe to events - EventBus should always be available in real deployments
	eventCh := ws.eventBus.Subscribe("webui")
	defer ws.eventBus.Unsubscribe("webui")

	// Send events to WebSocket with improved event handling
	conn.SetReadLimit(512 * 1024) // 512KB max message size
	for {
		select {
		case event := <-eventCh:
			if err := conn.WriteJSON(event); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}
		case <-time.After(50 * time.Millisecond):
			// Check if connection is still alive before reading
			if _, _, err := conn.NextReader(); err != nil {
				if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) ||
					websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket connection closed: %v", err)
					return
				}
				// Continue if it's just a timeout or other non-fatal error
				continue
			}

			// Connection is alive, try to read message
			// Set a very short deadline just for the read
			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			var msg map[string]interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				// Handle timeout and normal closure gracefully
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// Reset deadline for next iteration
					conn.SetReadDeadline(time.Time{})
					continue
				}
				if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) ||
					websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket read error: %v", err)
					return
				}
				// Reset deadline for next iteration
				conn.SetReadDeadline(time.Time{})
				continue
			}
			// Reset deadline since we got a message
			conn.SetReadDeadline(time.Time{})

			// Handle incoming WebSocket messages
			ws.handleWebSocketMessage(conn, msg)
		}
	}
}

// handleWebSocketMessage processes incoming WebSocket messages
func (ws *ReactWebServer) handleWebSocketMessage(conn *websocket.Conn, msg map[string]interface{}) {
	msgType, ok := msg["type"].(string)
	if !ok {
		return
	}

	switch msgType {
	case "ping":
		// Respond to ping with pong
		conn.WriteJSON(map[string]interface{}{
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
			conn.WriteJSON(map[string]interface{}{
				"type": "stats_update",
				"data": stats,
			})
		}()
	}
}

// handleTerminalWebSocket handles terminal WebSocket connections
func (ws *ReactWebServer) handleTerminalWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Terminal WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Generate session ID
	sessionID := fmt.Sprintf("terminal_%d", time.Now().UnixNano())

	// Create terminal session
	session, err := ws.terminalManager.CreateSession(sessionID)
	if err != nil {
		log.Printf("Failed to create terminal session: %v", err)
		conn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Failed to create terminal session"},
		})
		return
	}

	// Store connection
	ws.connections.Store(conn, true)
	defer ws.connections.Delete(conn)

	// Send session created message
	conn.WriteJSON(map[string]interface{}{
		"type": "session_created",
		"data": map[string]string{"session_id": sessionID},
	})

	// Start output reader goroutine with improved error handling
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Terminal output reader panic: %v", r)
			}
		}()

		if session.OutputCh != nil {
			for output := range session.OutputCh {
				select {
				case <-r.Context().Done():
					return
				default:
					if err := conn.WriteJSON(map[string]interface{}{
						"type": "output",
						"data": map[string]string{
							"session_id": sessionID,
							"output":     string(output),
						},
					}); err != nil {
						log.Printf("Terminal WebSocket write error: %v", err)
						return
					}
				}
			}
		}
	}()

	// Handle incoming messages with improved focus handling
	conn.SetReadLimit(512 * 1024) // 512KB max message size
	for {
		// Check if connection is still healthy before reading
		if _, _, err := conn.NextReader(); err != nil {
			if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) ||
				websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Terminal WebSocket connection closed: %v", err)
			}
			break
		}

		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Terminal WebSocket read error: %v", err)
			}
			break
		}

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
				conn.WriteJSON(map[string]interface{}{
					"type": "error",
					"data": map[string]string{
						"session_id": sessionID,
						"message":    err.Error(),
					},
				})
			}

		case "resize":
			// Handle terminal resize - actually resize the PTY
			if data, ok := msg["data"].(map[string]interface{}); ok {
				if rows, ok := data["rows"].(float64); ok {
					if cols, ok := data["cols"].(float64); ok {
						// Resize the terminal PTY
						err := ws.terminalManager.ResizeTerminal(sessionID, uint16(rows), uint16(cols))
						if err != nil {
							log.Printf("Failed to resize terminal %s: %v", sessionID, err)
							conn.WriteJSON(map[string]interface{}{
								"type": "error",
								"data": map[string]string{
									"session_id": sessionID,
									"message":    fmt.Sprintf("Resize failed: %v", err),
								},
							})
						} else {
							log.Printf("Terminal %s resized to %dx%d", sessionID, int(rows), int(cols))
						}
					}
				}
			}

			conn.WriteJSON(map[string]interface{}{
				"type": "resize_ack",
				"data": map[string]interface{}{
					"session_id": sessionID,
					"timestamp":  time.Now().Unix(),
				},
			})

		case "focus":
			// Handle terminal focus requests - this helps with the focus issues
			conn.WriteJSON(map[string]interface{}{
				"type": "focus_ack",
				"data": map[string]interface{}{
					"session_id": sessionID,
					"focused":    true,
					"timestamp":  time.Now().Unix(),
				},
			})

		case "blur":
			// Handle terminal blur events
			conn.WriteJSON(map[string]interface{}{
				"type": "blur_ack",
				"data": map[string]interface{}{
					"session_id": sessionID,
					"focused":    false,
					"timestamp":  time.Now().Unix(),
				},
			})

		case "close":
			// Close the terminal session
			ws.terminalManager.CloseSession(sessionID)
			return
		}
	}

	// Clean up session when connection closes
	ws.terminalManager.CloseSession(sessionID)
}
