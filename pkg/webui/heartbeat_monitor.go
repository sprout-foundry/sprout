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
	// maxPausedQueryDuration caps how long a backgrounded (paused) client's
	// in-flight query is kept alive. A tab that's hidden but periodically
	// returns keeps its run going; one that's backgrounded and abandoned for
	// this long is treated like a closed tab so work doesn't run forever.
	maxPausedQueryDuration = 30 * time.Minute
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
		if !clientCtx.ActiveQuery || now.Sub(clientCtx.LastSeenAt) <= heartbeatStaleThreshold {
			continue
		}
		// A paused (backgrounded) client keeps its query running until the
		// max-paused cap — it's expected to return and reattach.
		if clientCtx.Paused && now.Sub(clientCtx.PausedAt) < maxPausedQueryDuration {
			continue
		}
		staleClients = append(staleClients, clientID)
	}
	// Release lock before doing expensive work (agent interrupt, event publish)
	ws.mutex.Unlock()

	for _, clientID := range staleClients {
		log.Printf("[heartbeat] Stale connection detected for client %s (no heartbeat for >%s), cancelling active query", clientID, heartbeatStaleThreshold)
		ws.cancelQueryForClient(clientID, "heartbeat_timeout", "Query cancelled: no heartbeat received for 60 seconds")
	}
}

// cancelQueryForClient cancels a single client's in-flight query: triggers the
// agent interrupt, decrements the active-query counter, and publishes a
// query_cancelled event. Used by the heartbeat monitor (stale connection) and
// the explicit session_close path. The reason/message are surfaced to the
// frontend. Re-verifies under lock that there's still an active query, and (for
// the heartbeat path) that the client hasn't quietly become un-stale or paused.
func (ws *ReactWebServer) cancelQueryForClient(clientID, reason, message string) {
	ws.mutex.Lock()
	clientCtx := ws.clientContexts[clientID]
	if clientCtx == nil || !clientCtx.ActiveQuery {
		ws.mutex.Unlock()
		return
	}
	// For the heartbeat path, re-check staleness and pause under the lock —
	// the client may have sent a heartbeat or paused since the scan.
	if reason == "heartbeat_timeout" {
		if time.Since(clientCtx.LastSeenAt) <= heartbeatStaleThreshold ||
			(clientCtx.Paused && time.Since(clientCtx.PausedAt) < maxPausedQueryDuration) {
			ws.mutex.Unlock()
			return
		}
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
		"reason":  reason,
		"message": message,
	})
}
