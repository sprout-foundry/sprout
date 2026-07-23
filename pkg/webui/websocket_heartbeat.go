//go:build !js

package webui

import (
	"log/slog"
	"time"
)

// handleHeartbeatMessage processes a heartbeat message from a connected client.
// It refreshes the client's LastSeenAt timestamp and responds with a heartbeat_ack.
func (ws *ReactWebServer) handleHeartbeatMessage(safeConn *SafeConn, clientID string) {
	ws.touchClientLastSeen(clientID)
	if err := safeConn.WriteJSON(map[string]interface{}{
		"type": "heartbeat_ack",
		"data": map[string]interface{}{"timestamp": time.Now().Unix()},
	}); err != nil {
		ws.log().Error("heartbeat acknowledgement write failed", slog.String("client_id", clientID), slog.Any("err", err))
		return
	}
}
