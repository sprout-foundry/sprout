package webui

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

// driftResponseRequest is the JSON payload for a drift user response.
type driftResponseRequest struct {
	StartedNewChat bool `json:"startedNewChat"`
}

// handleAPIDriftResponse records the user's response to a drift notification.
func (ws *ReactWebServer) handleAPIDriftResponse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req driftResponseRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		log.Printf("handleAPIDriftResponse: invalid JSON: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := ws.resolveChatID(r, clientID)

	clientAgent, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		log.Printf("handleAPIDriftResponse: failed to get agent: %v", err)
		http.Error(w, "Failed to get agent", http.StatusNotFound)
		return
	}

	clientAgent.RecordDriftUserResponse(req.StartedNewChat)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}