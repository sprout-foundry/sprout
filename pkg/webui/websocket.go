package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/security"
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
	clientID := ws.resolveClientID(r)

	// Store the underlying connection with metadata
	ws.connections.Store(conn, &ConnectionInfo{
		SessionID:   sessionID,
		ClientID:    clientID,
		Type:        "webui",
		ConnectedAt: time.Now(),
	})
	defer ws.connections.Delete(conn)

	log.Printf("WebSocket client connected: %s", sessionID)

	// Send initial connection status
	safeConn.WriteJSON(map[string]interface{}{
		"type": "connection_status",
		"data": map[string]interface{}{"connected": true, "session_id": sessionID, "client_id": clientID},
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

	// Track last message time for dead connection detection
	lastMessage := time.Now()

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

				// Update last message time on successful read (includes pong responses,
				// which reset the dead connection timer).
				lastMessage = time.Now()

				// Touch the client context so it stays alive while the WebSocket
				// is active. Without this, a long-lived WebSocket connection in a
				// paused Chrome tab could have its client context garbage-collected
				// by the idle cleanup worker because no HTTP requests arrive.
				ws.touchClientLastSeen(clientID)

				// Handle incoming WebSocket messages
				ws.handleWebSocketMessage(safeConn, msg, clientID)
			}
		}
	}() // Write loop - handles outgoing events
	for {
		select {
		case <-ctx.Done():
			log.Printf("WebSocket %s context cancelled", sessionID)
			return

		case event := <-eventCh:
			if !ws.shouldForwardEventToConnection(event, clientID) {
				continue
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

func (ws *ReactWebServer) shouldForwardEventToConnection(event events.UIEvent, clientID string) bool {
	data, _ := event.Data.(map[string]interface{})
	if targetClientID, _ := data["client_id"].(string); strings.TrimSpace(targetClientID) != "" {
		return targetClientID == clientID
	}

	// Untargeted events should not leak across client windows.
	// Only allow explicit global events without a client_id.
	switch event.Type {
	case events.EventTypeMetricsUpdate, events.EventTypeFileContentChanged, events.EventTypeSecurityPromptRequest, events.EventTypeSecurityApprovalRequest:
		return true
	default:
		return false
	}
}

// handleWebSocketMessage processes incoming WebSocket messages
func (ws *ReactWebServer) handleWebSocketMessage(safeConn *SafeConn, msg map[string]interface{}, clientID string) {
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

	case "pong":
		// Client responded to ping - handled by read goroutine timestamp tracking
		// The read goroutine updates lastMessage on any successful read

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
			stats := ws.gatherStatsForClientID(clientID)
			safeConn.WriteJSON(map[string]interface{}{
				"type": "stats_update",
				"data": stats,
			})
		}()

	case "provider_change":
		go ws.handleProviderChangeMessage(safeConn, msg, clientID)

	case "model_change":
		go ws.handleModelChangeMessage(safeConn, msg, clientID)

	case "persona_change":
		go ws.handlePersonaChangeMessage(safeConn, msg, clientID)

	case "security_approval_response":
		go ws.handleSecurityApprovalResponse(safeConn, msg, clientID)

	case "security_prompt_response":
		go ws.handleSecurityPromptResponse(safeConn, msg, clientID)
	}
}

func (ws *ReactWebServer) handleProviderChangeMessage(safeConn *SafeConn, msg map[string]interface{}, clientID string) {
	// Use the active chat's agent for provider changes.
	activeChatID := ""
	ws.mutex.RLock()
	var ctx *webClientContext
	if ctx = ws.clientContexts[clientID]; ctx != nil {
		activeChatID = ctx.getActiveChatID()
	}
	ws.mutex.RUnlock()

	clientAgent, err := ws.getChatAgent(clientID, activeChatID)
	if err != nil || clientAgent == nil || clientAgent.GetConfigManager() == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Agent is not available"},
		})
		return
	}

	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Invalid provider change payload"},
		})
		return
	}

	providerName, _ := data["provider"].(string)
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Provider is required"},
		})
		return
	}

	providerType, err := clientAgent.GetConfigManager().MapStringToClientType(providerName)
	if err != nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": err.Error()},
		})
		return
	}

	// Check active query for the active chat, not the global client
	if ctx != nil && activeChatID != "" && ctx.hasActiveQueryForChat(activeChatID) {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Cannot change provider while this chat has an active run"},
		})
		return
	}

	if err := clientAgent.SetProvider(providerType); err != nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": err.Error()},
		})
		return
	}

	// Store provider on the chat session for per-session tracking.
	ws.mutex.RLock()
	if ctx := ws.clientContexts[clientID]; ctx != nil && activeChatID != "" {
		if cs := ctx.getChatSession(activeChatID); cs != nil {
			cs.mu.Lock()
			cs.Provider = api.GetProviderName(clientAgent.GetProviderType())
			cs.Model = clientAgent.GetModel()
			cs.mu.Unlock()
		}
	}
	ws.mutex.RUnlock()

	_ = ws.syncAgentStateForClientWithChat(clientID, activeChatID)
	ws.publishProviderState(clientID)
}

func (ws *ReactWebServer) handleModelChangeMessage(safeConn *SafeConn, msg map[string]interface{}, clientID string) {
	// Use the active chat's agent for model changes.
	activeChatID := ""
	ws.mutex.RLock()
	var ctx *webClientContext
	if ctx = ws.clientContexts[clientID]; ctx != nil {
		activeChatID = ctx.getActiveChatID()
	}
	ws.mutex.RUnlock()

	clientAgent, err := ws.getChatAgent(clientID, activeChatID)
	if err != nil || clientAgent == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Agent is not available"},
		})
		return
	}

	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Invalid model change payload"},
		})
		return
	}

	modelName, _ := data["model"].(string)
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Model is required"},
		})
		return
	}

	// Check active query for the active chat, not the global client
	if ctx != nil && activeChatID != "" && ctx.hasActiveQueryForChat(activeChatID) {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Cannot change model while this chat has an active run"},
		})
		return
	}

	previousProvider := clientAgent.GetProviderType()
	previousModel := clientAgent.GetModel()
	providerChanged := false

	if providerName, _ := data["provider"].(string); strings.TrimSpace(providerName) != "" {
		providerType, err := clientAgent.GetConfigManager().MapStringToClientType(providerName)
		if err == nil && providerType != clientAgent.GetProviderType() {
			if err := clientAgent.SetProvider(providerType); err != nil {
				_ = safeConn.WriteJSON(map[string]interface{}{
					"type": "error",
					"data": map[string]string{"message": err.Error()},
				})
				return
			}
			providerChanged = true
		}
	}

	if err := clientAgent.SetModel(modelName); err != nil {
		if providerChanged && previousProvider != "" {
			if rollbackErr := clientAgent.SetProvider(previousProvider); rollbackErr != nil {
				log.Printf("webui: failed to rollback provider change after model switch failure: provider=%s model=%s rollback_err=%v", previousProvider, previousModel, rollbackErr)
			} else if strings.TrimSpace(previousModel) != "" {
				if rollbackModelErr := clientAgent.SetModel(previousModel); rollbackModelErr != nil {
					log.Printf("webui: provider rollback succeeded but failed to restore model %q: %v", previousModel, rollbackModelErr)
				}
			}
		}
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": err.Error()},
		})
		return
	}

	// Store model on the chat session for per-session tracking.
	ws.mutex.RLock()
	if ctx := ws.clientContexts[clientID]; ctx != nil && activeChatID != "" {
		if cs := ctx.getChatSession(activeChatID); cs != nil {
			cs.mu.Lock()
			cs.Provider = api.GetProviderName(clientAgent.GetProviderType())
			cs.Model = clientAgent.GetModel()
			cs.mu.Unlock()
		}
	}
	ws.mutex.RUnlock()

	_ = ws.syncAgentStateForClientWithChat(clientID, activeChatID)
	ws.publishProviderState(clientID)
}

// handlePersonaChangeMessage handles persona change requests from the webui.
func (ws *ReactWebServer) handlePersonaChangeMessage(safeConn *SafeConn, msg map[string]interface{}, clientID string) {
	// Use the active chat's agent for persona changes.
	activeChatID := ""
	ws.mutex.RLock()
	var ctx *webClientContext
	if ctx = ws.clientContexts[clientID]; ctx != nil {
		activeChatID = ctx.getActiveChatID()
	}
	ws.mutex.RUnlock()

	clientAgent, err := ws.getChatAgent(clientID, activeChatID)
	if err != nil || clientAgent == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Agent is not available"},
		})
		return
	}

	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Invalid persona change payload"},
		})
		return
	}

	personaID, _ := data["persona"].(string)
	personaID = strings.TrimSpace(personaID)
	if personaID == "" {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Persona is required"},
		})
		return
	}

	// Check active query for the active chat, not the global client
	if ctx != nil && activeChatID != "" && ctx.hasActiveQueryForChat(activeChatID) {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Cannot change persona while this chat has an active run"},
		})
		return
	}

	if err := clientAgent.ApplyPersona(personaID); err != nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": err.Error()},
		})
		return
	}

	_ = ws.syncAgentStateForClientWithChat(clientID, activeChatID)
	ws.publishProviderState(clientID)
}

// handleSecurityApprovalResponse processes security approval responses from the webui.
// The webui sends a { "type": "security_approval_response", "data": { "request_id": "...", "approved": true/false } }
// message when the user approves or rejects a security warning.
func (ws *ReactWebServer) handleSecurityApprovalResponse(safeConn *SafeConn, msg map[string]interface{}, clientID string) {
	// Route to the currently active chat's agent, since the security dialog
	// is always shown in the context of the active chat view.
	activeChatID := ""
	ws.mutex.RLock()
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		activeChatID = ctx.getActiveChatID()
	}
	ws.mutex.RUnlock()

	clientAgent, err := ws.getChatAgent(clientID, activeChatID)
	if err != nil || clientAgent == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Agent is not available"},
		})
		return
	}

	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Invalid security approval response payload"},
		})
		return
	}

	requestID, _ := data["request_id"].(string)
	if requestID == "" {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "request_id is required"},
		})
		return
	}

	approved, _ := data["approved"].(bool)

	mgr := clientAgent.GetSecurityApprovalMgr()
	if mgr == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Security approval manager is not available"},
		})
		return
	}

	if !mgr.RespondToApproval(requestID, approved) {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": fmt.Sprintf("No pending security request with id: %s", requestID)},
		})
		return
	}

	log.Printf("Security approval response received: request_id=%s approved=%v", requestID, approved)
}

// handleSecurityPromptResponse processes security prompt responses from the webui.
// The webui sends a { "type": "security_prompt_response", "data": { "request_id": "...", "response": true/false } }
// message when the user responds to a file security concern prompt.
func (ws *ReactWebServer) handleSecurityPromptResponse(safeConn *SafeConn, msg map[string]interface{}, clientID string) {
	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Invalid security prompt response payload"},
		})
		return
	}

	requestID, _ := data["request_id"].(string)
	if requestID == "" {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "request_id is required"},
		})
		return
	}

	response, _ := data["response"].(bool)

	mgr := security.GetGlobalPromptManager()
	if mgr == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Security prompt manager is not available"},
		})
		return
	}

	if mgr.RespondToPrompt(requestID, response) {
		ws.publishClientEvent(clientID, events.EventTypeSecurityPromptRequest, map[string]interface{}{
			"status":     "responded",
			"request_id": requestID,
			"response":   response,
		})
		log.Printf("Security prompt response received: request_id=%s response=%v", requestID, response)
	} else {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": fmt.Sprintf("No pending security prompt with id: %s", requestID)},
		})
	}
}

// handleTerminalWebSocket handles terminal WebSocket connections.
// Supports both creating new sessions and reattaching to existing tmux-backed sessions.
// The client can request reattachment by passing ?reattach=<sessionID> in the URL.
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

	terminalManager := ws.getTerminalManagerForRequest(r)

	// Check if client wants to reattach to an existing session
	reattachID := strings.TrimSpace(r.URL.Query().Get("reattach"))
	var sessionID string
	var session *TerminalSession

	if reattachID != "" && terminalManager.HasSession(reattachID) {
		// Try to reattach to the existing session
		scrollback, err := terminalManager.ReattachSession(reattachID, 2000)
		if err != nil {
			log.Printf("Failed to reattach to session %s: %v, creating new session", reattachID, err)
			// Fall through to create new session
		} else {
			sessionID = reattachID
			session, _ = terminalManager.GetSession(sessionID)

			// Send session_restored message with scrollback
			if err := safeConn.WriteJSON(map[string]interface{}{
				"type": "session_restored",
				"data": map[string]interface{}{
					"session_id":  sessionID,
					"scrollback":  scrollback,
					"tmux_backed": true,
				},
			}); err != nil {
				log.Printf("Terminal %s FAILED to send session_restored: %v", sessionID, err)
			} else {
				log.Printf("Terminal %s reattached successfully (scrollback: %d bytes)", sessionID, len(scrollback))
			}
		}
	}

	// Create new session if not reattaching
	if session == nil {
		sessionID = fmt.Sprintf("terminal_%d", time.Now().UnixNano())
		log.Printf("Terminal WebSocket connection starting: %s", sessionID)

		shellOverride := strings.TrimSpace(r.URL.Query().Get("shell"))
		if len(shellOverride) > 64 {
			shellOverride = shellOverride[:64]
		}
		// Cross-check the override against the known available shells so
		// that only actual shells (not arbitrary PATH binaries) can be
		// started as PTY sessions.
		if shellOverride != "" {
			allowed := terminalManager.AvailableShells()
			valid := false
			for _, s := range allowed {
				if s.Name == shellOverride || s.Path == shellOverride {
					valid = true
					break
				}
			}
			if !valid {
				log.Printf("Terminal %s: shell override %q not in available shells list, ignoring", sessionID, shellOverride)
				shellOverride = ""
			}
		}
		session, err = terminalManager.CreateSession(sessionID, shellOverride)
		if err != nil {
			log.Printf("Failed to create terminal session: %v", err)
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": "Failed to create terminal session"},
			})
			return
		}

		// Send session created message
		msg := map[string]interface{}{
			"type": "session_created",
			"data": map[string]string{"session_id": sessionID},
		}
		msgBytes, _ := json.Marshal(msg)
		log.Printf("Terminal %s message bytes: %s", sessionID, string(msgBytes))

		if err := safeConn.WriteJSON(msg); err != nil {
			log.Printf("Terminal %s FAILED to send session_created: %v", sessionID, err)
		} else {
			log.Printf("Terminal %s successfully sent session_created", sessionID)
		}
	}

	// Store the underlying connection with metadata
	ws.connections.Store(conn, &ConnectionInfo{
		SessionID:   sessionID,
		Type:        "terminal",
		ConnectedAt: time.Now(),
	})
	defer ws.connections.Delete(conn)

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
			// Set read deadline for heartbeat.
			// Allows 90 seconds per individual read attempt. Chrome pauses
			// background tabs aggressively; the freeze→resume lifecycle
			// handlers on the client side should reconnect sooner than this,
			// but we give extra headroom to avoid premature disconnects.
			conn.SetReadDeadline(time.Now().Add(90 * time.Second))

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
				// On WebSocket disconnect, detach from session instead of closing it.
				// For tmux-backed sessions, this preserves the tmux session for reattach.
				// For raw PTY sessions, DetachFromSession will close them completely.
				terminalManager.DetachFromSession(sessionID)
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

				fmt.Printf("Terminal WebSocket: Received input command for session %s: %q\n", sessionID, input)
				if err := terminalManager.ExecuteCommand(sessionID, input); err != nil {
					safeConn.WriteJSON(map[string]interface{}{
						"type": "error",
						"data": map[string]string{
							"session_id": sessionID,
							"message":    err.Error(),
						},
					})
				}

			case "input_raw":
				data, ok := msg["data"].(map[string]interface{})
				if !ok {
					continue
				}

				input, ok := data["input"].(string)
				if !ok {
					continue
				}

				if err := terminalManager.WriteRawInput(sessionID, input); err != nil {
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
							err := terminalManager.ResizeTerminal(sessionID, uint16(rows), uint16(cols))
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
				terminalManager.CloseSession(sessionID)
				return
			}
		}
	}
}
