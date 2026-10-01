//go:build !js

package webui

// Package webui: WebSocket mode handlers (split from websocket_handler.go).
// The shared post-upgrade live loop lives in websocket_handler_live_loop.go.

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
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
		ws.log().Error("WebSocket upgrade failed", slog.String("mode", "daemon"), slog.Any("err", err))
		return
	}

	safeConn := NewSafeConn(conn)
	defer safeConn.Close()

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		ws.log().Error("WebSocket session ID generation failed", slog.String("mode", "daemon"), slog.Any("err", err))
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
			ws.log().Error("WebSocket handler panicked", slog.String("mode", "daemon"), slog.String("session_id", sessionID), slog.Any("panic", r))
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
	ws.log().Info("WebSocket connection accepted", slog.String("mode", "daemon"), slog.String("tracking_key", trackingKey), slog.String("session_id", sessionID), slog.Int("connection_count", ws.userConnections.Count(trackingKey)))

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
		ws.log().Error("WebSocket upgrade failed", slog.String("mode", "agent"), slog.Any("err", err))
		return
	}

	// Wrap connection in SafeConn to prevent concurrent write panics
	safeConn := NewSafeConn(conn)
	defer safeConn.Close()

	// Generate unique session ID for this connection using cryptographically secure random
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		ws.log().Error("WebSocket session ID generation failed", slog.String("mode", "agent"), slog.Any("err", err))
		conn.Close()
		return
	}
	sessionID := "ws_" + hex.EncodeToString(b)
	clientID := ws.resolveClientID(r)

	// Panic recovery for the main handler - moved here so safeConn and sessionID are available
	defer func() {
		if r := recover(); r != nil {
			ws.log().Error("WebSocket handler panicked", slog.String("mode", "agent"), slog.String("session_id", sessionID), slog.Any("panic", r))
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
		ws.log().Warn("WebSocket session conflict", slog.String("tracking_key", trackingKey), slog.String("new_session_id", sessionID), slog.String("existing_session_id", existingActive.sessionID))

		safeConn.WriteJSON(map[string]interface{}{
			"type": "session_conflict",
			"data": map[string]interface{}{
				"existing_session_id": existingActive.sessionID,
				"connected_at":        existingActive.connectedAt.Unix(),
			},
		})

		// Block here until the client either confirms takeover or disconnects.
		if !ws.waitForTakeover(conn, sessionID) {
			ws.log().Info("WebSocket disconnected without confirming takeover", slog.String("session_id", sessionID), slog.String("tracking_key", trackingKey))
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
			ws.log().Info("WebSocket session evicted", slog.String("session_id", existingActive.sessionID), slog.String("tracking_key", trackingKey))

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
