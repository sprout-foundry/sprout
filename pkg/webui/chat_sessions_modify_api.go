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

	ws.log().Info("renamed chat session", slog.String("chat_id", chatID), slog.String("name", name), slog.String("client_id", clientID))

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

	ws.log().Info("pinned chat session", slog.String("chat_id", chatID), slog.String("client_id", clientID))

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

	ws.log().Info("unpinned chat session", slog.String("chat_id", chatID), slog.String("client_id", clientID))

	summary := cs.chatSessionSummary(false)
	ws.publishSessionChanged(clientID, chatID, "unpin", summary)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":      "Chat session unpinned",
		"chat_session": summary,
		"is_pinned":    pinned,
	})
}
