//go:build !js

// Package webui: chat session switch/compact/clear-history (split from chat_sessions_api.go)
package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// handleAPIChatSessionsSwitch handles POST /api/chat-sessions/switch
// Body: { "id": "chat-id" }
// Switches the active chat for this client. Returns the switched-to session
// state (agent_state snapshot, messages, etc.) so the frontend can populate the chat view.
func (ws *ReactWebServer) handleAPIChatSessionsSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ws.rejectIfSharedMode(w) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	chatID := strings.TrimSpace(req.ID)
	if chatID == "" {
		http.Error(w, "Chat session ID is required", http.StatusBadRequest)
		return
	}

	clientID := ws.resolveClientID(r)

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ensureDefaultChatSession()

	// Verify the target chat session exists
	cs := ctx.getChatSession(chatID)
	if cs == nil {
		ws.mutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Chat session not found",
			"code":  "chat_session_not_found",
			"id":    chatID,
		})
		return
	}

	// Update the active chat ID.
	// No need to block on active queries — each chat has its own agent, so
	// switching is safe at any time.
	ctx.DefaultChatID = chatID

	// Point the client-level Agent reference at the active chat's agent for
	// backward compatibility with code paths that use getClientAgent.
	cs.mu.Lock()
	if cs.Agent != nil {
		ctx.Agent = cs.Agent
	} else {
		ctx.Agent = nil
	}
	// Sync the top-level agent state to match the active chat session for
	// backward compatibility with code that reads ctx.AgentState.
	snapshot := cs.AgentState
	if len(snapshot) == 0 {
		snapshot = emptyAgentStateSnapshot()
	}
	currentSessionID := cs.CurrentSessionID
	wtPath := cs.WorktreePath
	cs.mu.Unlock()

	// Switch workspace root to the target chat's worktree if it has one
	if wtPath != "" {
		ctx.WorkspaceRoot = wtPath
		if clientID == defaultWebClientID {
			ws.workspaceRoot = wtPath
		}
	}

	ctx.AgentState = append([]byte(nil), snapshot...)
	ctx.CurrentSessionID = currentSessionID
	ws.mutex.Unlock()

	log.Printf("handleAPIChatSessionsSwitch: switched to chat session %s for client %s", chatID, clientID)

	ws.publishSessionChanged(clientID, chatID, "switch", cs.chatSessionSummary(false))

	// Re-publish provider state so the WebUI status bar (model/cost/ctx)
	// reflects the newly active chat's agent rather than stale data from
	// the previous chat. Each chat owns its own agent, so the provider
	// and model may differ across chats — without this republish the bar
	// only updates on the next metrics event (e.g. tool_end), which can
	// leave the user looking at the wrong model name for several seconds.
	ws.publishProviderState(clientID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":        "Chat session switched",
		"active_chat_id": chatID,
		"chat_session":   cs.chatSessionWithMessages(),
	})
}

// handleAPIChatSessionsCompact handles POST /api/chat-sessions/compact
// Body: { "id": "chat-id" }
// Triggers state compaction for the specified chat session via the agent.
func (ws *ReactWebServer) handleAPIChatSessionsCompact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := strings.TrimSpace(req.ID)
	if chatID == "" {
		chatID = ws.resolveChatID(r, clientID)
	}

	// Sync state for this chat session. Each chat has its own agent, so we
	// can compact any chat (not just the active one).
	if err := ws.syncAgentStateForClientWithChat(clientID, chatID); err != nil {
		http.Error(w, fmt.Sprintf("Failed to sync chat state: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Chat session state compacted",
		"chat_id": chatID,
	})
}

// handleAPIChatSessionClearHistory handles POST /api/chat-sessions/history.
// Body: { "id": "chat-id" }
// Clears the conversation messages for a chat session while keeping the session
// and its config overrides intact.
func (ws *ReactWebServer) handleAPIChatSessionClearHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := strings.TrimSpace(req.ID)
	if chatID == "" {
		chatID = ws.resolveChatID(r, clientID)
	}

	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	ws.mutex.RUnlock()

	if ctx == nil {
		http.Error(w, "Client context not found", http.StatusNotFound)
		return
	}

	cs := ctx.getChatSession(chatID)
	if cs == nil {
		http.Error(w, "Chat session not found", http.StatusNotFound)
		return
	}

	// Close the current session as a complete, restorable unit and start a new
	// one. This mirrors the CLI's /clear behaviour: the prior session file stays
	// loadable via LoadStateScoped, and the agent moves to a fresh SessionID so
	// the next auto-save does not overwrite the previous history.
	//
	// We must also update cs.CurrentSessionID (chat-session level) and
	// ctx.CurrentSessionID (client level) so /api/chat-sessions, the WS
	// session-restored event flow, and getCurrentSessionIDForRequest publish
	// the rotated ID — not the stale pre-rotation one. The switch handler
	// (handleAPIChatSessionsSwitch) maintains the same invariant.
	//
	// Both fields are updated under a single ws.mutex.Lock() with a fresh
	// re-read of ws.clientContexts[clientID] — not the ctx pointer captured
	// from the early nil-check above (which may be stale if the client context
	// was removed between the RLock and this write). The fork handler uses
	// the same pattern.
	var newSessionID string
	if agentInst, err := ws.getChatAgent(clientID, chatID); err == nil && agentInst != nil {
		rotatedID, rotateErr := agentInst.RotateSession()
		if rotateErr != nil {
			log.Printf("handleAPIChatSessionClearHistory: rotate failed for chat %s client %s: %v", chatID, clientID, rotateErr)
			writeJSONErr(w, http.StatusInternalServerError, "rotate_failed", "failed to rotate session")
			return
		}
		newSessionID = rotatedID

		ws.mutex.Lock()
		ctx2 := ws.clientContexts[clientID]
		if ctx2 != nil {
			if cs2 := ctx2.getChatSession(chatID); cs2 != nil {
				cs2.mu.Lock()
				cs2.CurrentSessionID = rotatedID
				cs2.mu.Unlock()
			}
			ctx2.CurrentSessionID = rotatedID
		}
		ws.mutex.Unlock()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"chat_id":    chatID,
		"session_id": newSessionID,
		"messages":   fmt.Sprintf("New session started for chat %s (previous session preserved)", chatID),
	})
}
