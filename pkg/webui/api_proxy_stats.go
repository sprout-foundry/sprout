//go:build !js

package webui

import (
	"net/http"
)

// handleAPIProxyStats handles GET /api/proxy/stats — Foundry proxy stats endpoint.
// It returns the same stats payload used by the workspace stats API but is
// exposed under the proxy path for cloud-mode CloudAdapter consumers.
func (ws *ReactWebServer) handleAPIProxyStats(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	stats := ws.gatherStats(r)

	writeJSON(w, http.StatusOK, stats)
}
