//go:build !js

// Package webui provides React web server with embedded assets
package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
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

// rejectIfSharedMode returns true (and writes a 403) when the server is in
// shared-agent mode and the requested operation would create/modify chat
// sessions beyond the single shared one. In shared mode, the WebUI is coupled
// to the CLI's agent — multi-chat doesn't make sense because there's only one
// agent instance and one conversation.
//
// Returns false (no rejection) when not in shared mode.
func (ws *ReactWebServer) rejectIfSharedMode(w http.ResponseWriter) bool {
	if !ws.IsSharedMode() {
		return false
	}
	writeJSONErr(w, http.StatusForbidden, "shared_mode",
		"Multi-chat is disabled in shared mode (CLI + WebUI coupled). Use the terminal or daemon mode for multiple chats.")
	return true
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
			"chat_id":            info.ID,
			"id":                 info.ID,
			"name":               info.Name,
			"created_at":         info.CreatedAt.UTC().Format(time.RFC3339),
			"last_active_at":     info.LastActiveAt.UTC().Format(time.RFC3339),
			"message_count":      info.MessageCount,
			"current_session_id": info.CurrentSessionID,
			"active_query":       info.ActiveQuery,
			"is_default":         info.ID == activeChatID,
			"is_active":          info.ID == activeChatID,
			"is_pinned":          info.IsPinned,
		}
		if info.Provider != "" {
			entry["provider"] = info.Provider
		}
		if info.Model != "" {
			entry["model"] = info.Model
		}
		if info.WorktreePath != "" {
			entry["worktree_path"] = info.WorktreePath
		}
		if info.ActiveQuery && info.CurrentQuery != "" {
			entry["current_query"] = info.CurrentQuery
		}
		sessionList = append(sessionList, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":        "success",
		"chat_sessions":  sessionList,
		"active_chat_id": activeChatID,
		"total_sessions": len(sessionList),
	})
}

// handleAPIChatSessionsCreate handles POST /api/chat-sessions/create
// Body: { "name": "optional name" } or { "id": "optional custom id", "name": "optional name" }
func (ws *ReactWebServer) handleAPIChatSessionsCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ws.rejectIfSharedMode(w) {
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
		"success":      true,
		"id":           chatID,
		"message":      "Chat session created",
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
	if ws.rejectIfSharedMode(w) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		ID             string `json:"id"`
		RemoveWorktree bool   `json:"remove_worktree,omitempty"`
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
	// --- Atomic validation-and-delete region ---
	//
	// CRITICAL: ws.mutex is held from the initial lookup through the delete
	// below. This prevents a concurrent handleAPIQuery from acquiring ws.mutex,
	// seeing the chat as inactive, and starting a query on a chat that's being
	// deleted.
	//
	// The cs.mu is only needed to read cs.ActiveQuery and cs.WorktreePath.
	// Once we have those values, cs.mu can be released because ws.mutex
	// prevents any concurrent setChatQueryActive from running (which is the
	// only path that modifies cs.ActiveQuery).
	//
	// After confirming the chat is inactive, we set the top-level ActiveQuery
	// flag to true BEFORE removing the chat from the map. This ensures that
	// any concurrent query handler that subsequently acquires the lock sees
	// hasActiveQueryForChat(chatID) == true (via the top-level fallback when
	// the chat is absent from the map) and rejects. We reset the flag after
	// releasing the lock so the window where ActiveQuery is spuriously true
	// is bounded to the time between ws.mutex.Unlock() and the re-acquire.
	cs.mu.Lock()
	isActive := cs.ActiveQuery
	worktreePath := cs.WorktreePath
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

	// Mark the chat as "being deleted" by setting the top-level ActiveQuery
	// flag. This prevents a concurrent query handler from starting a query on
	// this chat ID — hasActiveQueryForChat(chatID) falls back to
	// ctx.ActiveQuery when the chat is absent from the map.
	ctx.ActiveQuery = true
	ctx.CurrentQuery = ""

	workspaceRoot := ctx.WorkspaceRoot
	delete(ctx.ChatSessions, chatID)
	ws.mutex.Unlock()

	// Reset the top-level ActiveQuery flag based on remaining chats.
	// This must happen after releasing ws.mutex so that any concurrent query
	// handler that runs during the window sees ActiveQuery == true and rejects.
	ws.mutex.Lock()
	ctx = ws.clientContexts[clientID]
	if ctx != nil {
		anyActive := false
		for _, other := range ctx.ChatSessions {
			other.mu.RLock()
			if other.ActiveQuery {
				anyActive = true
				other.mu.RUnlock()
				break
			}
			other.mu.RUnlock()
		}
		ctx.ActiveQuery = anyActive
		if !anyActive {
			ctx.CurrentQuery = ""
		}
	}
	ws.mutex.Unlock()

	log.Printf("handleAPIChatSessionsDelete: deleted chat session %s for client %s", chatID, clientID)

	// Safety check: verify no other chat session uses this worktree path
	if req.RemoveWorktree && worktreePath != "" {
		ws.mutex.RLock()
		stillInUse := false
		for _, other := range ctx.ChatSessions {
			if other.getWorktreePath() == worktreePath {
				stillInUse = true
				break
			}
		}
		ws.mutex.RUnlock()
		if stillInUse {
			worktreePath = "" // Clear to skip removal
		}
	}

	// Optionally clean up the associated worktree
	resp := map[string]interface{}{
		"success":          true,
		"message":          "Chat session deleted",
		"id":               chatID,
		"worktree_removed": false,
		"worktree_error":   "",
	}

	if req.RemoveWorktree && worktreePath != "" {
		absWorktree, absErr := filepath.Abs(worktreePath)
		if absErr == nil {
			absWorkspace, _ := filepath.Abs(workspaceRoot)
			if absWorktree != absWorkspace {
				removeCmd := ws.gitCommandForWorkspace(absWorkspace, "worktree", "remove", absWorktree)
				removeOutput, removeErr := removeCmd.CombinedOutput()
				if removeErr != nil {
					log.Printf("handleAPIChatSessionsDelete: failed to remove worktree %s: %v\nOutput: %s",
						absWorktree, removeErr, string(removeOutput))
					resp["worktree_removed"] = false
					resp["worktree_error"] = string(removeOutput)
				} else {
					log.Printf("handleAPIChatSessionsDelete: removed worktree %s for chat session %s", absWorktree, chatID)
					resp["worktree_removed"] = true
				}
			} else {
				log.Printf("handleAPIChatSessionsDelete: skipping worktree removal, %s is the current workspace", absWorktree)
				resp["worktree_error"] = "Cannot remove the current workspace"
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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

	summary := cs.chatSessionSummary(false)
	ws.publishSessionChanged(clientID, chatID, "rename", summary)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"message":      "Chat session renamed",
		"chat_session": summary,
	})
}

// handleAPIChatSessionsPin handles POST /api/chat-sessions/pin
// Body: { "id": "chat-id" }
// Pins a chat session so it stays visible at the top of the tab bar.
func (ws *ReactWebServer) handleAPIChatSessionsPin(w http.ResponseWriter, r *http.Request) {
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Missing chat id",
			"code":  "missing_id",
		})
		return
	}

	clientID := ws.resolveClientID(r)

	ws.mutex.Lock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		ws.mutex.Unlock()
		http.Error(w, "Client context not found", http.StatusNotFound)
		return
	}

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

	cs.mu.Lock()
	cs.IsPinned = true
	pinned := cs.IsPinned
	cs.mu.Unlock()

	ws.mutex.Unlock()

	log.Printf("handleAPIChatSessionsPin: pinned chat session %s for client %s", chatID, clientID)

	summary := cs.chatSessionSummary(false)
	ws.publishSessionChanged(clientID, chatID, "pin", summary)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":      "Chat session pinned",
		"chat_session": summary,
		"is_pinned":    pinned,
	})
}

// handleAPIChatSessionsUnpin handles POST /api/chat-sessions/unpin
// Body: { "id": "chat-id" }
// Unpins a chat session so it can auto-close with other tabs.
func (ws *ReactWebServer) handleAPIChatSessionsUnpin(w http.ResponseWriter, r *http.Request) {
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Missing chat id",
			"code":  "missing_id",
		})
		return
	}

	clientID := ws.resolveClientID(r)

	ws.mutex.Lock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		ws.mutex.Unlock()
		http.Error(w, "Client context not found", http.StatusNotFound)
		return
	}

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

	cs.mu.Lock()
	cs.IsPinned = false
	pinned := cs.IsPinned
	cs.mu.Unlock()

	ws.mutex.Unlock()

	log.Printf("handleAPIChatSessionsUnpin: unpinned chat session %s for client %s", chatID, clientID)

	summary := cs.chatSessionSummary(false)
	ws.publishSessionChanged(clientID, chatID, "unpin", summary)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":      "Chat session unpinned",
		"chat_session": summary,
		"is_pinned":    pinned,
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

// handleAPIChatSessionsDeleteAll handles POST /api/chat-sessions/delete-all
// Deletes all chat sessions except the default one, then sets the default session as active.
func (ws *ReactWebServer) handleAPIChatSessionsDeleteAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ws.rejectIfSharedMode(w) {
		return
	}

	clientID := ws.resolveClientID(r)

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ensureDefaultChatSession()

	// Collect chat IDs to delete (non-default, non-active, non-active-query sessions)
	chatIDsToDelete := make([]string, 0, len(ctx.ChatSessions))
	for chatID, cs := range ctx.ChatSessions {
		if chatID == defaultChatID {
			continue // Never delete the default session
		}
		cs.mu.Lock()
		isActive := cs.ActiveQuery
		cs.mu.Unlock()
		if isActive {
			continue // Skip sessions with active queries
		}
		chatIDsToDelete = append(chatIDsToDelete, chatID)
	}

	// Delete all collected sessions
	deletedCount := 0
	for _, chatID := range chatIDsToDelete {
		delete(ctx.ChatSessions, chatID)
		deletedCount++
	}

	// Set the default session as active
	ctx.DefaultChatID = defaultChatID

	// Update client-level agent reference to point to the default session's agent
	defaultCS := ctx.ChatSessions[defaultChatID]
	if defaultCS != nil {
		defaultCS.mu.Lock()
		if defaultCS.Agent != nil {
			ctx.Agent = defaultCS.Agent
		} else {
			ctx.Agent = nil
		}
		// Sync the top-level agent state
		snapshot := defaultCS.AgentState
		if len(snapshot) == 0 {
			snapshot = emptyAgentStateSnapshot()
		}
		currentSessionID := defaultCS.CurrentSessionID
		wtPath := defaultCS.WorktreePath
		defaultCS.mu.Unlock()

		ctx.AgentState = append([]byte(nil), snapshot...)
		ctx.CurrentSessionID = currentSessionID

		// Switch workspace root to the default chat's worktree if it has one
		if wtPath != "" {
			ctx.WorkspaceRoot = wtPath
			if clientID == defaultWebClientID {
				ws.workspaceRoot = wtPath
			}
		}
	}

	ws.mutex.Unlock()

	log.Printf("handleAPIChatSessionsDeleteAll: deleted %d chat sessions for client %s, switched to default %s", deletedCount, clientID, defaultChatID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":        "Chat sessions deleted",
		"deleted_count":  deletedCount,
		"active_chat_id": defaultChatID,
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
		// ExportState can be slow for large conversations (JSON marshal of
		// the full message history). Do it OUTSIDE ws.mutex so other HTTP
		// requests and WebSocket read goroutines aren't blocked.
		snapshot, exportErr := chatAgent.ExportState()
		if exportErr != nil {
			return fmt.Errorf("export chat state: %w", exportErr)
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
		// If no provider is configured, that's expected — just return.
		if errors.Is(err, ErrNoProviderConfigured) {
			return nil
		}
		return fmt.Errorf("get client agent for chat state sync: %w", err)
	}

	// Same pattern: export outside the lock, store inside.
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
