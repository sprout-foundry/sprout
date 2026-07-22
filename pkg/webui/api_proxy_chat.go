//go:build !js

package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// proxyChatRequest represents the Foundry proxy chat request format.
type proxyChatRequest struct {
	Provider      string             `json:"provider,omitempty"`
	Model         string             `json:"model,omitempty"`
	Messages      []proxyChatMessage `json:"messages"`
	Stream        bool               `json:"stream"` // Accepted for format compatibility; streaming is always used
	ChatID        string             `json:"chat_id,omitempty"`
	Steer         bool               `json:"steer,omitempty"`
	WorkspaceRoot string             `json:"workspace_root,omitempty"`
	SystemPrompt  string             `json:"system_prompt,omitempty"`
}

// proxyChatMessage represents a message in the chat format.
type proxyChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// getLastUserMessage extracts the last message with role "user" from the messages array.
// Returns empty string if no user message is found.
func getLastUserMessage(messages []proxyChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}

// handleAPIProxyChat handles POST /api/proxy/chat - Foundry proxy chat endpoint
func (ws *ReactWebServer) handleAPIProxyChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req proxyChatRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("handleAPIProxyChat: invalid JSON: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := ws.resolveChatID(r, clientID)

	// If chat_id is provided in the body, it takes precedence over the query param
	if req.ChatID != "" {
		chatID = req.ChatID
	}

	// If steer is true, delegate to steer logic
	if req.Steer {
		ws.handleAPIProxyChatSteer(w, r, clientID, chatID, &req)
		return
	}

	// Normal query processing
	ws.handleAPIProxyChatQuery(w, r, clientID, chatID, &req)
}

// handleAPIProxyChatSteer handles steer requests within the proxy chat endpoint
func (ws *ReactWebServer) handleAPIProxyChatSteer(w http.ResponseWriter, r *http.Request, clientID, chatID string, req *proxyChatRequest) {
	// Extract the query text from the last user message (already trimmed by getLastUserMessage)
	query := getLastUserMessage(req.Messages)
	if query == "" {
		log.Printf("handleAPIProxyChat: steer mode requires a user message")
		http.Error(w, "Steer mode requires a user message", http.StatusBadRequest)
		return
	}

	// Check for slash commands in steer mode - not allowed
	if strings.HasPrefix(query, "/") {
		http.Error(w, "Slash commands cannot be steered while a query is running", http.StatusBadRequest)
		return
	}

	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil || !ctx.hasActiveQueryForChat(chatID) {
		ws.mutex.RUnlock()
		http.Error(w, "No active query to steer", http.StatusConflict)
		return
	}
	ws.mutex.RUnlock()

	clientAgent, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		if isProviderConfigError(err) {
			writeJSONErr(w, http.StatusServiceUnavailable, "no_provider", "AI features require a provider. Please configure one in settings.")
		} else {
			http.Error(w, fmt.Sprintf("Failed to access chat agent: %v", err), http.StatusInternalServerError)
		}
		return
	}

	if err := clientAgent.InjectInputContext(query); err != nil {
		http.Error(w, fmt.Sprintf("Failed to steer active query: %v", err), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"accepted":  true,
		"mode":      "steer",
		"chat_id":   chatID,
		"timestamp": time.Now().Unix(),
	})
}

// handleAPIProxyChatQuery handles normal query requests within the proxy
// chat endpoint. Thin wrapper over the shared runChatQuery runner: the
// proxy-specific parsing (extracting the last user message from the
// messages[] array) stays here, the rest delegates. AllowSlashCommands
// is false because /api/proxy/chat is the LLM-only surface; the
// safe-surface for slash commands is /api/command/execute.
//
// After the consolidation, this endpoint now also gets:
//   - provider/model switch errors returned as 400 instead of silently
//     proceeding (the per-query override branches previously only
//     log.Printf'd and continued with the wrong model)
//   - chat_id stamped on the QueryFailed error event (was previously
//     only the client_id)
func (ws *ReactWebServer) handleAPIProxyChatQuery(w http.ResponseWriter, r *http.Request, clientID, chatID string, req *proxyChatRequest) {
	// Extract the query text from the last user message
	query := getLastUserMessage(req.Messages)
	if query == "" {
		log.Printf("handleAPIProxyChat: no user message found")
		http.Error(w, "Messages with a user role are required", http.StatusBadRequest)
		return
	}

	log.Printf("handleAPIProxyChat: processing query: %s", query)

	ws.runChatQuery(w, r, clientID, chatID, query, chatQueryOptions{
		Provider:           req.Provider,
		Model:              req.Model,
		WorkspaceRoot:      req.WorkspaceRoot,
		SystemPrompt:       req.SystemPrompt,
		AllowSlashCommands: false,
		EchoQueryInAccept:  false,
		LogTag:             "handleAPIProxyChat",
	})
}

// handleAPIProxyChatStop handles POST /api/proxy/chat/stop - Foundry
// proxy chat stop endpoint. Thin wrapper over the shared stopActiveQuery
// helper. After the consolidation this endpoint now cancels running
// subagents via GetSubagentRunner; before, it only TriggerInterrupted
// the primary agent, leaving subagents to drain for tens of seconds
// after the user pressed Stop.
func (ws *ReactWebServer) handleAPIProxyChatStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)

	// Parse optional chat_id from body if present
	var req struct {
		ChatID string `json:"chat_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := ws.resolveChatID(r, clientID)

	// If chat_id is provided in the body, it takes precedence
	if req.ChatID != "" {
		chatID = req.ChatID
	}

	ws.stopActiveQuery(w, r, clientID, chatID)
}

// handleAPIProxyChatStatus handles GET /api/proxy/chat/status - Foundry
// proxy chat status endpoint. Thin wrapper over the shared
// chatQueryStatus helper so /api/proxy/chat/status and
// /api/query/status stay byte-identical on the wire.
func (ws *ReactWebServer) handleAPIProxyChatStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := ws.resolveChatID(r, clientID)

	active := ws.chatQueryStatus(clientID, chatID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active":  active,
		"chat_id": chatID,
	})
}
