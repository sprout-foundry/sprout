//go:build !js

package webui

// Package webui: WebSocket session takeover, conflict resolution, and sync recovery (split from websocket_handler.go)

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

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
		ws.log().Warn("invalid message during takeover wait", slog.String("session_id", sessionID), slog.Any("err", err))
		return false
	}

	if msg.Type != AllowedMessageTypeSessionTakeover {
		ws.log().Warn("unexpected message type during takeover wait", slog.String("session_id", sessionID), slog.String("message_type", msg.Type), slog.String("expected_type", AllowedMessageTypeSessionTakeover))
		return false
	}

	ws.log().Info("WebSocket session confirmed takeover", slog.String("session_id", sessionID))
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
		ws.log().Error("unexpected active WebSocket entry type", slog.String("tracking_key", trackingKey))
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

	ws.log().Info("WebSocket session evicted", slog.String("session_id", active.sessionID), slog.String("tracking_key", trackingKey))
	return true
}

// handleSyncRecoverMessage processes a sync_recover message from the browser
// after container death or browser crash recovery.
func (ws *ReactWebServer) handleSyncRecoverMessage(safeConn *SafeConn, sessionID string, msg *WebSocketMessage, clientID string) {
	// Unmarshal the data payload
	var data map[string]interface{}
	if len(msg.Data) == 0 {
		ws.log().Warn("sync recovery received empty data", slog.String("session_id", sessionID))
		safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "invalid sync_recover data"},
		})
		return
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		ws.log().Warn("sync recovery received invalid JSON", slog.String("session_id", sessionID), slog.Any("err", err))
		safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "invalid sync_recover data"},
		})
		return
	}

	// Extract browser seq map from data
	seqsRaw, ok := data["seqs"]
	if !ok {
		ws.log().Warn("sync recovery data missing sequences", slog.String("session_id", sessionID))
		safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "missing seqs in sync_recover"},
		})
		return
	}

	seqsMap, ok := seqsRaw.(map[string]interface{})
	if !ok {
		ws.log().Warn("sync recovery sequences have invalid type", slog.String("session_id", sessionID))
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
			ws.log().Warn("sync recovery sequence has unexpected type", slog.String("path", path), slog.String("value_type", fmt.Sprintf("%T", seqVal)))
		}
	}

	ws.log().Info("sync recovery started", slog.String("client_id", clientID), slog.Int("file_count", len(browserSeqs)))

	// Run container death recovery with per-file seqs
	result, err := ws.HandleContainerRecoveryWithSeqs(context.Background(), clientID, browserSeqs)
	if err != nil {
		ws.log().Error("sync recovery reconciliation failed", slog.Any("err", err))
		safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": fmt.Sprintf("reconciliation failed: %v", err)},
		})
		return
	}

	// Send reconciliation plan back to browser
	if err := ws.SendSyncReconcile(safeConn, result); err != nil {
		ws.log().Error("sync recovery reconcile plan send failed", slog.Any("err", err))
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
			ws.log().Error("sync recovery agent lookup failed", slog.String("client_id", clientID), slog.Any("err", err))
			return
		}
		if err := ws.SendSyncReplayStart(safeConn, clientID, filesToReplay); err != nil {
			ws.log().Error("sync recovery replay start send failed", slog.Any("err", err))
			return
		}

		for _, action := range result.Plan {
			if action.Action != "container_ahead" {
				continue
			}
			// Validate path to prevent traversal attacks
			if filepath.IsAbs(action.FilePath) || strings.Contains(action.FilePath, "..") {
				ws.log().Warn("sync recovery skipping invalid path", slog.String("path", action.FilePath))
				continue
			}
			// Read the file content from container
			content, err := ag.ReadFileContent(action.FilePath)
			if err != nil {
				ws.log().Error("sync recovery file read failed", slog.String("path", action.FilePath), slog.Any("err", err))
				continue
			}
			if err := ws.SendSyncReplayFile(safeConn, clientID, action.FilePath, content, action.ContainerSeq); err != nil {
				ws.log().Error("sync recovery file replay failed", slog.String("path", action.FilePath), slog.Any("err", err))
				return
			}
		}

		if err := ws.SendSyncReplayComplete(safeConn, clientID); err != nil {
			ws.log().Error("sync recovery replay completion send failed", slog.Any("err", err))
		}
	}

	ws.log().Info("sync recovery completed", slog.String("client_id", clientID), slog.Int("files_reconciled", len(result.Plan)))
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
