// Package webui provides React web server with embedded assets
package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// resolveChatID reads the chat_id query param and falls back to the client's
// DefaultChatID. The caller must have resolved the clientID first.
func (ws *ReactWebServer) resolveChatID(r *http.Request, clientID string) string {
	chatID := strings.TrimSpace(r.URL.Query().Get("chat_id"))
	if chatID == "" {
		ws.mutex.RLock()
		if ctx := ws.clientContexts[clientID]; ctx != nil {
			chatID = ctx.getActiveChatID()
		}
		ws.mutex.RUnlock()
		if chatID == "" {
			chatID = defaultChatID
		}
	}
	return chatID
}

// handleAPIChatSessions handles GET /api/chat-sessions - lists all chat sessions
// for the requesting client with metadata.
func (ws *ReactWebServer) handleAPIChatSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := ws.resolveClientID(r)

	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		ws.mutex.RUnlock()
		// Create context so caller gets a valid response
		ctx = ws.getOrCreateClientContext(clientID)
		ws.mutex.RLock()
		ctx = ws.clientContexts[clientID]
	}

	activeChatID := ctx.getActiveChatID()
	sessions := ctx.listChatSessions()
	ws.mutex.RUnlock()

	// Build session summaries
	sessionList := make([]map[string]interface{}, 0, len(sessions))
	for _, info := range sessions {
		entry := map[string]interface{}{
			"id":                 info.ID,
			"name":               info.Name,
			"created_at":         info.CreatedAt.UTC().Format(time.RFC3339),
			"last_active_at":     info.LastActiveAt.UTC().Format(time.RFC3339),
			"message_count":      info.MessageCount,
			"current_session_id": info.CurrentSessionID,
			"active_query":       info.ActiveQuery,
			"is_default":         info.ID == activeChatID,
			"is_active":          info.ID == activeChatID,
		}
		if info.Provider != "" {
			entry["provider"] = info.Provider
		}
		if info.Model != "" {
			entry["model"] = info.Model
		}
		if info.ActiveQuery && info.CurrentQuery != "" {
			entry["current_query"] = info.CurrentQuery
		}
		sessionList = append(sessionList, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":         "success",
		"chat_sessions":   sessionList,
		"active_chat_id":  activeChatID,
		"total_sessions":  len(sessionList),
	})
}

// handleAPIChatSessionsCreate handles POST /api/chat-sessions/create
// Body: { "name": "optional name" } or { "id": "optional custom id", "name": "optional name" }
func (ws *ReactWebServer) handleAPIChatSessionsCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	clientID := ws.resolveClientID(r)

	// Generate a unique ID if not provided
	chatID := strings.TrimSpace(req.ID)
	if chatID == "" {
		chatID = generateChatID()
	}

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)

	// Ensure chat sessions are initialized
	ctx.ensureDefaultChatSession()

	// Check if a session with this ID already exists
	if _, ok := ctx.ChatSessions[chatID]; ok {
		ws.mutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Chat session with this ID already exists",
			"code":  "chat_session_exists",
			"id":    chatID,
		})
		return
	}

	// Generate name if not provided
	name := strings.TrimSpace(req.Name)
	if name == "" {
		ctx.nextChatNumber++
		name = "Chat " + strconv.Itoa(ctx.nextChatNumber)
	}

	cs := newChatSession(chatID, name)
	ctx.ChatSessions[chatID] = cs
	ws.mutex.Unlock()

	log.Printf("handleAPIChatSessionsCreate: created chat session %s (%s) for client %s", chatID, name, clientID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "Chat session created",
		"chat_session": cs.chatSessionSummary(false),
	})
}

// handleAPIChatSessionsDelete handles POST /api/chat-sessions/delete
// Body: { "id": "chat-id" }
func (ws *ReactWebServer) handleAPIChatSessionsDelete(w http.ResponseWriter, r *http.Request) {
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

	chatID := strings.TrimSpace(req.ID)
	if chatID == "" {
		http.Error(w, "Chat session ID is required", http.StatusBadRequest)
		return
	}

	clientID := ws.resolveClientID(r)

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ensureDefaultChatSession()

	if chatID == defaultChatID {
		ws.mutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Cannot delete the default chat session",
			"code":  "cannot_delete_default",
		})
		return
	}

	if chatID == ctx.DefaultChatID {
		ws.mutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Cannot delete the currently active chat session. Switch to another chat first.",
			"code":  "cannot_delete_active",
		})
		return
	}

	cs, exists := ctx.ChatSessions[chatID]
	if !exists {
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
	cs.mu.Lock()
	isActive := cs.ActiveQuery
	cs.mu.Unlock()
	if isActive {
		ws.mutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Cannot delete a chat session with an active query",
			"code":  "chat_session_active_query",
			"id":    chatID,
		})
		return
	}
	delete(ctx.ChatSessions, chatID)
	ws.mutex.Unlock()

	log.Printf("handleAPIChatSessionsDelete: deleted chat session %s for client %s", chatID, clientID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Chat session deleted",
		"id":      chatID,
	})
}

// handleAPIChatSessionsRename handles POST /api/chat-sessions/rename
// Body: { "id": "chat-id", "name": "new name" }
func (ws *ReactWebServer) handleAPIChatSessionsRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
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
	name := strings.TrimSpace(req.Name)
	if name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	clientID := ws.resolveClientID(r)

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ensureDefaultChatSession()

	if !ctx.renameChatSession(chatID, name) {
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

	// Get the updated session for the response
	cs := ctx.getChatSession(chatID)
	ws.mutex.Unlock()

	log.Printf("handleAPIChatSessionsRename: renamed chat session %s to %q for client %s", chatID, name, clientID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":      "Chat session renamed",
		"chat_session": cs.chatSessionSummary(false),
	})
}

// handleAPIChatSessionsSwitch handles POST /api/chat-sessions/switch
// Body: { "id": "chat-id" }
// Switches the active chat for this client. Returns the switched-to session
// state (agent_state snapshot, messages, etc.) so the frontend can populate the chat view.
func (ws *ReactWebServer) handleAPIChatSessionsSwitch(w http.ResponseWriter, r *http.Request) {
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
	cs.mu.Unlock()

	ctx.AgentState = append([]byte(nil), snapshot...)
	ctx.CurrentSessionID = currentSessionID
	ws.mutex.Unlock()

	log.Printf("handleAPIChatSessionsSwitch: switched to chat session %s for client %s", chatID, clientID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":         "Chat session switched",
		"active_chat_id":  chatID,
		"chat_session":    cs.chatSessionWithMessages(),
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

// syncAgentStateForClientWithChat is like syncAgentStateForClient but targets
// a specific chat session's state instead of the client's top-level state.
func (ws *ReactWebServer) syncAgentStateForClientWithChat(clientID, chatID string) error {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = defaultWebClientID
	}
	if chatID == "" {
		chatID = defaultChatID
	}

	// Try to get the chat-specific agent first.
	chatAgent, err := ws.getChatAgent(clientID, chatID)
	if err == nil && chatAgent != nil {
		snapshot, exportErr := chatAgent.ExportState()
		if exportErr != nil {
			return exportErr
		}
		ws.mutex.Lock()
		defer ws.mutex.Unlock()
		ctx := ws.getOrCreateClientContextLocked(clientID)
		ctx.setChatSessionState(chatID, snapshot)
		ctx.LastSeenAt = time.Now()
		if clientID == defaultWebClientID {
			ws.workspaceRoot = ctx.WorkspaceRoot
		}
		return nil
	}

	// Fallback to client-level agent (e.g. chat sessions not initialized).
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil {
		return fmt.Errorf("get client agent for chat state sync: %w", err)
	}

	snapshot, err := agentInst.ExportState()
	if err != nil {
		return fmt.Errorf("export agent state for chat: %w", err)
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.setChatSessionState(chatID, snapshot)
	ctx.LastSeenAt = time.Now()
	if clientID == defaultWebClientID {
		ws.workspaceRoot = ctx.WorkspaceRoot
	}
	return nil
}
