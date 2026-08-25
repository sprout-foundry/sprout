//go:build !js

// Package webui: chat session rename/pin/unpin (split from chat_sessions_api.go)
package webui

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// handleAPIChatSessionsRename handles POST /api/chat-sessions/rename
// Body: { "id": "chat-id", "name": "new name" }
func (ws *ReactWebServer) handleAPIChatSessionsRename(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	chatID := strings.TrimSpace(req.ID)
	if chatID == "" {
		writeJSONErr(w, http.StatusBadRequest, "chat_session_id_required", "Chat session ID is required")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeJSONErr(w, http.StatusBadRequest, "name_required", "Name is required")
		return
	}

	clientID := ws.resolveClientID(r)

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ensureDefaultChatSession()

	if !ctx.renameChatSession(chatID, name) {
		ws.mutex.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Chat session not found",
			"code":  "chat_session_not_found",
			"id":    chatID,
		})
		return
	}

	// Get the updated session for the response
	cs := ctx.getChatSession(chatID)
	ws.mutex.Unlock()

	ws.log().Info("renamed chat session", slog.String("chat_id", chatID), slog.String("name", name), slog.String("client_id", clientID))

	summary := cs.chatSessionSummary(false)
	ws.publishSessionChanged(clientID, chatID, "rename", summary)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"message":      "Chat session renamed",
		"chat_session": summary,
	})
}

// handleAPIChatSessionsPin handles POST /api/chat-sessions/pin
// Body: { "id": "chat-id" }
// Pins a chat session so it stays visible at the top of the tab bar.
func (ws *ReactWebServer) handleAPIChatSessionsPin(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	chatID := strings.TrimSpace(req.ID)
	if chatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
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
		writeJSONErr(w, http.StatusNotFound, "client_context_not_found", "Client context not found")
		return
	}

	cs := ctx.getChatSession(chatID)
	if cs == nil {
		ws.mutex.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
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

	ws.log().Info("pinned chat session", slog.String("chat_id", chatID), slog.String("client_id", clientID))

	summary := cs.chatSessionSummary(false)
	ws.publishSessionChanged(clientID, chatID, "pin", summary)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":      "Chat session pinned",
		"chat_session": summary,
		"is_pinned":    pinned,
	})
}

// handleAPIChatSessionsUnpin handles POST /api/chat-sessions/unpin
// Body: { "id": "chat-id" }
// Unpins a chat session so it can auto-close with other tabs.
func (ws *ReactWebServer) handleAPIChatSessionsUnpin(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	chatID := strings.TrimSpace(req.ID)
	if chatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
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
		writeJSONErr(w, http.StatusNotFound, "client_context_not_found", "Client context not found")
		return
	}

	cs := ctx.getChatSession(chatID)
	if cs == nil {
		ws.mutex.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
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

	ws.log().Info("unpinned chat session", slog.String("chat_id", chatID), slog.String("client_id", clientID))

	summary := cs.chatSessionSummary(false)
	ws.publishSessionChanged(clientID, chatID, "unpin", summary)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":      "Chat session unpinned",
		"chat_session": summary,
		"is_pinned":    pinned,
	})
}
