package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	agent_commands "github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/alantheprice/ledit/pkg/events"
)

const (
	maxQueryBodyBytes    = 1 << 20  // 1 MiB
	maxFileWriteBodySize = 10 << 20 // 10 MiB
	maxFileReadSize      = 10 << 20 // 10 MiB
	consentTokenHeader   = "X-Ledit-Consent-Token"
)

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
	if ws.activeQueries > 0 {
		ws.activeQueries--
	}
	if ctx := ws.clientContexts[clientID]; ctx != nil {
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
	if ws.eventBus == nil {
		return
	}
	if data == nil {
		data = map[string]interface{}{}
	}
	if strings.TrimSpace(clientID) != "" {
		data["client_id"] = clientID
	}
	ws.eventBus.Publish(eventType, data)
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
		Query string `json:"query"`
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
	chatID := ws.resolveChatID(r, clientID)

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
		http.Error(w, fmt.Sprintf("failed to initialize chat agent: %v", err), http.StatusInternalServerError)
		return
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
			queryEventData["chat_id"] = chatID
			ws.publishClientEvent(clientID, events.EventTypeQueryStarted, queryEventData)

			clientAgent.SetWorkspaceRoot(workspaceRoot)
			err := registry.Execute(query.Query, clientAgent)
			_ = ws.syncAgentStateForClientWithChat(clientID, chatID)
			if err != nil {
				log.Printf("handleAPIQuery: slash command error: %v", err)
				ws.publishClientEvent(clientID, events.EventTypeError, events.ErrorEvent("Slash command failed", err))
				return
			}

			trimmed := strings.TrimSpace(query.Query)
			ws.publishClientEvent(clientID, events.EventTypeStreamChunk, events.StreamChunkEvent(
				fmt.Sprintf("Executed command: `%s`\n", trimmed),
				"assistant_text",
			))
			ws.publishClientEvent(clientID, events.EventTypeQueryCompleted, events.QueryCompletedEvent(
				query.Query,
				fmt.Sprintf("Executed command: %s", trimmed),
				0,
				0,
				time.Since(startedAt),
			))
			return
		}

		log.Printf("handleAPIQuery: calling ProcessQueryWithContinuity")
		clientAgent.SetWorkspaceRoot(workspaceRoot)
		_, err := clientAgent.ProcessQueryWithContinuity(query.Query)
		_ = ws.syncAgentStateForClientWithChat(clientID, chatID)
		if err != nil {
			log.Printf("handleAPIQuery: ProcessQueryWithContinuity error: %v", err)
			ws.publishClientEvent(clientID, events.EventTypeError, events.ErrorEvent("Query failed", err))
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
		http.Error(w, fmt.Sprintf("Failed to access chat agent: %v", err), http.StatusInternalServerError)
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
		http.Error(w, fmt.Sprintf("Failed to access chat agent: %v", err), http.StatusInternalServerError)
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

