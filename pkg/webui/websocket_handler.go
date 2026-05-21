//go:build !js

package webui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/gorilla/websocket"
)

// handleWebSocket handles WebSocket connections for real-time events
func (ws *ReactWebServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// Wrap connection in SafeConn to prevent concurrent write panics
	safeConn := NewSafeConn(conn)
	defer safeConn.Close()

	// Generate unique session ID for this connection using cryptographically secure random
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Printf("Failed to generate session ID: %v", err)
		conn.Close()
		return
	}
	sessionID := "ws_" + hex.EncodeToString(b)
	clientID := ws.resolveClientID(r)

	// Panic recovery for the main handler - moved here so safeConn and sessionID are available
	defer func() {
		if r := recover(); r != nil {
			log.Printf("WebSocket handler panic: %v", r)
			safeConn.WritePanicError(sessionID, "websocket handler", r)
			ws.cleanupAfterPanic(clientID, sessionID)
		}
	}()

	userID := ws.ExtractUserID(r)

	// Read chat_id from query params (optional)
	chatID := r.URL.Query().Get("chat_id")

	// Read reattach params (SP-034-2c). When the client reconnects mid-query
	// it sends ?reattach=<chat-id>&after_seq=<last-seen-seq>; we look up the
	// chat's runBuffer and replay everything with seq > after_seq before the
	// live event loop starts. reattachChatID takes precedence over chat_id
	// when both are present — the spec disambiguates that case.
	reattachChatID := strings.TrimSpace(r.URL.Query().Get("reattach"))
	afterSeq := parseAfterSeqQuery(r.URL.Query().Get("after_seq"))
	if reattachChatID != "" {
		chatID = reattachChatID
	}

	// Store the underlying connection with metadata
	ws.connections.Store(conn, &ConnectionInfo{
		SessionID:   sessionID,
		ClientID:    clientID,
		ChatID:      chatID,
		Type:        "webui",
		UserID:      userID,
		ConnectedAt: time.Now(),
	})
	defer ws.connections.Delete(conn)

	// Auto-subscribe to the connected chat (SP-034-3b). When the client
	// switches chats over its lifetime, it'll send a subscribe message
	// with the new chatID; we unsubscribe from the prior one on
	// disconnect (UnsubscribeAll covers it).
	if chatID != "" && ws.chatSubscribers != nil {
		ws.chatSubscribers.Subscribe(chatID, conn)
	}
	defer func() {
		if ws.chatSubscribers != nil {
			ws.chatSubscribers.UnsubscribeAll(conn)
		}
	}()

	log.Printf("WebSocket client connected: %s", sessionID)
	if userID != "" {
		log.Printf("[web] WebSocket user: %s (session %s)", userID, sessionID)
	}

	// Send initial connection status
	safeConn.WriteJSON(map[string]interface{}{
		"type": "connection_status",
		"data": map[string]interface{}{"connected": true, "session_id": sessionID, "client_id": clientID},
	})

	// Replay any missed events before we start the live loop. Subscribing
	// AFTER the replay ensures the client sees buffered events strictly
	// before any live events that arrive on this connection — there's no
	// chance of seeing seq N+5 (live) before seq N+3 (replay).
	if reattachChatID != "" {
		ws.deliverChatRunReplay(safeConn, clientID, reattachChatID, afterSeq)
	}

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

	// Track last message time for dead connection detection
	lastMessage := time.Now()

	// Read goroutine - handles incoming messages
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer func() {
			if r := recover(); r != nil {
				log.Printf("WebSocket read goroutine panic recovered: %v", r)
				safeConn.WritePanicError(sessionID, "read goroutine", r)
				ws.cleanupAfterPanic(clientID, sessionID)
				cancel() // ensure write loop exits cleanly
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

				// Read raw message bytes for validation
				_, rawMsg, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) ||
						websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						log.Printf("WebSocket %s closed: %v", sessionID, err)
					} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						// If no message received in 180 seconds (3 minutes), connection is dead.
						// Chrome pauses background tabs aggressively, freezing timers and
						// throttling network. 3 minutes gives enough time for the pong
						// watchdog on the client side to detect the issue and proactively
						// reconnect before the server kills the connection.
						if time.Since(lastMessage) > 180*time.Second {
							log.Printf("WebSocket %s no activity for 180s, closing", sessionID)
							return
						}
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

				// Validate the incoming message
				msg, err := parseAndValidateMessage(rawMsg)
				if err != nil {
					log.Printf("WebSocket %s message validation failed: %v", sessionID, err)
					safeConn.WriteJSON(map[string]interface{}{
						"type": "error",
						"data": map[string]string{"message": err.Error()},
					})
					continue
				}

				// Update last message time on successful read (includes pong responses,
				// which reset the dead connection timer).
				lastMessage = time.Now()

				// Touch the client context so it stays alive while the WebSocket
				// is active. Without this, a long-lived WebSocket connection in a
				// paused Chrome tab could have its client context garbage-collected
				// by the idle cleanup worker because no HTTP requests arrive.
				ws.touchClientLastSeen(clientID)

				// Handle incoming WebSocket messages
				ws.handleWebSocketMessage(safeConn, sessionID, msg, clientID)
			}
		}
	}() // Write loop - handles outgoing events
	for {
		select {
		case <-ctx.Done():
			log.Printf("WebSocket %s context cancelled", sessionID)
			return

		case event := <-eventCh:
			// Get connection info for this connection
			connInfoVal, ok := ws.connections.Load(conn)
			if !ok {
				log.Printf("WebSocket %s connection info not found, skipping event", sessionID)
				continue
			}
			connInfo, ok := connInfoVal.(*ConnectionInfo)
			if !ok {
				log.Printf("WebSocket %s connection info type mismatch, skipping event", sessionID)
				continue
			}
			if !ws.shouldForwardEventToConnection(event, connInfo) {
				continue
			}
			if event.Type == events.EventTypeSecurityApprovalRequest {
				if data, ok := event.Data.(map[string]interface{}); ok {
					log.Printf("[SECURITY] Forwarding security_approval_request to client %s: request_id=%v tool=%s risk=%s",
						connInfo.ClientID, data["request_id"], data["tool_name"], data["risk_level"])
				}
			}
			if event.Type == events.EventTypeAskUserRequest {
				if data, ok := event.Data.(map[string]interface{}); ok {
					log.Printf("[ASK_USER] Forwarding ask_user_request to client %s: request_id=%v question=%q",
						connInfo.ClientID, data["request_id"], data["question"])
				}
			}
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

func (ws *ReactWebServer) shouldForwardEventToConnection(event events.UIEvent, connInfo *ConnectionInfo) bool {
	data, _ := event.Data.(map[string]interface{})

	// --- User isolation (service mode) ---
	// If the connection has a UserID, enforce user boundary:
	//   - Events with a user_id must match the connection's UserID
	//   - Events without user_id (or empty/whitespace-only) are allowed through (backward compatible)
	// If the connection has NO UserID (local mode), skip user filtering.
	if connInfo.UserID != "" {
		eventUserID, _ := data["user_id"].(string)
		if strings.TrimSpace(eventUserID) != "" {
			if eventUserID != connInfo.UserID {
				return false
			}
		}
	}

	// Extract target client_id and chat_id from event
	targetClientID, _ := data["client_id"].(string)
	targetChatID, _ := data["chat_id"].(string)

	// Check if event has client_id targeting
	if strings.TrimSpace(targetClientID) != "" {
		// Event has explicit client_id - must match connection's client_id
		if strings.TrimSpace(targetClientID) != strings.TrimSpace(connInfo.ClientID) {
			// Log mismatched security/interaction events for diagnostics
			if event.Type == events.EventTypeSecurityApprovalRequest || event.Type == events.EventTypeSecurityPromptRequest || event.Type == events.EventTypeAskUserRequest {
				log.Printf("[SECURITY] Dropping %s event: payload client_id=%q does not match connection client_id=%q (request_id=%v)",
					event.Type, strings.TrimSpace(targetClientID), connInfo.ClientID, data["request_id"])
			}
			return false
		}
		// Client ID matches, now check chat_id if present
		if strings.TrimSpace(targetChatID) != "" {
			// Event has chat_id - connection must match or be unfiltered
			if strings.TrimSpace(connInfo.ChatID) != "" && strings.TrimSpace(connInfo.ChatID) != strings.TrimSpace(targetChatID) {
				return false
			}
		}
		return true
	}

	// No client_id in event - check chat_id targeting
	if strings.TrimSpace(targetChatID) != "" {
		// Event has chat_id but no client_id
		// Forward if connection has matching chat_id or no specific chat
		if strings.TrimSpace(connInfo.ChatID) != "" && strings.TrimSpace(connInfo.ChatID) != strings.TrimSpace(targetChatID) {
			return false
		}
		return true
	}

	// No client_id and no chat_id - only allow known global event types
	// or events with user_id (user-scoped broadcasts after passing user filtering above)
	switch event.Type {
	case events.EventTypeMetricsUpdate, events.EventTypeFileContentChanged, events.EventTypeSecurityPromptRequest, events.EventTypeSecurityApprovalRequest, events.EventTypeAskUserRequest, events.EventTypeDriftDetected:
		return true
	default:
		// Allow events with a non-empty user_id (user-scoped broadcasts)
		eventUserID, _ := data["user_id"].(string)
		if strings.TrimSpace(eventUserID) != "" {
			return true
		}
		return false
	}
}

// handleWebSocketMessage processes incoming WebSocket messages
func (ws *ReactWebServer) handleWebSocketMessage(safeConn *SafeConn, sessionID string, msg *WebSocketMessage, clientID string) {
	switch msg.Type {
	case AllowedMessageTypePing:
		// Respond to ping with pong
		safeConn.WriteJSON(map[string]interface{}{
			"type": "pong",
			"data": map[string]interface{}{"timestamp": time.Now().Unix()},
		})

	case AllowedMessageTypePong:
		// Client responded to ping - handled by read goroutine timestamp tracking
		// The read goroutine updates lastMessage on any successful read

	case AllowedMessageTypeSubscribe:
		// Handle subscription requests for specific event types AND
		// (SP-034-3b) chat-id subscriptions for multi-tab consistency.
		data, err := parseAndValidateData[SubscribeData](msg.Data, func(d *SubscribeData) error {
			return d.Validate()
		})
		if err != nil {
			log.Printf("WebSocket %s invalid subscribe data: %v", sessionID, err)
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": err.Error()},
			})
			return
		}
		log.Printf("WebSocket client subscribed to events: %v chat_ids: %v", data.Events, data.ChatIDs)

		// Register chat subscriptions so events for these chats fan out
		// to this connection even when the originating clientID differs
		// (e.g. same chat open in two browser tabs).
		if ws.chatSubscribers != nil {
			for _, chatID := range data.ChatIDs {
				ws.chatSubscribers.Subscribe(chatID, safeConn.Conn())
			}
		}

	case AllowedMessageTypeRequestStats:
		// Send current stats immediately
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, func() {
			stats := ws.gatherStatsForClientID(clientID)
			safeConn.WriteJSON(map[string]interface{}{
				"type": "stats_update",
				"data": stats,
			})
		})

	case AllowedMessageTypeProviderChange:
		data, err := parseAndValidateData[ProviderChangeData](msg.Data, func(d *ProviderChangeData) error {
			return d.Validate()
		})
		if err != nil {
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": err.Error()},
			})
			return
		}
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, func() {
			ws.handleProviderChangeMessage(safeConn, data, clientID)
		})

	case AllowedMessageTypeModelChange:
		data, err := parseAndValidateData[ModelChangeData](msg.Data, func(d *ModelChangeData) error {
			return d.Validate()
		})
		if err != nil {
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": err.Error()},
			})
			return
		}
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, func() {
			ws.handleModelChangeMessage(safeConn, data, clientID)
		})

	case AllowedMessageTypePersonaChange:
		data, err := parseAndValidateData[PersonaChangeData](msg.Data, func(d *PersonaChangeData) error {
			return d.Validate()
		})
		if err != nil {
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": err.Error()},
			})
			return
		}
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, func() {
			ws.handlePersonaChangeMessage(safeConn, data, clientID)
		})

	case AllowedMessageTypeSecurityApprovalResponse:
		data, err := parseAndValidateData[SecurityApprovalResponseData](msg.Data, func(d *SecurityApprovalResponseData) error {
			return d.Validate()
		})
		if err != nil {
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": err.Error()},
			})
			return
		}
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, func() {
			ws.handleSecurityApprovalResponse(safeConn, data, clientID)
		})

	case AllowedMessageTypeSecurityPromptResponse:
		data, err := parseAndValidateData[SecurityPromptResponseData](msg.Data, func(d *SecurityPromptResponseData) error {
			return d.Validate()
		})
		if err != nil {
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": err.Error()},
			})
			return
		}
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, func() {
			ws.handleSecurityPromptResponse(safeConn, data, clientID)
		})

	case AllowedMessageTypeAskUserResponse:
		data, err := parseAndValidateData[AskUserResponseData](msg.Data, func(d *AskUserResponseData) error {
			return d.Validate()
		})
		if err != nil {
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": err.Error()},
			})
			return
		}
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, func() {
			ws.handleAskUserResponse(safeConn, data, clientID)
		})
	}
}

// safeHandleGoroutine runs fn in a goroutine with panic recovery. If fn
// panics, an error event is written to the WebSocket, the client's active
// query state is reset, and the connection is closed.
func (ws *ReactWebServer) safeHandleGoroutine(safeConn *SafeConn, sessionID, clientID string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("WebSocket handler panic in session %s: %v", sessionID, r)
				safeConn.WritePanicError(sessionID, "message handler", r)
				ws.cleanupAfterPanic(clientID, sessionID)
				safeConn.Close() // Terminate the session since state is unreliable after a panic
			}
		}()
		fn()
	}()
}

// cleanupAfterPanic resets the client's query state and publishes a clean
// state event so the UI doesn't get stuck showing "running" after a panic.
//
// Design note: this clears ALL chat sessions for the clientID, not just the
// one that panicked. This is intentional — a panicked goroutine may have
// corrupted shared agent state (e.g. the MCP manager or conversation history),
// so it's safer to force full agent recreation rather than risk using a
// half-initialized agent in other chat sessions.
//
// The session_terminated event is published to the event bus for any other
// subscribers (monitoring, multi-tab clients). The panicked connection
// itself already receives the event directly via WritePanicError.
func (ws *ReactWebServer) cleanupAfterPanic(clientID, sessionID string) {
	if strings.TrimSpace(clientID) == "" {
		return
	}

	// 1. Decrement top-level active query counter and clear top-level state
	ws.decrementActiveQueries(clientID)

	// 2. Reset per-chat session query state — prevents individual chats
	// from being stuck in "running" state after a panic.
	// Hold ws.mutex to safely access ctx and its fields (ActiveQuery,
	// CurrentQuery, ChatSessions) which are always guarded by ws.mutex.
	ws.mutex.Lock()
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		ctx.clearAllChatQueryState()
	}
	ws.mutex.Unlock()

	// 3. Clear cached agents — a panicked goroutine may have left the
	// agent in a half-initialized or corrupted state. Force recreation.
	ws.clearCachedAgent(clientID)

	// 4. Publish session_terminated event so the client can tear down UI
	ws.publishClientEvent(clientID, events.EventTypeSessionTerminated, map[string]interface{}{
		"session_id": sessionID,
		"status":     "error",
		"code":       "internal_panic",
		"message":    "Session terminated due to internal error",
	})
}
