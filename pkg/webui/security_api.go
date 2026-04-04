package webui

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/security"
)

// handleAPIConfirm handles user responses to security prompts (both approval requests and file security prompts)
// Expected JSON body: {"request_id": "string", "response": boolean}
func (ws *ReactWebServer) handleAPIConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		RequestID string `json:"request_id"`
		Response  bool   `json:"response"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("handleAPIConfirm: invalid JSON: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if payload.RequestID == "" {
		log.Printf("handleAPIConfirm: request_id is required")
		http.Error(w, "request_id is required", http.StatusBadRequest)
		return
	}

	// Try security approval response first
	ws.mutex.RLock()
	agent := ws.agent
	ws.mutex.RUnlock()

	if agent != nil && agent.GetSecurityApprovalMgr() != nil {
		if agent.GetSecurityApprovalMgr().RespondToApproval(payload.RequestID, payload.Response) {
			ws.publishClientEvent(defaultWebClientID, events.EventTypeSecurityApprovalRequest, map[string]interface{}{
				"status":   "responded",
				"request_id": payload.RequestID,
				"response": payload.Response,
			})
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "Security approval response recorded",
			})
			return
		}
	}

	// Try security prompt response
	if mgr := security.GetGlobalPromptManager(); mgr != nil && mgr.RespondToPrompt(payload.RequestID, payload.Response) {
		ws.publishClientEvent(defaultWebClientID, events.EventTypeSecurityPromptRequest, map[string]interface{}{
			"status":    "responded",
			"request_id": payload.RequestID,
			"response":   payload.Response,
		})
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Security prompt response recorded",
		})
		return
	}

	log.Printf("handleAPIConfirm: unknown or already handled request_id: %s", payload.RequestID)
	http.Error(w, "Request ID not found", http.StatusNotFound)
}