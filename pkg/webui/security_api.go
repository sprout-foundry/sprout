//go:build !js

package webui

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/sprout-foundry/sprout/pkg/events"
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
		// Action enables the 4-option dialog (SP-058 follow-up). Optional —
		// empty falls back to Response. See SecurityApprovalResponseData for
		// the legal value set.
		Action string `json:"action,omitempty"`
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
		decision := resolveApprovalDecision(payload.Action, payload.Response)
		if agent.GetSecurityApprovalMgr().RespondToApprovalDecision(payload.RequestID, decision) {
			ws.publishClientEvent(defaultWebClientID, events.EventTypeSecurityApprovalRequest, map[string]interface{}{
				"status":     "responded",
				"request_id": payload.RequestID,
				"response":   decision.Approved(),
				"action":     decision.String(),
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
	if ws.securityPromptMgr != nil && ws.securityPromptMgr.RespondToApproval(payload.RequestID, payload.Response) {
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