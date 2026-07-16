//go:build !js

package webui

// Package webui: WebSocket mode handlers and shared connection live loop (split from websocket_handler.go)

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sprout-foundry/sprout/pkg/events"
)

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
//
//	handleWebSocket (entry) reads `agentEnforceSingleSession` and
//	forwards here when false. The dispatcher is the ONLY dispatch point;
//	internal callers use one of the two mode handlers directly.
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
