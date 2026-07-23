//go:build !js

// Package webui: chat session creation (split from chat_sessions_api.go)
package webui

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

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

	ws.log().Info("created chat session", slog.String("chat_id", chatID), slog.String("name", name), slog.String("client_id", clientID))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"id":           chatID,
		"message":      "Chat session created",
		"chat_session": cs.chatSessionSummary(false),
	})
}
