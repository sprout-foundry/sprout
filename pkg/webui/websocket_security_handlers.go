//go:build !js

package webui

import (
	"fmt"
	"log"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// handleSecurityApprovalResponse processes security approval responses from the webui.
// The webui sends a { "type": "security_approval_response", "data": { "request_id": "...", "approved": true/false } }
// message when the user approves or rejects a security warning.
func (ws *ReactWebServer) handleSecurityApprovalResponse(safeConn *SafeConn, data *SecurityApprovalResponseData, clientID string) {
	// Route to the currently active chat's agent, since the security dialog
	// is always shown in the context of the active chat view.
	_, activeChatID := ws.getActiveChatContext(clientID)

	clientAgent, err := ws.getChatAgent(clientID, activeChatID)
	if err != nil || clientAgent == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Agent is not available"},
		})
		return
	}

	mgr := clientAgent.GetSecurityApprovalMgr()
	if mgr == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Security approval manager is not available"},
		})
		return
	}

	decision := resolveApprovalDecision(data.Action, data.Approved)
	if !mgr.RespondToApprovalDecision(data.RequestID, decision) {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": fmt.Sprintf("No pending security request with id: %s", data.RequestID)},
		})
		return
	}

	log.Printf("Security approval response received: request_id=%s decision=%s", data.RequestID, decision.String())
}

// resolveApprovalDecision picks the typed ApprovalDecision based on the
// new Action field, falling back to the legacy Approved bool when Action
// is empty. Old WebUI clients keep working unchanged.
func resolveApprovalDecision(action string, approved bool) security.ApprovalDecision {
	if action != "" {
		return security.ApprovalDecisionFromString(action)
	}
	if approved {
		return security.ApprovalApproveOnce
	}
	return security.ApprovalDeny
}

// handleSecurityPromptResponse processes security prompt responses from the webui.
// The webui sends a { "type": "security_prompt_response", "data": { "request_id": "...", "response": true/false } }
// message when the user responds to a file security concern prompt.
func (ws *ReactWebServer) handleSecurityPromptResponse(safeConn *SafeConn, data *SecurityPromptResponseData, clientID string) {
	if ws.securityPromptMgr == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Security prompt manager is not available"},
		})
		return
	}

	if ws.securityPromptMgr.RespondToApproval(data.RequestID, data.Response) {
		ws.publishClientEvent(clientID, events.EventTypeSecurityPromptRequest, map[string]interface{}{
			"status":     "responded",
			"request_id": data.RequestID,
			"response":   data.Response,
		})
		log.Printf("Security prompt response received: request_id=%s response=%v", data.RequestID, data.Response)
	} else {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": fmt.Sprintf("No pending security prompt with id: %s", data.RequestID)},
		})
	}
}

// handleAskUserResponse processes ask_user responses from the webui.
// The webui sends a { "type": "ask_user_response", "data": { "request_id": "...", "response": "..." } }
// message when the user responds to a question prompt.
func (ws *ReactWebServer) handleAskUserResponse(safeConn *SafeConn, data *AskUserResponseData, clientID string) {
	if ws.askUserMgr == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Ask user manager is not available"},
		})
		return
	}

	if ws.askUserMgr.RespondToAskUser(data.RequestID, data.Response) {
		log.Printf("Ask user response received: request_id=%s response_length=%d", data.RequestID, len(data.Response))
	} else {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": fmt.Sprintf("No pending ask_user request with id: %s", data.RequestID)},
		})
	}
}
