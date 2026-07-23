//go:build !js

package webui

import (
	"encoding/json"
	"log/slog"
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
		ws.log().Warn("invalid security confirmation JSON", slog.Any("err", err))
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if payload.RequestID == "" {
		ws.log().Warn("security confirmation request ID is required")
		http.Error(w, "request_id is required", http.StatusBadRequest)
		return
	}

	// In daemon mode, ws.agent is nil but all agents share the webui-owned
	// securityPromptMgr (injected via InjectWebUIManagers). Use it directly
	// for both tool approval responses and file content prompt responses —
	// the old code checked ws.agent first (nil in daemon mode), causing
	// tool approval responses to be silently dropped.
	if ws.securityPromptMgr != nil {
		decision := resolveApprovalDecision(payload.Action, payload.Response)
		if ws.securityPromptMgr.RespondToApprovalDecision(payload.RequestID, decision) {
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

	// Try security prompt response (legacy boolean path for file content
	// security prompts). Tool approvals use RespondToApprovalDecision above;
	// this path handles the old yes/no prompt responses.
	if ws.securityPromptMgr != nil && ws.securityPromptMgr.RespondToApproval(payload.RequestID, payload.Response) {
		ws.publishClientEvent(defaultWebClientID, events.EventTypeSecurityPromptRequest, map[string]interface{}{
			"status":     "responded",
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

	ws.log().Warn("security confirmation request not found or already handled", slog.String("request_id", payload.RequestID))
	http.Error(w, "Request ID not found", http.StatusNotFound)
}
