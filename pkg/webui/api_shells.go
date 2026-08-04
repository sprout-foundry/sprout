//go:build !js

package webui

import (
	"net/http"
)

// handleAPITerminalShells returns the list of available shells on the system.
func (ws *ReactWebServer) handleAPITerminalShells(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	terminalManager := ws.getTerminalManagerForRequest(r)
	shells := terminalManager.AvailableShells()

	writeJSON(w, http.StatusOK, map[string]interface{}{"shells": shells})
}
