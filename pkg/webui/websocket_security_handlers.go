package webui

import (
	"fmt"
	"log"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/security"
	agenttools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// handleSecurityApprovalResponse processes security approval responses from the webui.
// The webui sends a { "type": "security_approval_response", "data": { "request_id": "...", "approved": true/false } }
// message when the user approves or rejects a security warning.
func (ws *ReactWebServer) handleSecurityApprovalResponse(safeConn *SafeConn, msg map[string]interface{}, clientID string) {
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

	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Invalid security approval response payload"},
		})
		return
	}

	requestID, _ := data["request_id"].(string)
	if requestID == "" {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "request_id is required"},
		})
		return
	}

	approved, _ := data["approved"].(bool)

	mgr := clientAgent.GetSecurityApprovalMgr()
	if mgr == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Security approval manager is not available"},
		})
		return
	}

	if !mgr.RespondToApproval(requestID, approved) {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": fmt.Sprintf("No pending security request with id: %s", requestID)},
		})
		return
	}

	log.Printf("Security approval response received: request_id=%s approved=%v", requestID, approved)
}

// handleSecurityPromptResponse processes security prompt responses from the webui.
// The webui sends a { "type": "security_prompt_response", "data": { "request_id": "...", "response": true/false } }
// message when the user responds to a file security concern prompt.
func (ws *ReactWebServer) handleSecurityPromptResponse(safeConn *SafeConn, msg map[string]interface{}, clientID string) {
	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Invalid security prompt response payload"},
		})
		return
	}

	requestID, _ := data["request_id"].(string)
	if requestID == "" {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "request_id is required"},
		})
		return
	}

	response, _ := data["response"].(bool)

	mgr := security.GetGlobalApprovalManager()
	if mgr == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Security prompt manager is not available"},
		})
		return
	}

	if mgr.RespondToApproval(requestID, response) {
		ws.publishClientEvent(clientID, events.EventTypeSecurityPromptRequest, map[string]interface{}{
			"status":     "responded",
			"request_id": requestID,
			"response":   response,
		})
		log.Printf("Security prompt response received: request_id=%s response=%v", requestID, response)
	} else {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": fmt.Sprintf("No pending security prompt with id: %s", requestID)},
		})
	}
}

// handleAskUserResponse processes ask_user responses from the webui.
// The webui sends a { "type": "ask_user_response", "data": { "request_id": "...", "response": "..." } }
// message when the user responds to a question prompt.
func (ws *ReactWebServer) handleAskUserResponse(safeConn *SafeConn, msg map[string]interface{}, clientID string) {
	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Invalid ask_user response payload"},
		})
		return
	}

	requestID, _ := data["request_id"].(string)
	if requestID == "" {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "request_id is required"},
		})
		return
	}

	response, _ := data["response"].(string)
	response = strings.TrimSpace(response)
	if response == "" {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "response cannot be empty"},
		})
		return
	}

	mgr := agenttools.GetGlobalAskUserManager()
	if mgr == nil {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": "Ask user manager is not available"},
		})
		return
	}

	if mgr.RespondToAskUser(requestID, response) {
		log.Printf("Ask user response received: request_id=%s response_length=%d", requestID, len(response))
	} else {
		_ = safeConn.WriteJSON(map[string]interface{}{
			"type": "error",
			"data": map[string]string{"message": fmt.Sprintf("No pending ask_user request with id: %s", requestID)},
		})
	}
}
