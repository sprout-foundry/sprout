//go:build !js

package webui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// activeWSConn tracks a single active WebSocket connection for a user
// to enforce single-active-session policy (SP-118 Phase 1, Mode 1).
// In SP-118 Phase 2 (Mode 2), this type is not used; the daemon uses
// UserConnections (pkg/webui/multi_connection_registry.go) instead.
type activeWSConn struct {
	safeConn    *SafeConn
	conn        *websocket.Conn
	sessionID   string
	connectedAt time.Time
	closed      chan struct{} // closed when the connection is closed
}

// handleWebSocket is the internal entry point that pkg/webui/routes.go
// wires to /ws. It dispatches to the mode-appropriate handler based on
// agentEnforceSingleSession:
//   - true  → handleWebSocket_Agent (single-active-session, Mode 1)
//   - false → handleWebSocket_Daemon (multi-session, Mode 2)
//
// Do NOT dispatch on serviceMode here. Tests like
// TestSessionConflict_Takeover_UserMode set serviceMode=true and exercise
// the takeover flow as Mode 1 behavior; re-using serviceMode as the
// dispatch key would break those tests. See SP-118 §Design "Dispatch
// signal".
func (ws *ReactWebServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if ws.agentEnforceSingleSession {
		ws.handleWebSocket_Agent(w, r)
		return
	}
	ws.handleWebSocket_Daemon(w, r)
}

// handleWebSocket_Daemon handles WebSocket connections in multi-session
// mode (daemon, sprout service). N parallel browser windows are allowed
// per user; each gets its own chat session. This is the Mode 2 / SP-118
// path.
//
// In contrast to handleWebSocket_Agent (Mode 1) this handler:
//   - does NOT enforce single-active-session via activeWSByUserID
//   - does NOT trigger session_conflict / waitForTakeover / session_displaced
//   - registers the connection in ws.userConnections so other paths
//     (cleanupAfterPanicSession, future diagnostics) can find it
//   - calls cleanupAfterPanicSession on read-goroutine panic, which
//     scopes the blast radius to this session rather than the whole
//     clientID (preserved by the per-user connection count check)
//
// The pre-loop work (upgrade, sessionID, resolveClientID, panic recovery,
// connection storage, chatSubscribers subscribe, replay, then the
// read/write goroutines) is structurally identical to Mode 1 — the
// differences live in (a) what counts as a "conflict" (nothing does)
// and (b) what cleanup runs on panic. To keep both handlers readable
// without duplicating ~300 lines of loop body, the live loop is split
// out into runConnectionLiveLoop below; both modes call it after their
// mode-specific pre-loop setup.
//
// Effective routing for the dispatcher:
//   handleWebSocket (entry) reads `agentEnforceSingleSession` and
//   forwards here when false. The dispatcher is the ONLY dispatch point;
//   internal callers use one of the two mode handlers directly.
func (ws *ReactWebServer) handleWebSocket_Daemon(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error (daemon): %v", err)
		return
	}

	safeConn := NewSafeConn(conn)
	defer safeConn.Close()

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Printf("[SP-118-Mode2] Failed to generate session ID: %v", err)
		conn.Close()
		return
	}
	sessionID := "ws_" + hex.EncodeToString(b)
	clientID := ws.resolveClientID(r)
	userID := ws.ExtractUserID(r)
	chatID := r.URL.Query().Get("chat_id")

	// Mode 2 panic recovery — use the session-scoped cleanup so a panic
	// in one window doesn't invalidate sibling windows on the same
	// clientID. safeConn + sessionID are in scope, so we can mirror the
	// Mode 1 defer shape exactly.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[SP-118-Mode2] WebSocket handler panic: %v", r)
			safeConn.WritePanicError(sessionID, "websocket handler", r)
			ws.cleanupAfterPanicSession(clientID, userID, chatID, sessionID)
		}
	}()

	reattachChatID := strings.TrimSpace(r.URL.Query().Get("reattach"))
	afterSeq := parseAfterSeqQuery(r.URL.Query().Get("after_seq"))
	if reattachChatID != "" {
		chatID = reattachChatID
	}

	// trackingKey: userID in service mode, clientID in local mode. This
	// matches the Mode 1 convention so cleanupAfterPanicSession can
	// reason about "other windows for the same user" correctly.
	trackingKey := userID
	if trackingKey == "" {
		trackingKey = clientID
	}

	// Register in the multi-connection registry BEFORE the live loop so
	// concurrent panic cleanup can find this session. Removal happens on
	// exit via the deferred unregister below.
	if ws.userConnections != nil {
		ws.userConnections.Add(trackingKey, UserConnection{
			Conn:      safeConn,
			Raw:       conn,
			SessionID: sessionID,
			ClientID:  clientID,
			UserID:    userID,
		})
		defer ws.userConnections.Remove(trackingKey, conn)
	}

	// Mode 2 has no terminal-displacement notification. The function is
	// a no-op here by design — calling it would only matter if a
	// takeover happened, which Mode 2 explicitly does not do.
	log.Printf("[SP-118-Mode2] Daemon connection accepted for user %s session %s (count=%d)",
		trackingKey, sessionID, ws.userConnections.Count(trackingKey))

	ws.runConnectionLiveLoop(conn, safeConn, sessionID, clientID, userID, chatID, reattachChatID, afterSeq, true)
}

// handleWebSocket_Agent handles WebSocket connections in single-active-session mode
// (sprout agent / CWS-bound mode). This is the Mode 1 path: only one browser window
// may be active at a time per user. A second window triggers session_conflict and
// waits for the user to confirm takeover via session_takeover.
//
// Dispatch from handleWebSocket (the internal entry point) uses agentEnforceSingleSession,
// NOT serviceMode. See SP-118 §Design "Dispatch signal".
func (ws *ReactWebServer) handleWebSocket_Agent(w http.ResponseWriter, r *http.Request) {
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
			ws.cleanupAfterPanicAgent(clientID, sessionID)
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

	// --- SP-046: Single-active-session conflict detection ---
	// Determine the tracking key: use userID if present (service mode),
	// otherwise fall back to clientID (local mode). This identifies the
	// "user" for single-session enforcement.
	trackingKey := userID
	if trackingKey == "" {
		trackingKey = clientID
	}

	// Pre-create the activeConn so we can use it in LoadOrStore below.
	activeConn := &activeWSConn{
		safeConn:    safeConn,
		conn:        conn,
		sessionID:   sessionID,
		connectedAt: time.Now(),
		closed:      make(chan struct{}),
	}

	// Atomically check for existing connection and register ourselves.
	// LoadOrStore eliminates the TOCTOU race between checking and storing.
	actualVal, loaded := ws.activeWSByUserID.LoadOrStore(trackingKey, activeConn)
	if loaded {
		// Another connection is already active. Our activeConn was NOT stored.
		existingActive := actualVal.(*activeWSConn)

		// Conflict! Notify the NEW connection that an existing session
		// is active and wait for the client to confirm takeover.
		log.Printf("[SP-118-Mode1] Session conflict for user %s: new session %s vs existing %s",
			trackingKey, sessionID, existingActive.sessionID)

		safeConn.WriteJSON(map[string]interface{}{
			"type": "session_conflict",
			"data": map[string]interface{}{
				"existing_session_id": existingActive.sessionID,
				"connected_at":        existingActive.connectedAt.Unix(),
			},
		})

		// Block here until the client either confirms takeover or disconnects.
		if !ws.waitForTakeover(conn, sessionID) {
			log.Printf("[SP-118-Mode1] New session %s disconnected without confirming takeover for user %s",
				sessionID, trackingKey)
			return
		}

		// Client confirmed — evict the old connection.
		// Use CompareAndDelete to atomically remove the old entry only if
		// it hasn't been replaced by yet another connection in the meantime.
		if ws.activeWSByUserID.CompareAndDelete(trackingKey, existingActive) {
			existingActive.safeConn.WriteJSON(map[string]interface{}{
				"type": "session_displaced",
				"data": map[string]interface{}{
					"reason":  "session_taken_over",
					"message": "This session has been moved to another device",
				},
			})
			existingActive.safeConn.Close()
			log.Printf("[SP-118-Mode1] Session %s evicted for user %s", existingActive.sessionID, trackingKey)

			// Also notify terminal WebSocket connections for the same tracking
			// key so they can show a displacement banner. Terminal sessions
			// (PTY processes) are intentionally left running — they persist
			// across disconnects by design (ring buffer + reattach). The
			// notification lets the client UI reflect the displacement without
			// forcing terminal teardown, which would break the "reopen laptop
			// and terminal is still there" UX.
			ws.notifyTerminalConnectionsDisplaced(trackingKey)
		}

		// Now store ourselves as the active connection.
		ws.activeWSByUserID.Store(trackingKey, activeConn)
	}
	// When loaded == false, LoadOrStore already stored our activeConn — nothing more to do.

	// Remove from activeWSByUserID when this connection exits.
	defer func() {
		// Only clean up if we're still the active connection for this
		// key.  During takeover, evictExistingConnection already removed
		// us and a new entry was stored; we must not delete the new entry.
		if val, ok := ws.activeWSByUserID.Load(trackingKey); ok {
			if existing, ok := val.(*activeWSConn); ok && existing == activeConn {
				ws.activeWSByUserID.Delete(trackingKey)
			}
		}
		close(activeConn.closed)
	}()

	// Store the underlying connection with metadata, subscribe to the
	// auto-chat, run the replay, and start the read/write goroutines.
	// Shared with handleWebSocket_Daemon — see runConnectionLiveLoop
	// for the Mode 1 vs Mode 2 panic-cleanup branching.
	ws.runConnectionLiveLoop(conn, safeConn, sessionID, clientID, userID, chatID, reattachChatID, afterSeq, false)
}

// runConnectionLiveLoop is the shared post-upgrade body of both
// handleWebSocket_Agent (Mode 1) and handleWebSocket_Daemon (Mode 2).
// It is responsible for:
//
//   - Storing ConnectionInfo in ws.connections so other paths (chat
//     fanout, security dialogs, diagnostics) can find this socket.
//   - Subscribing to chatSubscribers for the auto-chat (replay fanout).
//   - Replaying buffered chat events when reattachChatID is set.
//   - Starting the read goroutine (parses incoming WS frames).
//   - Running the write loop (drains events, coalesces stream chunks,
//     forwards through shouldForwardEventToConnection).
//
// The only difference between the two modes is which cleanup runs on
// panic in the read goroutine: Mode 1 calls cleanupAfterPanicAgent
// (nukes the whole clientID's state — safe because there's only one
// window); Mode 2 calls cleanupAfterPanicSession (scoped to this
// session, plus clientID-clear only when this was the last window for
// the user). The choice is signalled via the `daemon` bool: true →
// Mode 2, false → Mode 1.
//
// Parameters:
//   - conn, safeConn, sessionID, clientID, userID, chatID, reattachChatID,
//     afterSeq: same fields as the original handleWebSocket entry point.
//   - daemon: true for handleWebSocket_Daemon; false for handleWebSocket_Agent.
func (ws *ReactWebServer) runConnectionLiveLoop(
	conn *websocket.Conn,
	safeConn *SafeConn,
	sessionID, clientID, userID, chatID, reattachChatID string,
	afterSeq int64,
	daemon bool,
) {
	// Store the underlying connection with metadata
	ws.connections.Store(conn, &ConnectionInfo{
		SessionID:          sessionID,
		ClientID:           clientID,
		ChatID:             chatID,
		Type:               "webui",
		UserID:             userID,
		ConnectedAt:        time.Now(),
		Conn:               conn,
		SafeConn:           safeConn, // shared write mutex for cross-connection notifications
		subscribedChannels: make(map[string]bool),
	})
	defer ws.connections.Delete(conn)

	// A fresh connection means the client is back (e.g. a backgrounded tab
	// returned to the foreground). Clear any paused state so normal
	// heartbeat-based cancellation resumes.
	ws.setClientPaused(clientID, false)

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

	// Subscribe to events BEFORE replay so live events published during the
	// replay window are captured instead of being lost. The EventBus channel
	// is buffered (1024), so it absorbs any burst without blocking.
	eventCh := ws.eventBus.Subscribe(sessionID)
	defer ws.eventBus.Unsubscribe(sessionID)

	// Replay any missed events before we start the live loop.
	// Because we subscribed above, any live events published during replay
	// land in eventCh. We capture them in a drain goroutine and flush them
	// after the replay batch so the client sees buffered events first, then
	// live ones — preserving the invariant that seq N+3 (replay) arrives
	// before seq N+5 (live).
	if reattachChatID != "" {
		// Drain goroutine: captures live events that arrive during replay.
		var capturedMu sync.Mutex
		var capturedEvents []events.UIEvent
		drainStop := make(chan struct{})
		drainDone := make(chan struct{})
		go func() {
			defer close(drainDone)
			for {
				select {
				case <-drainStop:
					return
				case ev := <-eventCh:
					capturedMu.Lock()
					capturedEvents = append(capturedEvents, ev)
					capturedMu.Unlock()
				}
			}
		}()

		ws.deliverChatRunReplay(safeConn, clientID, reattachChatID, afterSeq)

		// Stop the drain goroutine and flush captured live events.
		close(drainStop)
		<-drainDone

		// Get connInfo for filtering captured events.
		connInfoVal, ok := ws.connections.Load(conn)
		var connInfo *ConnectionInfo
		if ok {
			connInfo, _ = connInfoVal.(*ConnectionInfo)
		}

		capturedMu.Lock()
		captured := capturedEvents
		capturedMu.Unlock()
		for _, ev := range captured {
			if connInfo != nil && !ws.shouldForwardEventToConnection(ev, connInfo) {
				continue
			}
			if err := safeConn.WriteJSON(ev); err != nil {
				log.Printf("WebSocket %s write error flushing captured events: %v", sessionID, err)
				return
			}
		}
	}

	// Set up close handler to send disconnect status
	conn.SetCloseHandler(func(code int, text string) error {
		log.Printf("WebSocket %s closing with code %d: %s", sessionID, code, text)
		return nil
	})

	// Use separate goroutines for reading and writing
	// This is the standard pattern for bidirectional WebSocket communication
	ctx, cancel := context.WithCancel(context.Background())
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
				// Mode-specific cleanup. Mode 1 (sprout agent) nukes the
				// whole clientID; Mode 2 (daemon) only clears this
				// session. See cleanupAfterPanicAgent / cleanupAfterPanicSession.
				if daemon {
					ws.cleanupAfterPanicSession(clientID, userID, chatID, sessionID)
				} else {
					ws.cleanupAfterPanicAgent(clientID, sessionID)
				}
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
				ws.handleWebSocketMessage(safeConn, sessionID, msg, clientID, userID, chatID, daemon)
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

			// Opportunistically drain any already-queued events (non-blocking)
			// and coalesce runs of adjacent stream chunks before writing. Under
			// a backlog — the only time stream chunks get dropped — this turns
			// hundreds of tiny writes into a few, letting the channel drain fast
			// instead of overflowing. With no backlog the drain pulls nothing,
			// so streaming latency is unchanged.
			batch := []events.UIEvent{event}
		drain:
			for len(batch) < maxCoalesceDrain {
				select {
				case e2 := <-eventCh:
					batch = append(batch, e2)
				default:
					break drain
				}
			}

			for _, ev := range coalesceStreamChunks(batch) {
				if !ws.shouldForwardEventToConnection(ev, connInfo) {
					continue
				}
				if ev.Type == events.EventTypeSecurityApprovalRequest {
					if data, ok := ev.Data.(map[string]interface{}); ok {
						log.Printf("[SECURITY] Forwarding security_approval_request to client %s: request_id=%v tool=%s risk=%s",
							connInfo.ClientID, data["request_id"], data["tool_name"], data["risk_level"])
					}
				}
				if ev.Type == events.EventTypeAskUserRequest {
					if data, ok := ev.Data.(map[string]interface{}); ok {
						log.Printf("[ASK_USER] Forwarding ask_user_request to client %s: request_id=%v question=%q",
							connInfo.ClientID, data["request_id"], data["question"])
					}
				}
				if err := safeConn.WriteJSON(ev); err != nil {
					log.Printf("WebSocket %s write error: %v", sessionID, err)
					return
				}
			}

		case <-readDone:
			// Read goroutine has exited
			return
		}
	}
}

// cleanupAfterPanicSession is the Mode 2 / daemon-side panic cleanup.
// It bounds the blast radius by what could plausibly share state with
// the panicked session:
//
//  1. Drop this session from the userConnections registry (so future
//     reads/writes don't try to use a half-closed socket).
//  2. Clear THIS session's chat query state — only the chat that
//     panicked (chatID), not the client's other chats.
//  3. Clear cached agents for clientID only when this was the LAST
//     window for the user (Count(userID) <= 1). The MCP-manager /
//     conversation-history corruption defense from cleanupAfterPanicAgent
//     still applies when the user has only one window; with multiple
//     windows open, we trust the other windows' agents are independent.
//
// When userID is empty (local mode), trackingKey falls back to clientID
// and the count check uses that, preserving Mode-1 parity for the
// single-window case.
func (ws *ReactWebServer) cleanupAfterPanicSession(clientID, userID, chatID, sessionID string) {
	if strings.TrimSpace(clientID) == "" {
		return
	}

	trackingKey := userID
	if trackingKey == "" {
		trackingKey = clientID
	}

	// 1. Decrement top-level active query counter for this client.
	//    Even when multiple windows are open, the counter is per-clientID,
	//    so this remains the right level of accounting.
	ws.decrementActiveQueries(clientID)

	// 2. Reset per-chat session query state — but only for the chat
	//    tied to this session, not the whole clientID. Other windows on
	//    the same clientID with their own chats are unaffected.
	ws.mutex.Lock()
	if ctx := ws.clientContexts[clientID]; ctx != nil && chatID != "" {
		if cs, ok := ctx.ChatSessions[chatID]; ok && cs != nil {
			cs.setQueryActive(false, "")
		}
	}
	ws.mutex.Unlock()

	// 3. Cached-agent clear is gated on Count(trackingKey) <= 1. With
	//    only one window open, the original Mode-1 corruption defense
	//    still applies — the agent might be half-initialized. With
	//    multiple windows, we trust the other windows to retain working
	//    agents and skip the clear so siblings don't lose their in-flight
	//    query state.
	if ws.userConnections != nil && ws.userConnections.Count(trackingKey) <= 1 {
		ws.clearCachedAgent(clientID)
	}

	// 4. Publish session_terminated event so the panicked client can
	//    tear down UI. Other windows on the same clientID are NOT
	//    notified — they continue serving the user.
	ws.publishClientEvent(clientID, events.EventTypeSessionTerminated, map[string]interface{}{
		"session_id": sessionID,
		"status":     "error",
		"code":       "internal_panic",
		"message":    "Session terminated due to internal error",
	})
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

	// SP-065-2e: Automate events require explicit channel subscription.
	// Only forward automate.* events to connections that have opted in
	// via {type: "subscribe", data: {channel: "automate"}}.
	if strings.HasPrefix(event.Type, "automate.") {
		if !connInfo.isSubscribedToChannel("automate") {
			return false
		}
		// Connection has opted in — allow the event. Automate events
		// don't carry client_id/chat_id targeting, so they'd otherwise
		// be rejected by the global event type switch below.
		return true
	}

	// Extract target client_id and chat_id from event
	targetClientID, _ := data["client_id"].(string)
	targetChatID, _ := data["chat_id"].(string)

	// Check if event has client_id targeting
	if strings.TrimSpace(targetClientID) != "" {
		// Event has explicit client_id - must match connection's client_id
		// OR (SP-034-3c) the connection must be subscribed to the event's
		// chat for multi-tab consistency. Security/interaction events still
		// require clientID match because they're authenticating a specific
		// browser session, not broadcasting state.
		if strings.TrimSpace(targetClientID) != strings.TrimSpace(connInfo.ClientID) {
			if isSecurityScopedEvent(event.Type) {
				log.Printf("[SECURITY] Dropping %s event: payload client_id=%q does not match connection client_id=%q (request_id=%v)",
					event.Type, strings.TrimSpace(targetClientID), connInfo.ClientID, data["request_id"])
				return false
			}
			// Allow on multi-tab match: either this connection's primary
			// chat_id matches the event's chat_id, or the connection has
			// explicitly subscribed to the chat via the chatSubscribers
			// registry. Either way, the same chat is open on this tab and
			// the event belongs on its screen.
			targetChat := strings.TrimSpace(targetChatID)
			if targetChat == "" {
				return false // clientID mismatch and no chat scope → drop
			}
			if strings.TrimSpace(connInfo.ChatID) != targetChat &&
				!ws.connectionSubscribedToChat(connInfo, targetChat) {
				return false
			}
			return true
		}
		// Client ID matches, now check chat_id if present
		if strings.TrimSpace(targetChatID) != "" {
			targetChat := strings.TrimSpace(targetChatID)
			// Event has chat_id - connection must match, be unfiltered, or be subscribed.
			// Security-scoped events are strict: they only allow when the connection's
			// primary chat_id matches (or is unfiltered), NOT via chatSubscribers.
			if isSecurityScopedEvent(event.Type) {
				if strings.TrimSpace(connInfo.ChatID) != "" && strings.TrimSpace(connInfo.ChatID) != targetChat {
					return false
				}
			} else {
				// For normal events: allow if connection has no specific chat,
				// its primary chat matches, or it has explicitly subscribed to
				// the target chat (multi-chat switch over persistent WS).
				if strings.TrimSpace(connInfo.ChatID) != "" &&
					strings.TrimSpace(connInfo.ChatID) != targetChat &&
					!ws.connectionSubscribedToChat(connInfo, targetChat) {
					return false
				}
			}
		}
		return true
	}

	// No client_id in event - check chat_id targeting
	if strings.TrimSpace(targetChatID) != "" {
		targetChat := strings.TrimSpace(targetChatID)
		// Event has chat_id but no client_id
		// Forward if connection has matching chat_id, no specific chat, or is subscribed.
		if strings.TrimSpace(connInfo.ChatID) != "" &&
			strings.TrimSpace(connInfo.ChatID) != targetChat &&
			!ws.connectionSubscribedToChat(connInfo, targetChat) {
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

// handleWebSocketMessage processes incoming WebSocket messages.
//
// The `daemon`, `userID`, and `chatID` parameters drive Mode-2-aware
// panic cleanup in safeHandleGoroutine below: a panic in a Mode-2
// message handler must not invalidate sibling windows on the same
// clientID. Tests that pre-date SP-118 pass empty strings / false
// for these and continue to behave as Mode 1.
func (ws *ReactWebServer) handleWebSocketMessage(safeConn *SafeConn, sessionID string, msg *WebSocketMessage, clientID, userID, chatID string, daemon bool) {
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

	case AllowedMessageTypeHeartbeat:
		ws.handleHeartbeatMessage(safeConn, clientID)

	case AllowedMessageTypePause:
		// Tab backgrounded — keep any in-flight query running in the background
		// instead of letting the heartbeat monitor cancel it on staleness.
		log.Printf("[lifecycle] client %s paused (backgrounded) — keeping any active query alive", clientID)
		ws.setClientPaused(clientID, true)

	case AllowedMessageTypeResume:
		// Tab foregrounded — resume normal heartbeat-based cancellation.
		ws.setClientPaused(clientID, false)

	case AllowedMessageTypeSessionClose:
		// Tab closing/navigating away — cancel the in-flight query now rather
		// than waiting out the heartbeat timeout.
		ws.setClientPaused(clientID, false)
		ws.cancelQueryForClient(clientID, "session_closed", "Query cancelled: the Web UI was closed")

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
		log.Printf("WebSocket client subscribed to events: %v chat_ids: %v channel: %s", data.Events, data.ChatIDs, data.Channel)

		// Register chat subscriptions so events for these chats fan out
		// to this connection even when the originating clientID differs
		// (e.g. same chat open in two browser tabs).
		if ws.chatSubscribers != nil {
			for _, chatID := range data.ChatIDs {
				ws.chatSubscribers.Subscribe(chatID, safeConn.Conn())
			}
		}

		// SP-065-2e: Register channel subscriptions (e.g., "automate")
		// so automate events are only forwarded to connections that
		// explicitly opted in.
		if data.Channel != "" {
			connInfoVal, ok := ws.connections.Load(safeConn.Conn())
			if ok {
				if ci, ok := connInfoVal.(*ConnectionInfo); ok {
					ci.subscribeToChannel(data.Channel)
				}
			}
		}

	case AllowedMessageTypeRequestStats:
		// Send current stats immediately
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
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
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
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
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
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
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
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
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
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
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
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
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
			ws.handleAskUserResponse(safeConn, data, clientID)
		})

	case AllowedMessageTypeHydrateRequest:
		// SP-046: client requests cold-hydrate of workspace files.
		// Runs in a goroutine so the read loop stays responsive.
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
			ws.handleColdHydrateRequest(safeConn, ws.getWorkspaceRootForClient(clientID))
		})

	case AllowedMessageTypeSyncRecover:
		// SP-046: client requests sync recovery after container death or browser crash.
		// Runs in a goroutine so the read loop stays responsive.
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
			ws.handleSyncRecoverMessage(safeConn, sessionID, msg, clientID)
		})

	case AllowedMessageTypeSessionTakeover:
		// SP-046: session_takeover is expected only during the conflict
		// wait loop. If it arrives during normal message dispatch, log
		// and ignore — there is nothing to do.
		log.Printf("[SP-118-Mode1] session_takeover received for session %s outside of conflict state, ignoring", sessionID)
	}
}

// safeHandleGoroutine runs fn in a goroutine with panic recovery. If fn
// panics, an error event is written to the WebSocket, the active query
// state is reset (mode-appropriate blast radius), and the connection
// is closed.
//
// The userID, chatID, and daemon bool are used to dispatch the right
// cleanup function on panic:
//   - daemon=false (Mode 1 / sprout agent): cleanupAfterPanicAgent
//     nukes the whole clientID's state — safe because there is only
//     one window per user.
//   - daemon=true (Mode 2 / daemon): cleanupAfterPanicSession scopes
//     the clear to this session + clientID-clear only when this is
//     the last window for the user.
func (ws *ReactWebServer) safeHandleGoroutine(safeConn *SafeConn, sessionID, clientID, userID, chatID string, daemon bool, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("WebSocket handler panic in session %s: %v", sessionID, r)
				safeConn.WritePanicError(sessionID, "message handler", r)
				if daemon {
					ws.cleanupAfterPanicSession(clientID, userID, chatID, sessionID)
				} else {
					ws.cleanupAfterPanicAgent(clientID, sessionID)
				}
				safeConn.Close() // Terminate the session since state is unreliable after a panic
			}
		}()
		fn()
	}()
}

// cleanupAfterPanicAgent resets the client's query state in Mode 1
// (single-session). This is the Mode 1 / SP-118-Mode1 path: only one
// browser window is active per user, so clearing the full clientID's
// state is safe and correct.
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
func (ws *ReactWebServer) cleanupAfterPanicAgent(clientID, sessionID string) {
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

// waitForTakeover blocks on the raw WebSocket connection waiting for a
// session_takeover message from the client. This is used during conflict
// resolution (SP-046) — the new connection is in a "pending" state where
// only the takeover message is accepted. Returns true if the client
// confirmed takeover, false if it disconnected without confirming.
func (ws *ReactWebServer) waitForTakeover(conn *websocket.Conn, sessionID string) bool {
	// Limit frame size to prevent a malicious client from sending a
	// multi-gigabyte WebSocket frame during the takeover window.
	conn.SetReadLimit(512 * 1024)
	// Set a generous read deadline so the client has time to decide.
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, rawMsg, err := conn.ReadMessage()
	if err != nil {
		return false
	}

	msg, err := parseAndValidateMessage(rawMsg)
	if err != nil {
		log.Printf("[SP-118-Mode1] Session %s: invalid message during takeover wait: %v", sessionID, err)
		return false
	}

	if msg.Type != AllowedMessageTypeSessionTakeover {
		log.Printf("[SP-118-Mode1] Session %s: unexpected message type %q during takeover wait (expected %q)",
			sessionID, msg.Type, AllowedMessageTypeSessionTakeover)
		return false
	}

	log.Printf("[SP-118-Mode1] Session %s confirmed takeover", sessionID)
	return true
}

// evictExistingConnection sends a session_displaced message to the
// current active connection for the given key, closes it, and removes
// it from the active registry. Returns true if eviction happened,
// false if no existing connection was found.
func (ws *ReactWebServer) evictExistingConnection(trackingKey string) bool {
	val, loaded := ws.activeWSByUserID.LoadAndDelete(trackingKey)
	if !loaded {
		return false
	}
	active, ok := val.(*activeWSConn)
	if !ok {
		log.Printf("[SP-118-Mode1] unexpected type in activeWSByUserID for key %s", trackingKey)
		return false
	}

	// Send displacement notification, then close.
	active.safeConn.WriteJSON(map[string]interface{}{
		"type": "session_displaced",
		"data": map[string]interface{}{
			"reason":  "session_taken_over",
			"message": "This session has been moved to another device",
		},
	})
	active.safeConn.Close()

	log.Printf("[SP-118-Mode1] Session %s evicted for user %s", active.sessionID, trackingKey)
	return true
}

// handleSyncRecoverMessage processes a sync_recover message from the browser
// after container death or browser crash recovery.
func (ws *ReactWebServer) handleSyncRecoverMessage(safeConn *SafeConn, sessionID string, msg *WebSocketMessage, clientID string) {
	// Unmarshal the data payload
	var data map[string]interface{}
	if len(msg.Data) == 0 {
		log.Printf("[SP-118-Mode1] sync_recover: empty data from %s", sessionID)
		safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "invalid sync_recover data"},
		})
		return
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		log.Printf("[SP-118-Mode1] sync_recover: invalid JSON from %s: %v", sessionID, err)
		safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "invalid sync_recover data"},
		})
		return
	}

	// Extract browser seq map from data
	seqsRaw, ok := data["seqs"]
	if !ok {
		log.Printf("[SP-118-Mode1] sync_recover: missing seqs from %s", sessionID)
		safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "missing seqs in sync_recover"},
		})
		return
	}

	seqsMap, ok := seqsRaw.(map[string]interface{})
	if !ok {
		log.Printf("[SP-118-Mode1] sync_recover: seqs is not a map from %s", sessionID)
		safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "seqs must be a map"},
		})
		return
	}

	browserSeqs := make(map[string]int64)
	for path, seqVal := range seqsMap {
		switch v := seqVal.(type) {
		case float64:
			browserSeqs[path] = int64(v)
		case int64:
			browserSeqs[path] = v
		default:
			log.Printf("[SP-118-Mode1] sync_recover: unexpected seq type for %s: %T", path, seqVal)
		}
	}

	log.Printf("[SP-118-Mode1] sync_recover from client %s: %d files", clientID, len(browserSeqs))

	// Run container death recovery with per-file seqs
	result, err := ws.HandleContainerRecoveryWithSeqs(context.Background(), clientID, browserSeqs)
	if err != nil {
		log.Printf("[SP-118-Mode1] sync_recover reconciliation failed: %v", err)
		safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": fmt.Sprintf("reconciliation failed: %v", err)},
		})
		return
	}

	// Send reconciliation plan back to browser
	if err := ws.SendSyncReconcile(safeConn, result); err != nil {
		log.Printf("[SP-118-Mode1] sync_recover: failed to send reconcile plan: %v", err)
		return
	}

	// For container_ahead files, replay the patches
	filesToReplay := 0
	for _, action := range result.Plan {
		if action.Action == "container_ahead" {
			filesToReplay++
		}
	}

	if filesToReplay > 0 {
		ag, err := ws.getClientAgent(clientID)
		if err != nil {
			log.Printf("[SP-118-Mode1] sync_recover: failed to get agent for %s: %v", clientID, err)
			return
		}
		if err := ws.SendSyncReplayStart(safeConn, clientID, filesToReplay); err != nil {
			log.Printf("[SP-118-Mode1] sync_recover: failed to send replay start: %v", err)
			return
		}

		for _, action := range result.Plan {
			if action.Action != "container_ahead" {
				continue
			}
			// Validate path to prevent traversal attacks
			if filepath.IsAbs(action.FilePath) || strings.Contains(action.FilePath, "..") {
				log.Printf("[SP-118-Mode1] sync_recover: skipping invalid path: %s", action.FilePath)
				continue
			}
			// Read the file content from container
			content, err := ag.ReadFileContent(action.FilePath)
			if err != nil {
				log.Printf("[SP-118-Mode1] sync_recover: failed to read %s: %v", action.FilePath, err)
				continue
			}
			if err := ws.SendSyncReplayFile(safeConn, clientID, action.FilePath, content, action.ContainerSeq); err != nil {
				log.Printf("[SP-118-Mode1] sync_recover: failed to replay %s: %v", action.FilePath, err)
				return
			}
		}

		if err := ws.SendSyncReplayComplete(safeConn, clientID); err != nil {
			log.Printf("[SP-118-Mode1] sync_recover: failed to send replay complete: %v", err)
		}
	}

	log.Printf("[SP-118-Mode1] sync_recover complete for client %s: %d files reconciled", clientID, len(result.Plan))
}

// notifyTerminalConnectionsDisplaced sends a session_displaced message to
// all terminal WebSocket connections matching the given tracking key (user ID
// or client ID). This is called when a chat session is taken over (SP-046) so
// that terminal tabs on the displaced device can show a banner instead of
// silently continuing as if nothing happened. Terminal PTY processes are NOT
// closed — they persist across disconnects by design.
//
// Mode 2 (daemon, sprout service) is a no-op: takeover does not happen in
// Mode 2 because there is no single-active-session enforcement. The handler
// checks `ws.agentEnforceSingleSession` so this function remains a safe
// call site from Mode 1 paths without leaking displacement messages into
// the multi-session daemon.
func (ws *ReactWebServer) notifyTerminalConnectionsDisplaced(trackingKey string) {
	if !ws.agentEnforceSingleSession {
		// Mode 2: no displacement event — terminal tabs persist by design.
		return
	}
	displacedMsg := map[string]interface{}{
		"type": "session_displaced",
		"data": map[string]interface{}{
			"reason":  "session_taken_over",
			"message": "This session has been moved to another device",
		},
	}
	ws.connections.Range(func(conn, value interface{}) bool {
		info, ok := value.(*ConnectionInfo)
		if !ok || info == nil || info.Type != "terminal" {
			return true
		}
		// Match by UserID (service mode) or ClientID (local mode), whichever
		// the tracking key represents.
		if info.UserID == trackingKey || info.ClientID == trackingKey {
			// Use the shared SafeConn (same mutex as the owning handler's
			// goroutine) to avoid concurrent-write panics. Creating a new
			// SafeConn here would have a separate mutex from the terminal
			// handler's write loop, racing on the underlying *websocket.Conn.
			if info.SafeConn != nil {
				info.SafeConn.WriteJSON(displacedMsg)
			}
		}
		return true
	})
}
