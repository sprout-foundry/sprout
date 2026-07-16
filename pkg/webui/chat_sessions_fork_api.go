//go:build !js

// Package webui: chat session fork/breakpoints (split from chat_sessions_api.go)
package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// handleAPIChatSessionFork handles POST /api/chat-sessions/fork
// Body: { "chat_id": "string", "breakpoint_index": int }
// Forks the conversation at the given user-message breakpoint (1-based),
// saving the current session and creating a new one from the truncated
// history. Returns the new session ID.
func (ws *ReactWebServer) handleAPIChatSessionFork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		ChatID          string `json:"chat_id"`
		BreakpointIndex int    `json:"breakpoint_index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.BreakpointIndex < 1 {
		writeJSONErr(w, http.StatusBadRequest, "invalid_breakpoint", "breakpoint_index must be >= 1")
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := strings.TrimSpace(req.ChatID)
	if chatID == "" {
		chatID = ws.resolveChatID(r, clientID)
	}

	// Get the chat agent and fork.
	agentInst, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "agent_not_found", fmt.Sprintf("failed to get chat agent: %v", err))
		return
	}
	if agentInst == nil {
		writeJSONErr(w, http.StatusInternalServerError, "agent_not_found", "no agent available for this chat session")
		return
	}

	newSessionID, forkErr := agentInst.ForkAtBreakpoint(req.BreakpointIndex)
	if forkErr != nil {
		log.Printf("handleAPIChatSessionFork: fork failed for chat %s client %s breakpoint %d: %v",
			chatID, clientID, req.BreakpointIndex, forkErr)
		writeJSONErr(w, http.StatusBadRequest, "fork_failed", forkErr.Error())
		return
	}

	// Update the chat session's CurrentSessionID so the WebSocket session-restored
	// event flow and getCurrentSessionIDForRequest publish the new ID.
	// Hold ws.mutex for the full read+write to avoid racing with concurrent
	// switch/clear-history handlers updating the same context.
	ws.mutex.Lock()
	ctx := ws.clientContexts[clientID]
	if ctx != nil {
		cs := ctx.getChatSession(chatID)
		if cs != nil {
			cs.mu.Lock()
			cs.CurrentSessionID = newSessionID
			cs.mu.Unlock()
		}
		ctx.CurrentSessionID = newSessionID
	}
	ws.mutex.Unlock()

	log.Printf("handleAPIChatSessionFork: forked chat %s at breakpoint %d → new session %s for client %s",
		chatID, req.BreakpointIndex, newSessionID, clientID)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"chat_id":    chatID,
		"session_id": newSessionID,
		"message":    fmt.Sprintf("Forked session: %s", newSessionID),
	})
}

// handleAPIChatSessionBreakpoints handles POST /api/chat-sessions/breakpoints
// Body: { "chat_id": "string" }
// Returns the list of forkable breakpoints (user messages) for the chat session.
func (ws *ReactWebServer) handleAPIChatSessionBreakpoints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		ChatID string `json:"chat_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := strings.TrimSpace(req.ChatID)
	if chatID == "" {
		chatID = ws.resolveChatID(r, clientID)
	}

	agentInst, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "agent_not_found", fmt.Sprintf("failed to get chat agent: %v", err))
		return
	}
	if agentInst == nil {
		writeJSONErr(w, http.StatusInternalServerError, "agent_not_found", "no agent available for this chat session")
		return
	}

	bps := agentInst.Breakpoints()
	type bp struct {
		Index   int    `json:"index"`
		Content string `json:"content"`
	}
	result := make([]bp, len(bps))
	for i, b := range bps {
		result[i] = bp{Index: b.Index, Content: b.Content}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"breakpoints": result,
	})
}
