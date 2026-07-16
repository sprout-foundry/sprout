//go:build !js

// Package webui: shared helpers + list endpoint (split from chat_sessions_api.go)
package webui

import (
	"encoding/json"
	"net/http"
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
