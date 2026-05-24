//go:build !js

package webui

import (
	"context"
	"log"
	"time"
)

const (
	heartbeatMonitorInterval = 15 * time.Second
	heartbeatStaleThreshold  = 60 * time.Second
)

// startHeartbeatMonitor runs a background goroutine that periodically
// checks for stale client connections (no heartbeat for 60s with active
// query) and cancels their queries to prevent orphaned work.
func (ws *ReactWebServer) startHeartbeatMonitor(ctx context.Context) {
	ticker := time.NewTicker(heartbeatMonitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ws.checkStaleConnections()
		}
	}
}

// checkStaleConnections scans all client contexts for ones that haven't
// received a heartbeat within the stale threshold AND have an active query.
// Locks are held only briefly for the scan, then released before doing
// expensive work (agent interrupt, event publish).
func (ws *ReactWebServer) checkStaleConnections() {
	now := time.Now()
	ws.mutex.Lock()
	var staleClients []string
	for clientID, clientCtx := range ws.clientContexts {
		if now.Sub(clientCtx.LastSeenAt) > heartbeatStaleThreshold && clientCtx.ActiveQuery {
			staleClients = append(staleClients, clientID)
		}
	}
	// Release lock before doing expensive work (agent interrupt, event publish)
	ws.mutex.Unlock()

	for _, clientID := range staleClients {
		log.Printf("[heartbeat] Stale connection detected for client %s (no heartbeat for >%s), cancelling active query", clientID, heartbeatStaleThreshold)
		ws.cancelQueryForStaleClient(clientID)
	}
}

// cancelQueryForStaleClient performs the cleanup for a single stale client:
// re-verifies staleness under lock, triggers agent interrupt, decrements the
// active query counter, and publishes a query_cancelled event to notify the frontend.
func (ws *ReactWebServer) cancelQueryForStaleClient(clientID string) {
	ws.mutex.Lock()
	clientCtx := ws.clientContexts[clientID]
	// Re-check staleness under lock — client may have sent a heartbeat between
	// the scan in checkStaleConnections and this call.
	if clientCtx == nil || !clientCtx.ActiveQuery ||
		time.Since(clientCtx.LastSeenAt) <= heartbeatStaleThreshold {
		ws.mutex.Unlock()
		return
	}
	agent := clientCtx.Agent
	ws.mutex.Unlock()

	// Trigger agent interrupt if agent exists
	if agent != nil {
		agent.TriggerInterrupt()
	}

	// Decrement active queries to clean up state
	ws.decrementActiveQueries(clientID)

	// Publish event to notify frontend
	ws.publishClientEvent(clientID, "query_cancelled", map[string]interface{}{
		"reason":  "heartbeat_timeout",
		"message": "Query cancelled: no heartbeat received for 60 seconds",
	})
}
