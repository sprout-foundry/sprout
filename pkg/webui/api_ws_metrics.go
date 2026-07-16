//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"sort"
)

// wsMode is the typed enumeration of WebSocket dispatch modes
// surfaced in /api/ws-metrics. The string values are part of the
// public HTTP API contract — operators grep logs and dashboards for
// them — so the JSON encoding is `WSMode` and the wire values are
// the untyped string constants. Using a named type means a typo in
// a future caller fails at compile time rather than silently
// reporting the wrong mode.
type wsMode string

const (
	wsModeAgent  wsMode = "agent"
	wsModeDaemon wsMode = "daemon"
)

// wsMetricsResponse is the JSON shape returned by GET /api/ws-metrics.
//
// Schema:
//   - mode: "agent" (single-active-session, Mode 1) or "daemon" (multi-session,
//     Mode 2). Lets an operator confirm the rollout is active on the
//     deployed instance without inspecting logs.
//   - total_connections: total live WS connections across all users.
//   - users_with_connections: count of distinct users with ≥1 open
//     connection. Useful for "how many of our customers are multi-tabbing"
//     reporting.
//   - max_connections_per_user: highest per-user connection count right
//     now. In Mode 1 this is always ≤1; in Mode 2 it can be N.
//   - per_user: top-K user-id → connection-count, sorted descending by
//     count. Capped at top 20 to keep the payload bounded.
//
// The endpoint never reveals session IDs, client IDs, or chat IDs — those
// are debugging breadcrumbs we don't expose via HTTP.
type wsMetricsResponse struct {
	Mode                  wsMode         `json:"mode"`
	TotalConnections      int            `json:"total_connections"`
	UsersWithConnections  int            `json:"users_with_connections"`
	MaxConnectionsPerUser int            `json:"max_connections_per_user"`
	PerUser               []userConnInfo `json:"per_user,omitempty"`
}

type userConnInfo struct {
	UserID       string `json:"user_id"`
	SessionCount int    `json:"session_count"`
}

// maxPerUserInMetrics caps the per-user breakdown at 20 entries so
// the response stays small even on a noisy multi-tenant daemon. The
// total counts above are always exact.
const maxPerUserInMetrics = 20

// handleAPIWSMetrics handles GET /api/ws-metrics. Returns a JSON
// snapshot of current WebSocket session state. SP-118 Phase 5.
func (ws *ReactWebServer) handleAPIWSMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := ws.computeWSMetrics()

	w.Header().Set("Content-Type", "application/json")
	// Cache-Control: never cache. Counts can shift between requests;
	// any intermediary that holds a stale snapshot defeats the
	// purpose of the endpoint (debugging, capacity planning).
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

// computeWSMetrics builds the wsMetricsResponse by walking
// ws.userConnections. In Mode 1 the registry is unused, so
// per-user counts will be 0 — the response still includes the mode
// flag so an operator can tell at a glance.
//
// Pulls a snapshot of every user's session count, sorts descending,
// and trims to maxPerUserInMetrics entries.
//
// Note: AllUserIDs and Count are separate operations. A connection
// added or removed between them may not be reflected in the snapshot.
// This is acceptable for a debug endpoint — a few dropped entries on a
// busy daemon are better than spending time on a fully-consistent
// snapshot that operators only consult by hand.
func (ws *ReactWebServer) computeWSMetrics() wsMetricsResponse {
	mode := wsModeDaemon
	if ws.shouldUseMode1() {
		mode = wsModeAgent
	}

	resp := wsMetricsResponse{Mode: mode}

	if ws.userConnections == nil {
		// Defensive: nil registry (shouldn't happen post-Phase-1)
		// returns a zeroed but well-formed payload.
		return resp
	}

	users := ws.userConnections.AllUserIDs()
	total := 0
	maxPer := 0
	usersWithConn := 0
	perUser := make([]userConnInfo, 0, len(users))
	for _, uid := range users {
		c := ws.userConnections.Count(uid)
		if c == 0 {
			continue
		}
		total += c
		if c > maxPer {
			maxPer = c
		}
		usersWithConn++
		perUser = append(perUser, userConnInfo{UserID: uid, SessionCount: c})
	}

	// Sort descending by SessionCount, then by UserID for stable
	// output when counts tie.
	sort.Slice(perUser, func(i, j int) bool {
		if perUser[i].SessionCount != perUser[j].SessionCount {
			return perUser[i].SessionCount > perUser[j].SessionCount
		}
		return perUser[i].UserID < perUser[j].UserID
	})
	if len(perUser) > maxPerUserInMetrics {
		perUser = perUser[:maxPerUserInMetrics]
	}

	resp.TotalConnections = total
	resp.UsersWithConnections = usersWithConn
	resp.MaxConnectionsPerUser = maxPer
	resp.PerUser = perUser
	return resp
}
