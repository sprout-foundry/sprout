//go:build !js

package webui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// generateTerminalSessionID creates a collision-resistant terminal session ID
// using 8 bytes of cryptographic randomness (64 bits). This avoids the
// collision risk of time-based IDs when two terminals are created in the same
// nanosecond (possible on fast machines or during rapid reconnect cycles).
func generateTerminalSessionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely — crypto/rand should never fail on a healthy
		// system. Fall back to time-based uniqueness as a last resort.
		return fmt.Sprintf("terminal_%d", time.Now().UnixNano())
	}
	return "terminal_" + hex.EncodeToString(b)
}

// handleTerminalWebSocket handles terminal WebSocket connections.
// Supports both creating new sessions and reattaching to existing sessions.
// The client can request reattachment by passing ?reattach=<sessionID> in the URL.
func (ws *ReactWebServer) handleTerminalWebSocket(w http.ResponseWriter, r *http.Request) {
	// Generate a session ID early so it's available for panic recovery
	sessionID := generateTerminalSessionID()

	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		ws.log().Error("terminal WebSocket upgrade failed", slog.Any("err", err))
		return
	}

	// Wrap connection in SafeConn to prevent concurrent write panics
	safeConn := NewSafeConn(conn)
	defer safeConn.Close()

	// Resolve client ID for panic cleanup (terminal connections typically have empty clientID)
	clientID := ws.resolveClientID(r)

	// Panic recovery - now safeConn is available
	defer func() {
		if r := recover(); r != nil {
			ws.log().Error("terminal WebSocket handler panicked", slog.String("session_id", sessionID), slog.Any("panic", r))
			safeConn.WritePanicError(sessionID, "terminal handler", r)
			ws.cleanupAfterPanicAgent(clientID, sessionID)
		}
	}()

	terminalManager := ws.getTerminalManagerForRequest(r)

	// Check if client wants to reattach to an existing session
	reattachID := strings.TrimSpace(r.URL.Query().Get("reattach"))
	var session *TerminalSession

	if reattachID != "" && terminalManager.HasVisibleSession(reattachID) {
		// Reattach: snapshot ring buffer for scrollback replay
		scrollback, err := terminalManager.ReattachSession(reattachID)
		if err != nil {
			ws.log().Warn("terminal session reattach failed; creating new session", slog.String("session_id", reattachID), slog.Any("err", err))
			// Fall through to create new session
		} else {
			sessionID = reattachID
			// GetSession's bool return is load-bearing: between a successful
			// ReattachSession and this call, the session can be torn down by
			// its background timeout goroutine. Discarding the bool would leave
			// `session` nil and crash on the subscribe() call below.
			var exists bool
			session, exists = terminalManager.GetSession(sessionID)
			if !exists || session == nil {
				ws.log().Warn("terminal session disappeared during reattach; creating new session", slog.String("session_id", sessionID))
				session = nil
				sessionID = ""
				// Fall through to create new session
			} else {
				// Send session_restored message with scrollback
				if err := safeConn.WriteJSON(map[string]interface{}{
					"type": "session_restored",
					"data": map[string]interface{}{
						"session_id": sessionID,
						"scrollback": scrollback,
					},
				}); err != nil {
					ws.log().Error("terminal session restoration message send failed", slog.String("session_id", sessionID), slog.Any("err", err))
				} else {
					ws.log().Info("terminal session reattached", slog.String("session_id", sessionID), slog.Int("scrollback_bytes", len(scrollback)))
				}
			}
		}
	}

	// Create new session if not reattaching
	if session == nil {
		sessionID = generateTerminalSessionID()
		ws.log().Info("terminal WebSocket connection starting", slog.String("session_id", sessionID))

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
				ws.log().Warn("terminal shell override unavailable; ignoring", slog.String("session_id", sessionID), slog.String("shell", shellOverride))
				shellOverride = ""
			}
		}
		session, err = terminalManager.CreateSession(sessionID, shellOverride)
		if err != nil {
			ws.log().Error("terminal session creation failed", slog.Any("err", err))
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

		if err := safeConn.WriteJSON(msg); err != nil {
			// Distinguish "dropped by the outbound allowlist" from a
			// real transport failure. The first is a server-side
			// configuration bug (missing allowlist entry) that strands
			// the client on a loading state; the second is a network
			// problem worth a higher-severity log. Without this split
			// every drop logged as "FAILED" looked like flaky network
			// and every successful send logged the same line as a
			// silently-dropped one, both misleading.
			if errors.Is(err, ErrOutboundDropped) {
				ws.log().Error("terminal session creation message dropped", slog.String("session_id", sessionID), slog.String("remediation", "check allowedOutboundMessageTypes registry"))
			} else {
				ws.log().Error("terminal session creation message send failed", slog.String("session_id", sessionID), slog.Any("err", err))
			}
		}
	}

	// Store the underlying connection with metadata
	// Terminal connections don't use chat_id
	ws.connections.Store(conn, &ConnectionInfo{
		SessionID:   sessionID,
		ClientID:    "",
		ChatID:      "",
		Type:        "terminal",
		UserID:      ws.ExtractUserID(r),
		ConnectedAt: time.Now(),
		SafeConn:    safeConn, // shared write mutex for cross-connection notifications
	})
	defer ws.connections.Delete(conn)

	// Use context for proper cleanup coordination
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Write done channel for output sync
	writeDone := make(chan struct{})

	// Subscribe to the session's output stream. The subscription is removed
	// when the output writer goroutine exits (via defer session.unsubscribe).
	sub := session.subscribe()

	// Output writer goroutine - reads from the subscription channel and writes to WebSocket.
	// The shell process keeps running after this goroutine exits; the client can reconnect
	// and receive a ring-buffer replay of the output it missed.
	go func() {
		defer close(writeDone)
		defer session.unsubscribe(sub)
		defer func() {
			if r := recover(); r != nil {
				ws.log().Error("terminal output writer panicked", slog.String("session_id", sessionID), slog.Any("panic", r))
				safeConn.WritePanicError(sessionID, "terminal output writer", r)
				cancel()
			}
		}()

		for {
			select {
			case <-ctx.Done():
				ws.log().Debug("terminal output writer stopped", slog.String("session_id", sessionID), slog.String("reason", "context cancelled"))
				return

			case output, ok := <-sub.ch:
				if !ok {
					// Channel closed: either the PTY process exited or the subscriber
					// buffer overflowed. Check which case it is.
					session.mutex.RLock()
					active := session.Active
					session.mutex.RUnlock()
					if !active {
						ws.log().Info("terminal output channel closed", slog.String("session_id", sessionID), slog.String("reason", "PTY exited"))
						safeConn.WriteJSON(map[string]interface{}{
							"type": "pty_exit",
							"data": map[string]string{
								"session_id": sessionID,
								"message":    "Process exited",
							},
						})
					} else {
						ws.log().Warn("terminal subscriber buffer overflowed; disconnecting for ring-buffer replay", slog.String("session_id", sessionID))
					}
					return
				}

				if err := safeConn.WriteJSON(map[string]interface{}{
					"type": "output",
					"data": map[string]string{
						"session_id": sessionID,
						"output":     string(output),
					},
				}); err != nil {
					ws.log().Error("terminal WebSocket write failed", slog.String("session_id", sessionID), slog.Any("err", err))
					cancel() // Signal other goroutines to stop
					return
				}
			}
		}
	}()

	// Read loop - handles incoming messages from WebSocket
	conn.SetReadLimit(512 * 1024) // 512KB max message size
	// Track last message time for dead-connection detection. The client sends
	// pings every 30s, so any real connection will have activity within this
	// window. If the individual read deadline keeps timing out (half-open
	// TCP connection, Chrome tab freeze with no JS running), we kill the
	// connection after this absolute cap instead of spinning forever.
	// Matches the main /ws handler's 180s threshold.
	lastMessage := time.Now()
	const deadConnectionTimeout = 180 * time.Second
	for {
		select {
		case <-ctx.Done():
			ws.log().Debug("terminal read loop stopped", slog.String("session_id", sessionID), slog.String("reason", "context cancelled"))
			return

		case <-writeDone:
			// Output writer stopped
			ws.log().Debug("terminal read loop stopped", slog.String("session_id", sessionID), slog.String("reason", "writer finished"))
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
					ws.log().Info("terminal WebSocket closed", slog.String("session_id", sessionID), slog.Any("err", err))
				} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// Individual read timed out. Check whether the connection has
					// been dead (no messages at all) for the absolute cap — if
					// so, it's a half-open zombie that will never recover, so
					// close it instead of spinning.
					if time.Since(lastMessage) > deadConnectionTimeout {
						ws.log().Warn("terminal WebSocket inactive; closing dead connection", slog.String("session_id", sessionID), slog.Duration("inactivity", deadConnectionTimeout))
						cancel()
						return
					}
					continue
				} else {
					ws.log().Error("terminal WebSocket read failed", slog.String("session_id", sessionID), slog.Any("err", err))
				}
				// Shell keeps running. Unsubscription is handled by the output
				// writer goroutine's defer when it sees ctx.Done().
				cancel()
				return
			}

			// Update last message time on successful read (includes client
			// pings, which reset the dead-connection timer).
			lastMessage = time.Now()

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
								ws.log().Error("terminal resize failed", slog.String("session_id", sessionID), slog.Any("err", err))
							} else {
								ws.log().Debug("terminal resized", slog.String("session_id", sessionID), slog.Int("rows", int(rows)), slog.Int("cols", int(cols)))
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
				ws.log().Info("terminal close requested", slog.String("session_id", sessionID))
				cancel() // Ensure goroutines stop
				terminalManager.CloseSession(sessionID)
				return
			}
		}
	}
}
