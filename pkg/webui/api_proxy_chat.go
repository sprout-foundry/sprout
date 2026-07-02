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

	"github.com/sprout-foundry/sprout/pkg/events"
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

// handleAPIProxyChatQuery handles normal query requests within the proxy chat endpoint
func (ws *ReactWebServer) handleAPIProxyChatQuery(w http.ResponseWriter, r *http.Request, clientID, chatID string, req *proxyChatRequest) {
	// Extract the query text from the last user message
	query := getLastUserMessage(req.Messages)
	if query == "" {
		log.Printf("handleAPIProxyChat: no user message found")
		http.Error(w, "Messages with a user role are required", http.StatusBadRequest)
		return
	}

	log.Printf("handleAPIProxyChat: processing query: %s", query)

	// Resolve workspace root with worktree awareness
	workspaceRoot := ws.resolveWorkspaceRootForChat(clientID, chatID)
	if workspaceRoot == "" {
		workspaceRoot = ws.getWorkspaceRootForRequest(r)
	}

	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		ws.mutex.RUnlock()
		http.Error(w, "Client context not found", http.StatusBadRequest)
		return
	}
	if ctx.hasActiveQueryForChat(chatID) {
		ws.mutex.RUnlock()
		http.Error(w, "A query is already running for this chat", http.StatusConflict)
		return
	}
	ws.mutex.RUnlock()

	clientAgent, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		if isProviderConfigError(err) {
			writeJSONErr(w, http.StatusServiceUnavailable, "no_provider", "AI features require a provider. Please configure one in settings.")
		} else {
			http.Error(w, fmt.Sprintf("failed to initialize chat agent: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// Apply per-query overrides: provider, model
	if req.Provider != "" {
		cm := ws.getConfigManager(r, w)
		if cm != nil {
			cm.EnrichCustomProviders()
			if providerType, err := cm.MapStringToClientType(req.Provider); err == nil {
				if serr := clientAgent.SetProvider(providerType); serr != nil {
					log.Printf("handleAPIProxyChat: failed to set provider %q: %v", req.Provider, serr)
				}
			} else {
				log.Printf("handleAPIProxyChat: invalid provider %q: %v", req.Provider, err)
			}
		}
	}
	if req.Model != "" {
		if err := clientAgent.SetModel(req.Model); err != nil {
			log.Printf("handleAPIProxyChat: failed to set model %q: %v", req.Model, err)
		}
	}

	// Apply per-query workspace root override
	if req.WorkspaceRoot != "" {
		workspaceRoot = req.WorkspaceRoot
	}

	// Apply per-query system prompt override
	if req.SystemPrompt != "" {
		clientAgent.SetSystemPrompt(req.SystemPrompt)
	}

	// Store CurrentQuery atomically with ActiveQuery
	ws.mutex.Lock()
	ws.queryCount++
	ws.activeQueries++
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		ctx.setChatQueryActive(chatID, true, query)
	}
	ws.mutex.Unlock()

	// Run the query asynchronously. The web UI consumes progress and completion via WebSocket.
	go func() {
		defer func() {
			ws.mutex.Lock()
			if ws.activeQueries > 0 {
				ws.activeQueries--
			}
			if ctx := ws.clientContexts[clientID]; ctx != nil {
				ctx.setChatQueryActive(chatID, false, "")
			}
			ws.mutex.Unlock()
		}()

		log.Printf("handleAPIProxyChat: calling ProcessQueryWithContinuity chat_id=%s provider=%s model=%s", chatID, clientAgent.GetProvider(), clientAgent.GetModel())
		queryStart := time.Now()
		clientAgent.SetWorkspaceRoot(workspaceRoot)
		_, err := clientAgent.ProcessQueryWithContinuity(query)
		queryDuration := time.Since(queryStart)

		// Record cost after query completes
		chargedCost := clientAgent.GetChargedCostTotal()
		tokenCost := clientAgent.GetTokenCostTotal()
		if chargedCost > 0 || tokenCost > 0 {
			providerName := clientAgent.GetProvider()
			GetCostStore().RecordCostWithBilling(
				providerName,
				clientAgent.GetModel(),
				clientAgent.GetSessionID(),
				chatID,
				clientAgent.GetSessionName(),
				clientAgent.GetWorkspaceRoot(),
				resolveBillingTypeForProvider(providerName),
				clientAgent.GetPromptTokens(),
				clientAgent.GetCompletionTokens(),
				chargedCost,
				tokenCost,
			)
		}

		_ = ws.syncAgentStateForClientWithChat(clientID, chatID)
		if err != nil {
			log.Printf("handleAPIProxyChat: ProcessQueryWithContinuity error chat_id=%s duration=%s err=%v", chatID, queryDuration, err)
			ws.publishClientEvent(clientID, events.EventTypeError, events.ErrorEvent("Query failed", err))
		} else {
			log.Printf("handleAPIProxyChat: completed chat_id=%s duration=%s prompt_tokens=%d completion_tokens=%d total_cost=%.6f",
				chatID, queryDuration,
				clientAgent.GetPromptTokens(), clientAgent.GetCompletionTokens(),
				clientAgent.GetTotalCost())
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"accepted":  true,
		"chat_id":   chatID,
		"timestamp": time.Now().Unix(),
	})
}

// handleAPIProxyChatStop handles POST /api/proxy/chat/stop - Foundry proxy chat stop endpoint
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

	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil || !ctx.hasActiveQueryForChat(chatID) {
		ws.mutex.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":            "ok",
			"already_completed": true,
			"timestamp":         time.Now().Unix(),
		})
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

	clientAgent.TriggerInterrupt()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"accepted":  true,
		"mode":      "stop",
		"chat_id":   chatID,
		"timestamp": time.Now().Unix(),
	})
}

// handleAPIProxyChatStatus handles GET /api/proxy/chat/status - Foundry proxy chat status endpoint
func (ws *ReactWebServer) handleAPIProxyChatStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := ws.resolveChatID(r, clientID)

	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	active := ctx != nil && ctx.hasActiveQueryForChat(chatID)
	ws.mutex.RUnlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active":  active,
		"chat_id": chatID,
	})
}
