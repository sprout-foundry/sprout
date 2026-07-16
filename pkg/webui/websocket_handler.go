//go:build !js

package webui

// Package webui: WebSocket entry dispatcher and active-connection tracking (split from websocket_handler.go)

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// activeWSConn tracks a single active WebSocket connection for a user
// to enforce single-active-session policy (SP-118 Phase 1, Mode 1).
// In SP-118 Phase 2 (Mode 2), this type is not used; the daemon uses
// UserConnections (pkg/webui/multi_connection_registry.go) instead.
type activeWSConn struct {
	safeConn    *SafeConn
	conn        *websocket.Conn
	sessionID   string
	connectedAt time.Time
	closed      chan struct{} // closed when the connection is closed
}

// handleWebSocket is the internal entry point that pkg/webui/routes.go
// wires to /ws. It dispatches to the mode-appropriate handler based on
// (agentEnforceSingleSession, DaemonMultiSession config):
//
//	Mode 1 (handleWebSocket_Agent) when EITHER:
//	  - agentEnforceSingleSession is true (sprout agent / interactive
//	    CLI explicitly opts in to single-session), OR
//	  - agentEnforceSingleSession is false but the daemon_multi_session
//	    config setting is false (operator opted the daemon back into
//	    Mode 1 for a window; SP-118 Phase 4 rollout gate).
//
//	Mode 2 (handleWebSocket_Daemon) when:
//	  - agentEnforceSingleSession is false AND
//	  - daemon_multi_session is true (default)
//
// Effective value formula (per spec): `(!agentEnforceSingleSession) && daemon_multi_session`.
// The agent path always uses Mode 1 regardless of daemon_multi_session;
// the daemon path uses Mode 2 only when both conditions hold.
//
// Do NOT dispatch on serviceMode here. Tests like
// TestSessionConflict_Takeover_UserMode set serviceMode=true and exercise
// the takeover flow as Mode 1 behavior; re-using serviceMode as the
// dispatch key would break those tests. See SP-118 §Design "Dispatch
// signal".
func (ws *ReactWebServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if ws.shouldUseMode1() {
		ws.handleWebSocket_Agent(w, r)
		return
	}
	ws.handleWebSocket_Daemon(w, r)
}

// shouldUseMode1 returns true when the dispatcher should route this
// connection through the single-active-session (Mode 1) path. Mode 1
// is forced when agentEnforceSingleSession is set (sprout agent
// always uses Mode 1, regardless of config) OR when the operator
// has disabled daemon_multi_session in config (rollout escape hatch).
//
// Reads the DaemonMultiSession config lazily on each call so a
// config change is picked up without restart.
//
// When ws.agent is nil (pre-SP-118 test scaffolding with no real
// config manager), we default to Mode 2: this matches the production
// daemon path (which has no agent) and lets the new daemon
// integration tests exercise Mode 2 without setting up a full
// agent+config manager. The agent path (sprout interactive) always
// has a non-nil agent, so this fallback never fires in production.
func (ws *ReactWebServer) shouldUseMode1() bool {
	if ws.agentEnforceSingleSession {
		return true
	}
	if ws.agent == nil {
		// No agent → no config manager → Mode 2 default.
		return false
	}
	cm := ws.agent.GetConfigManager()
	if cm == nil {
		return false
	}
	cfg := cm.GetConfig()
	if cfg == nil {
		return false
	}
	// cfg.DaemonMultiSession defaults to true via config_migration.go's
	// applyDaemonMultiSessionDefault. Setting false in config opts the
	// daemon back into Mode 1 (rollout escape hatch).
	return !cfg.DaemonMultiSession
}
