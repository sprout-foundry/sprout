//go:build !js

package webui

// websocket_handler_live_loop.go — the shared post-upgrade live loop
// (split from websocket_handler_mode.go). runConnectionLiveLoop is the
// post-upgrade body shared by both Mode 1 (handleWebSocket_Agent) and
// Mode 2 (handleWebSocket_Daemon): connection storage, auto-chat
// subscribe, replay drain, read goroutine, and the write loop that
// coalesces stream chunks.
import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sprout-foundry/sprout/pkg/events"
)

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

	ws.log().Info("WebSocket connected", slog.String("session_id", sessionID))
	if userID != "" {
		ws.log().Info("WebSocket user identified", slog.String("user_id", userID), slog.String("session_id", sessionID))
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
			defer func() {
				if r := recover(); r != nil {
					ws.log().Error("drain goroutine panicked", slog.String("session_id", sessionID), slog.Any("panic", r))
				}
			}()
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
				ws.log().Error("WebSocket captured event flush failed", slog.String("session_id", sessionID), slog.Any("err", err))
				return
			}
		}
	}

	// Set up close handler to send disconnect status
	conn.SetCloseHandler(func(code int, text string) error {
		ws.log().Info("WebSocket closing", slog.String("session_id", sessionID), slog.Int("code", code), slog.String("reason", text))
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
				ws.log().Error("WebSocket read goroutine panicked", slog.String("session_id", sessionID), slog.Any("panic", r))
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
						ws.log().Info("WebSocket closed", slog.String("session_id", sessionID), slog.Any("err", err))
					} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						// If no message received in 180 seconds (3 minutes), connection is dead.
						// Chrome pauses background tabs aggressively, freezing timers and
						// throttling network. 3 minutes gives enough time for the pong
						// watchdog on the client side to detect the issue and proactively
						// reconnect before the server kills the connection.
						if time.Since(lastMessage) > 180*time.Second {
							ws.log().Warn("WebSocket inactive; closing", slog.String("session_id", sessionID), slog.Duration("inactivity", 180*time.Second))
							return
						}
						// Heartbeat timeout, send ping
						if err := safeConn.WriteJSON(map[string]interface{}{
							"type": "ping",
							"data": map[string]interface{}{"timestamp": time.Now().Unix()},
						}); err != nil {
							ws.log().Error("WebSocket ping failed", slog.String("session_id", sessionID), slog.Any("err", err))
							return
						}
						continue
					} else {
						ws.log().Error("WebSocket read failed", slog.String("session_id", sessionID), slog.Any("err", err))
					}
					return
				}

				// Validate the incoming message
				msg, err := parseAndValidateMessage(rawMsg)
				if err != nil {
					ws.log().Warn("WebSocket message validation failed", slog.String("session_id", sessionID), slog.Any("err", err))
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
			ws.log().Debug("WebSocket context cancelled", slog.String("session_id", sessionID))
			return

		case event := <-eventCh:
			// Get connection info for this connection
			connInfoVal, ok := ws.connections.Load(conn)
			if !ok {
				ws.log().Warn("WebSocket connection info not found; skipping event", slog.String("session_id", sessionID))
				continue
			}
			connInfo, ok := connInfoVal.(*ConnectionInfo)
			if !ok {
				ws.log().Error("WebSocket connection info type mismatch; skipping event", slog.String("session_id", sessionID))
				continue
			}

			// Opportunistically drain any already-queued events (non-blocking)
			// and coalesce before writing: runs of adjacent progress_milestone
			// events collapse into a single batched event (SP-151 §151b), and
			// runs of adjacent stream chunks merge into larger writes. Under a
			// backlog — the only time stream chunks get dropped — this turns
			// hundreds of tiny writes into a few, letting the channel drain fast
			// instead of overflowing. With no backlog the drain pulls nothing,
			// so streaming latency is unchanged. The two coalescers target
			// disjoint event types (milestone vs stream_chunk) so applying them
			// in series is safe; both preserve the relative order of the other
			// events.
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

			for _, ev := range coalesceStreamChunks(coalesceProgressMilestones(batch)) {
				if !ws.shouldForwardEventToConnection(ev, connInfo) {
					continue
				}
				if ev.Type == events.EventTypeSecurityApprovalRequest {
					if data, ok := ev.Data.(map[string]interface{}); ok {
						ws.log().Debug("forwarding security approval request", slog.String("client_id", connInfo.ClientID), slog.Any("request_id", data["request_id"]), slog.Any("tool_name", data["tool_name"]), slog.Any("risk_level", data["risk_level"]))
					}
				}
				if ev.Type == events.EventTypeAskUserRequest {
					if data, ok := ev.Data.(map[string]interface{}); ok {
						ws.log().Debug("forwarding ask user request", slog.String("client_id", connInfo.ClientID), slog.Any("request_id", data["request_id"]), slog.Any("question", data["question"]))
					}
				}
				if err := safeConn.WriteJSON(ev); err != nil {
					ws.log().Error("WebSocket write failed", slog.String("session_id", sessionID), slog.Any("err", err))
					return
				}
			}

		case <-readDone:
			// Read goroutine has exited
			return
		}
	}
}
