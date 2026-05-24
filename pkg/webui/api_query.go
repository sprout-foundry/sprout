//go:build !js

package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/events"
)

const (
	maxQueryBodyBytes    = 1 << 20  // 1 MiB
	maxFileWriteBodySize = 10 << 20 // 10 MiB
	maxFileReadSize      = 10 << 20 // 10 MiB
	consentTokenHeader   = "X-Sprout-Consent-Token"
)

// isProviderConfigError reports whether err originated from the agent
// creation path because no AI provider is configured (or the configured
// provider lacks credentials). The substrings mirror the error messages
// returned by pkg/agent and pkg/configuration.
func isProviderConfigError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNoProviderConfigured) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "provider recovery failed") ||
		strings.Contains(s, "failed to initialize provider") ||
		strings.Contains(s, "failed to select provider") ||
		strings.Contains(s, "provider_not_configured") ||
		strings.Contains(s, "no provider configured") ||
		strings.Contains(s, "editor mode is active")
}

func (ws *ReactWebServer) incrementActiveQueries(clientID string) {
	ws.mutex.Lock()
	ws.activeQueries++
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ActiveQuery = true
	ws.mutex.Unlock()
}

func (ws *ReactWebServer) incrementActiveQueriesWithQuery(clientID, currentQuery string) {
	ws.mutex.Lock()
	ws.activeQueries++
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ActiveQuery = true
	ctx.CurrentQuery = currentQuery
	ws.mutex.Unlock()
}

func (ws *ReactWebServer) decrementActiveQueries(clientID string) {
	ws.mutex.Lock()
	if ctx := ws.clientContexts[clientID]; ctx != nil && ctx.ActiveQuery {
		if ws.activeQueries > 0 {
			ws.activeQueries--
		}
		ctx.ActiveQuery = false
		ctx.CurrentQuery = ""
	}
	ws.mutex.Unlock()
}

func (ws *ReactWebServer) hasActiveQuery() bool {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	return ws.activeQueries > 0
}

func (ws *ReactWebServer) publishClientEvent(clientID, eventType string, data map[string]interface{}) {
	ws.publishClientEventWithChat(clientID, "", eventType, data)
}

// publishClientEventWithChat publishes an event to the event bus with client_id and optional chat_id.
// The chat_id is included in the event data so that WebSocket connections can filter events by chat session.
// In service mode, the user_id from the client context is also added for user isolation.
//
// For reattach-relevant event types (stream chunks, tool start/end, query
// frame events, errors), the event is also appended to the chat's
// runBuffer (SP-034-2a) so a browser tab that loses its WebSocket can
// reconnect with `?reattach=<chat-id>&after_seq=<n>` and replay anything
// it missed. The seq assigned by Append is stamped onto the event data as
// `__seq` so the WS subscriber forwards it to the client.
func (ws *ReactWebServer) publishClientEventWithChat(clientID, chatID, eventType string, data map[string]interface{}) {
	if ws.eventBus == nil {
		return
	}
	if data == nil {
		data = map[string]interface{}{}
	}
	if strings.TrimSpace(clientID) != "" {
		data["client_id"] = clientID
	}
	if strings.TrimSpace(chatID) != "" {
		data["chat_id"] = chatID
	}
	// Stamp user_id from client context for user isolation in service mode
	if userID := ws.userIDForClient(clientID); userID != "" {
		data["user_id"] = userID
	}

	if seq := ws.appendChatEventToRunBuffer(clientID, chatID, eventType, data); seq > 0 {
		data["__seq"] = seq
	}

	ws.eventBus.Publish(eventType, data)
}

// publishSessionChanged broadcasts a session_changed event for the given
// chat. The event reaches every connection subscribed to chatID (via the
// chatSubscribers registry — SP-034-3a/3c), so multi-tab views reconcile
// their local session state with the canonical server payload.
//
// SP-034-3e: emitted from rename / pin / unpin / switch handlers. The
// `change` field tags which mutation occurred so the client can react
// appropriately (e.g. visually flash a renamed tab title).
func (ws *ReactWebServer) publishSessionChanged(clientID, chatID, change string, summary map[string]interface{}) {
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeSessionChanged, map[string]interface{}{
		"change":  change,
		"summary": summary,
	})
}

// reattachBufferedEventTypes lists the event types that get persisted in
// the per-chat ring buffer for replay on reconnect. Picked deliberately:
// stream chunks and tool activity are the user-visible events a reconnect
// needs to recover; per-file changes and metrics are not.
var reattachBufferedEventTypes = map[string]struct{}{
	events.EventTypeQueryStarted:   {},
	events.EventTypeQueryProgress:  {},
	events.EventTypeQueryCompleted: {},
	events.EventTypeStreamChunk:    {},
	events.EventTypeToolStart:      {},
	events.EventTypeToolEnd:        {},
	events.EventTypeAgentMessage:   {},
	events.EventTypeError:          {},
}

// appendChatEventToRunBuffer pushes the event into the chat's run buffer
// if the type is reattach-relevant and the chatID resolves. Returns the
// assigned seq, or 0 when not buffered. Lazy-creates the buffer on the
// chat session.
func (ws *ReactWebServer) appendChatEventToRunBuffer(clientID, chatID, eventType string, data map[string]interface{}) int64 {
	if strings.TrimSpace(chatID) == "" {
		return 0
	}
	if _, ok := reattachBufferedEventTypes[eventType]; !ok {
		return 0
	}

	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	ws.mutex.RUnlock()
	if ctx == nil {
		return 0
	}
	cs := ctx.getChatSession(chatID)
	if cs == nil {
		return 0
	}

	cs.mu.Lock()
	if cs.runBuffer == nil {
		cs.runBuffer = newChatRunRingBuffer()
	}
	buf := cs.runBuffer

	// SP-034-2f: manage the TTL reset timer based on run lifecycle events.
	// A fresh query_started cancels any pending reset (we want to keep the
	// buffer alive across the run). A query_completed schedules a reset
	// for defaultRunBufferTTLAfterCompletion later, giving reconnecting
	// tabs that window to grab the trailing events before we drop them.
	switch eventType {
	case events.EventTypeQueryStarted:
		if cs.runBufferResetTimer != nil {
			cs.runBufferResetTimer.Stop()
			cs.runBufferResetTimer = nil
		}
	case events.EventTypeQueryCompleted:
		if cs.runBufferResetTimer != nil {
			cs.runBufferResetTimer.Stop()
		}
		cs.runBufferResetTimer = time.AfterFunc(defaultRunBufferTTLAfterCompletion, func() {
			cs.mu.Lock()
			b := cs.runBuffer
			cs.runBufferResetTimer = nil
			cs.mu.Unlock()
			if b != nil {
				b.Reset()
			}
		})
	}
	cs.mu.Unlock()

	return buf.Append(events.UIEvent{Type: eventType, Data: data})
}

// handleAPIQuery handles API queries to the agent
func (ws *ReactWebServer) handleAPIQuery(w http.ResponseWriter, r *http.Request) {
	log.Printf("handleAPIQuery called")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var query struct {
		Query         string `json:"query"`
		ChatID        string `json:"chat_id,omitempty"`
		Provider      string `json:"provider,omitempty"`
		Model         string `json:"model,omitempty"`
		WorkspaceRoot string `json:"workspace_root,omitempty"`
		SystemPrompt  string `json:"system_prompt,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
		log.Printf("handleAPIQuery: invalid JSON: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if query.Query == "" {
		log.Printf("handleAPIQuery: empty query")
		http.Error(w, "Query is required", http.StatusBadRequest)
		return
	}

	log.Printf("handleAPIQuery: processing query: %s", query.Query)
	clientID := ws.resolveClientID(r)
	
	// Resolve chat_id: prefer body parameter, fall back to query parameter
	chatID := strings.TrimSpace(query.ChatID)
	if chatID == "" {
		chatID = ws.resolveChatID(r, clientID)
	}

	// Resolve workspace root with worktree awareness - check if chat has a worktree path
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
	if query.Provider != "" {
		cm := ws.getConfigManager(r, w)
		if cm != nil {
			// Enrich custom providers from disk before mapping — the config
			// manager may not have them loaded if it was created via fallback.
			cm.EnrichCustomProviders()
			if providerType, err := cm.MapStringToClientType(query.Provider); err == nil {
				if serr := clientAgent.SetProvider(providerType); serr != nil {
					log.Printf("handleAPIQuery: failed to set provider %q: %v", query.Provider, serr)
				}
			} else {
				log.Printf("handleAPIQuery: invalid provider %q: %v", query.Provider, err)
			}
		}
	}
	if query.Model != "" {
		if err := clientAgent.SetModel(query.Model); err != nil {
			log.Printf("handleAPIQuery: failed to set model %q: %v", query.Model, err)
		}
	}

	// Apply per-query workspace root override
	if query.WorkspaceRoot != "" {
		workspaceRoot = query.WorkspaceRoot
	}

	// Apply per-query system prompt override (session-scoped, resets after query not needed)
	if query.SystemPrompt != "" {
		clientAgent.SetSystemPrompt(query.SystemPrompt)
	}

	// Store CurrentQuery atomically with ActiveQuery so that stats responses
	// include it on reconnect without a TOCTOU window.
	ws.mutex.Lock()
	ws.queryCount++
	ws.activeQueries++
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		ctx.setChatQueryActive(chatID, true, query.Query)
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
		startedAt := time.Now()
		registry := agent_commands.NewCommandRegistry()

		if registry.IsSlashCommand(query.Query) {
			log.Printf("handleAPIQuery: executing slash command: %s", query.Query)
			queryEventData := events.QueryStartedEvent(
				query.Query,
				clientAgent.GetProvider(),
				clientAgent.GetModel(),
			)
			ws.publishClientEventWithChat(clientID, chatID, events.EventTypeQueryStarted, queryEventData)

			clientAgent.SetWorkspaceRoot(workspaceRoot)
			err := registry.Execute(query.Query, clientAgent)
			_ = ws.syncAgentStateForClientWithChat(clientID, chatID)
			if err != nil {
				log.Printf("handleAPIQuery: slash command error: %v", err)
				ws.publishClientEventWithChat(clientID, chatID, events.EventTypeError, events.ErrorEvent("Slash command failed", err))
				return
			}

			trimmed := strings.TrimSpace(query.Query)
			ws.publishClientEventWithChat(clientID, chatID, events.EventTypeStreamChunk, events.StreamChunkEvent(
				fmt.Sprintf("Executed command: `%s`\n", trimmed),
				"assistant_text",
			))
			queryCompletedData := events.QueryCompletedEvent(
				query.Query,
				fmt.Sprintf("Executed command: %s", trimmed),
				0,
				0,
				time.Since(startedAt),
			)
			ws.publishClientEventWithChat(clientID, chatID, events.EventTypeQueryCompleted, queryCompletedData)
			return
		}

		log.Printf("handleAPIQuery: calling ProcessQueryWithContinuity chat_id=%s provider=%s model=%s", chatID, clientAgent.GetProvider(), clientAgent.GetModel())
		queryStart := time.Now()
		clientAgent.SetWorkspaceRoot(workspaceRoot)
		_, err := clientAgent.ProcessQueryWithContinuity(query.Query)
		queryDuration := time.Since(queryStart)

		// Record cost after query completes
		if cost := clientAgent.GetTotalCost(); cost > 0 {
			GetCostStore().RecordCost(
				clientAgent.GetProvider(),
				clientAgent.GetModel(),
				clientAgent.GetSessionID(),
				chatID,
				clientAgent.GetPromptTokens(),
				clientAgent.GetCompletionTokens(),
				cost,
			)
		}

		_ = ws.syncAgentStateForClientWithChat(clientID, chatID)
		if err != nil {
			log.Printf("handleAPIQuery: ProcessQueryWithContinuity error chat_id=%s duration=%s err=%v", chatID, queryDuration, err)
			ws.publishClientEventWithChat(clientID, chatID, events.EventTypeError, events.ErrorEvent("Query failed", err))
		} else {
			// Success-path log: lets operators see that the provider responded
			// and at what cost. Without this the log goes silent after
			// "calling ProcessQueryWithContinuity" and the server looks hung.
			log.Printf("handleAPIQuery: completed chat_id=%s duration=%s prompt_tokens=%d completion_tokens=%d total_cost=%.6f",
				chatID, queryDuration,
				clientAgent.GetPromptTokens(), clientAgent.GetCompletionTokens(),
				clientAgent.GetTotalCost())
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"accepted":  true,
		"query":     query.Query,
		"chat_id":   chatID,
		"timestamp": time.Now().Unix(),
	})
}

// handleAPIQuerySteer injects user input into the currently running query loop.
func (ws *ReactWebServer) handleAPIQuerySteer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var query struct {
		Query string `json:"query"`
	}

	if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	query.Query = strings.TrimSpace(query.Query)
	if query.Query == "" {
		http.Error(w, "Query is required", http.StatusBadRequest)
		return
	}

	if strings.HasPrefix(query.Query, "/") {
		http.Error(w, "Slash commands cannot be steered while a query is running", http.StatusBadRequest)
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := ws.resolveChatID(r, clientID)

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

	if err := clientAgent.InjectInputContext(query.Query); err != nil {
		http.Error(w, fmt.Sprintf("Failed to steer active query: %v", err), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"accepted":  true,
		"mode":      "steer",
		"query":     query.Query,
		"timestamp": time.Now().Unix(),
	})
}

// handleAPIQueryStop interrupts the currently running query loop.
func (ws *ReactWebServer) handleAPIQueryStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := ws.resolveChatID(r, clientID)

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
		"timestamp": time.Now().Unix(),
	})
}

// handleAPIQueryStatus handles GET /api/query/status?chat_id=xxx
// Returns whether a query is currently active for the specified chat.
// This is a polling fallback for when the WebSocket drops and reconnects.
func (ws *ReactWebServer) handleAPIQueryStatus(w http.ResponseWriter, r *http.Request) {
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

