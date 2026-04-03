package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// handleAPITerminalShells returns the list of available shells on the system.
func (ws *ReactWebServer) handleAPITerminalShells(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	terminalManager := ws.getTerminalManagerForRequest(r)
	shells := terminalManager.AvailableShells()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"shells": shells}); err != nil {
		fmt.Printf("handleAPITerminalShells: encode error: %v\n", err)
	}
}
