package webui

import (
	"encoding/json"
	"net/http"
)

// handleAPIProxyStats handles GET /api/proxy/stats — Foundry proxy stats endpoint.
// It returns the same stats payload used by the workspace stats API but is
// exposed under the proxy path for cloud-mode CloudAdapter consumers.
func (ws *ReactWebServer) handleAPIProxyStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := ws.gatherStats(r)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
