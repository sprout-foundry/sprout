//go:build !js

package webui

import (
	"log/slog"
	"time"
)

// handleHeartbeatMessage processes a heartbeat message from a connected client.
// It refreshes the client's LastSeenAt timestamp and responds with a heartbeat_ack.
//
// A heartbeat may also carry the window's focus ("foreground"): several
// windows of one client share an event bus, and ask_user requests only go to
// the window on screen (see window_focus.go). A window that comes forward
// reports it here and starts receiving questions on the next fan-out. A
// heartbeat without the field leaves the connection's focus as it was.
func (ws *ReactWebServer) handleHeartbeatMessage(safeConn *SafeConn, clientID string, connInfo *ConnectionInfo, data *HeartbeatData) {
	ws.touchClientLastSeen(clientID)
	if data != nil && data.Foreground != nil {
		ws.setClientForeground(clientID, *data.Foreground)
		if connInfo != nil {
			connInfo.setForeground(*data.Foreground)
		}
	}
	if err := safeConn.WriteJSON(map[string]interface{}{
		"type": "heartbeat_ack",
		"data": map[string]interface{}{"timestamp": time.Now().Unix()},
	}); err != nil {
		ws.log().Error("heartbeat acknowledgement write failed", slog.String("client_id", clientID), slog.Any("err", err))
		return
	}
}
