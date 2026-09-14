//go:build !js

package webui

import (
	"net/http"
	"strings"
)

// handleAPIChatSessionMessages handles GET /api/chat-sessions/messages?chat_id=X
//
// Read-only fetch of a chat session's messages. Unlike /switch it does NOT
// change the client's active chat, agent pointer, or workspace root — it
// exists so a background chat pane (a chat buffer open in a non-active
// split pane) can refresh its transcript while events stream to it, without
// disturbing the active chat anywhere else in the UI.
//
// Response shape matches the switch endpoint's chat_session.messages so the
// frontend can reuse the same mapping code.
func (ws *ReactWebServer) handleAPIChatSessionMessages(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := strings.TrimSpace(r.URL.Query().Get("chat_id"))
	if chatID == "" {
		chatID = ws.resolveChatID(r, clientID)
	}

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ensureDefaultChatSession()
	cs := ctx.getChatSession(chatID)
	if cs == nil {
		ws.mutex.Unlock()
		writeJSONErr(w, http.StatusBadRequest, "chat_session_not_found", "Chat session not found")
		return
	}
	ws.mutex.Unlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"chat_id":      cs.ID,
		"chat_session": cs.chatSessionWithMessages(),
	})
}
