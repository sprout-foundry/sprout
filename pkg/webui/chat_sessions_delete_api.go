//go:build !js

// Package webui: chat session deletion (split from chat_sessions_api.go)
package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

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
