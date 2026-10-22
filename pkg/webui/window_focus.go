//go:build !js

package webui

import (
	"strings"
	"time"
)

// window_focus.go — which window of a client is on screen.
//
// A client (one browser profile) can have several windows open, all sharing
// one event bus. Security-scoped events already stay on the window that owns
// them, but ask_user is a question to *the user* and fans out to every window
// of the session — otherwise the owning window, often one the user is not
// looking at, would swallow the question and the agent would block until the
// ask_user timeout. The fan-out has a cost: every window behind the active one
// pops a modal dialog, which is easy to miss and easy to answer by accident.
//
// So the fan-out is narrowed to the window the user is actually looking at.
// Each window reports its focus on every heartbeat ("foreground" field). The
// client context keeps the newest report and stamps it with the reporting
// window's client ID; a connection is foreground when the newest report is its
// own. A window that comes forward reports on its next heartbeat and is served
// from then on.
//
// Defaults are deliberately permissive: a client that has never reported focus
// (older client, single window, tests) is treated as foreground everywhere, so
// the narrowing only kicks in once a window actually says it is in front.

// foregroundReportTTL bounds how long a focus report is trusted. Heartbeats
// are frequent (seconds), so this only matters for a window that goes away
// without a final heartbeat: its "I am in front" report would otherwise keep
// every other window of the client starved of questions. After this long the
// next heartbeat to arrive takes over.
const foregroundReportTTL = 30 * time.Second

// setClientForeground records that the window identified by clientID is (or is
// not) the one on screen.
func (ws *ReactWebServer) setClientForeground(clientID string, foreground bool) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = defaultWebClientID
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		return
	}
	ctx.ForegroundWindow = clientID
	ctx.Foreground = foreground
	ctx.ForegroundAt = time.Now()
}

// clientForegroundWindow returns the client ID of the window last reported as
// foreground, and whether that report is still fresh. ok is false when no
// window has reported focus (or the report went stale), in which case callers
// must treat every window as foreground.
func (ws *ReactWebServer) clientForegroundWindow(clientID string) (window string, ok bool) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = defaultWebClientID
	}

	ws.mutex.RLock()
	defer ws.mutex.RUnlock()

	ctx := ws.clientContexts[clientID]
	if ctx == nil || ctx.ForegroundWindow == "" || !ctx.Foreground {
		return "", false
	}
	if time.Since(ctx.ForegroundAt) > foregroundReportTTL {
		return "", false
	}
	return ctx.ForegroundWindow, true
}

// isWebUIConnectionForeground decides whether a newly created connection
// belongs to the window on screen. Used at connection setup, where there is no
// ConnectionInfo yet: the client context still holds the previous connection's
// report, so a connection whose client is not the reported foreground window
// is assumed to be behind it.
func isWebUIConnectionForeground(ws *ReactWebServer, clientID string) bool {
	window, ok := ws.clientForegroundWindow(clientID)
	if !ok {
		return true
	}
	return strings.TrimSpace(window) == strings.TrimSpace(clientID)
}
