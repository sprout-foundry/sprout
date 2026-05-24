//go:build !js

package webui

import (
	"log"
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
		log.Printf("[heartbeat] heartbeat_ack write failed for client %s: %v", clientID, err)
		return
	}
}
