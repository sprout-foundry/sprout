//go:build !js

package webui

import (
	"net/http"
)

// ---------------------------------------------------------------------------
// POST /api/computer-use/test
// ---------------------------------------------------------------------------

// handleAPIComputerUseTest is a lightweight readiness check for the Computer
// Use feature (SP-063 Phase 7). It does NOT take a screenshot — the actual
// screenshot/automation pipeline is wired up in later phases. Instead it
// verifies that the feature flag is enabled so the settings UI can give
// immediate feedback before a user invests time in configuration.
//
// Response shapes:
//   enabled  → { "status": "ok",      "message": "Backend ready" }
//   disabled → { "status": "disabled", "message": "Computer use is not enabled" }
func (ws *ReactWebServer) handleAPIComputerUseTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return // getConfigManager already wrote an error response
	}

	cfg := cm.GetConfig()
	resolved := cfg.ComputerUse.Resolve()

	if !resolved.Enabled {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "disabled",
			"message": "Computer use is not enabled",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "Backend ready",
	})
}
