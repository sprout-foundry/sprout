//go:build !js

// Preview API — SP-155 §155a (TODO 155.4).
//
// Four endpoints drive the preview pane (155.3's presentational component,
// which renders state entirely through its props) from the project's dev
// server, owned by a per-root pkg/preview.Manager:
//
//   - GET  /api/preview/status  — the lifecycle state {status, url, error,
//     detected}: starting | running | stopped | failed.
//   - POST /api/preview/start   — detect an already-running dev server on
//     the manifest's port, or start the manifest's dev command (readiness
//     settles asynchronously in the background; the pane polls status).
//   - POST /api/preview/restart — stop the manager's dev server and start
//     it again (a detected server is re-detected, not restarted).
//   - POST /api/preview/stop    — stop the dev server the manager started.
//
// The handlers mirror api_starters.go's structure and conventions (plain
// handlers, writeJSON / writeJSONErr, this file behind //go:build !js):
// the only state is the per-root manager cache on the server, drained on
// Shutdown (and in test cleanup) so a daemon restart never leaks a
// preview process.
package webui

import (
	"context"
	"errors"
	"net/http"

	"github.com/sprout-foundry/sprout/pkg/preview"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// registerPreviewRoutes mounts the SP-155 §155a preview surface (TODO
// 155.4): the dev-server state the preview pane renders and the actions
// that drive it (start, restart, stop).
func (ws *ReactWebServer) registerPreviewRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/preview/status", ws.handleAPIPreviewStatus)
	mux.HandleFunc("/api/preview/start", ws.handleAPIPreviewStart)
	mux.HandleFunc("/api/preview/restart", ws.handleAPIPreviewRestart)
	mux.HandleFunc("/api/preview/stop", ws.handleAPIPreviewStop)
}

// previewManager returns the preview manager for the project root,
// creating it on first use. One manager (and at most one dev server) per
// root: a worktree switch to a different root gets its own manager.
func (ws *ReactWebServer) previewManager(root string) *preview.Manager {
	ws.previewManagersMu.Lock()
	defer ws.previewManagersMu.Unlock()
	if m, ok := ws.previewManagers[root]; ok {
		return m
	}
	m := preview.New(root)
	ws.previewManagers[root] = m
	return m
}

// stopPreviewManagers stops every preview dev server and drains the cache
// (server Shutdown; the tests' cleanup). Managers are per-root, so a
// later start for the same root gets a clean manager.
func (ws *ReactWebServer) stopPreviewManagers() {
	ws.previewManagersMu.Lock()
	managers := ws.previewManagers
	ws.previewManagers = make(map[string]*preview.Manager)
	ws.previewManagersMu.Unlock()
	for _, m := range managers {
		m.Stop()
	}
}

// previewDevDeclaration resolves the project's dev declaration for the
// start/restart actions: the manifest's dev port with (0, "") when it
// carries a usable dev command and port, or the HTTP code, error code and
// message to reply with — a 404 (no_dev_command) for a structural no-dev
// project, a 400 (invalid_manifest) for an invalid one. The manager never
// guesses a command, so the actions refuse a missing declaration up front
// (the status endpoint reports it instead: stopped, with the reason).
func previewDevDeclaration(root string) (port, code int, errCode, msg string) {
	manifest, err := starterstore.LoadStarterManifest(root)
	switch {
	case errors.Is(err, starterstore.ErrNoManifest):
		return 0, http.StatusNotFound, "no_dev_command", "no starter manifest: the project declares no dev server"
	case err != nil:
		return 0, http.StatusBadRequest, "invalid_manifest", "invalid starter manifest: " + err.Error()
	case manifest.Dev == "":
		return 0, http.StatusNotFound, "no_dev_command", "the starter manifest declares no dev command (dev)"
	case manifest.DevPort == 0:
		return 0, http.StatusNotFound, "no_dev_command", "the starter manifest declares no dev port (dev_port)"
	}
	return manifest.DevPort, 0, "", ""
}

// handleAPIPreviewStatus handles GET /api/preview/status: the dev
// server's lifecycle state for the current workspace root. The response
// is always 200 — a state observation, never a caller fault: a project
// without a dev server reports stopped, with the reason.
func (ws *ReactWebServer) handleAPIPreviewStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	m := ws.previewManager(ws.GetWorkspaceRoot())
	writeJSON(w, http.StatusOK, m.State())
}

// handleAPIPreviewStart handles POST /api/preview/start: detect an
// already-running dev server on the manifest's port, or start the
// manifest's dev command. The response is the state the start settles
// into: running (detected) synchronously, or starting — readiness settles
// in the background and the pane polls /api/preview/status for the
// outcome. A project with no usable dev declaration is refused up front
// (the manager never guesses a command).
func (ws *ReactWebServer) handleAPIPreviewStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	root := ws.GetWorkspaceRoot()
	port, code, errCode, msg := previewDevDeclaration(root)
	if code != 0 {
		writeJSONErr(w, code, errCode, msg)
		return
	}

	m := ws.previewManager(root)
	st := m.State()
	switch st.Status {
	case preview.StatusRunning, preview.StatusStarting:
		// Already up (detected or owned), or a start in flight: report
		// the state as-is, never spawn a second server.
		writeJSON(w, http.StatusOK, st)
		return
	}

	// Detection fast path: a dev server already answering on the
	// manifest's port settles synchronously, so the pane reports it
	// immediately instead of on the next poll.
	if preview.PortServing(port) {
		writeJSON(w, http.StatusOK, m.Start(r.Context()))
		return
	}

	// Settle readiness in the background: a cold dev server can take up
	// to the manager's ready timeout, and the pane polls for the
	// outcome. context.Background on purpose (nolint:gosec G118): the
	// start must outlive this request — the pane tracks it through
	// /api/preview/status, so cancelling it with r.Context() the moment
	// this handler returns would kill the dev server mid-start.
	go m.Start(context.Background()) //nolint:gosec // G118: fire-and-forget start (see above)
	writeJSON(w, http.StatusOK, preview.State{Status: preview.StatusStarting})
}

// handleAPIPreviewRestart handles POST /api/preview/restart: stop the
// manager's dev server and start it again. A detected (external) server
// is not owned — a restart re-detects it, so the response is its running
// state, not a start.
func (ws *ReactWebServer) handleAPIPreviewRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	root := ws.GetWorkspaceRoot()
	_, code, errCode, msg := previewDevDeclaration(root)
	if code != 0 {
		writeJSONErr(w, code, errCode, msg)
		return
	}

	m := ws.previewManager(root)
	if st := m.State(); st.Status == preview.StatusRunning && st.Detected {
		// An external server: nothing of ours to restart.
		writeJSON(w, http.StatusOK, st)
		return
	}
	go m.Restart(context.Background())
	writeJSON(w, http.StatusOK, preview.State{Status: preview.StatusStarting})
}

// handleAPIPreviewStop handles POST /api/preview/stop: stop the dev
// server the manager started. A detected (external) server is left
// alone: the response reports its state, still running/detected.
func (ws *ReactWebServer) handleAPIPreviewStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	m := ws.previewManager(ws.GetWorkspaceRoot())
	writeJSON(w, http.StatusOK, m.Stop())
}
