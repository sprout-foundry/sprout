//go:build !js

package webui

// api_query.go — the webui query API: the active-query counters, the
// client-event publishing + session-update hooks, the run-buffer append, and
// the handleAPIQuery / handleAPIQueryStop / handleAPIQueryStatus handlers.
// The steer + steer-retract handlers and their streaming execution live in
// api_query_steer.go.

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

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

// publishAgentSessionUpdate fans a background-session lifecycle event out
// to the client that owns the session's chat. Routing: the event carries
// chat_id, so WebSocket subscribers scoped per chat receive it; the
// fallback (unknown chat) publishes to the default client context.
func (ws *ReactWebServer) publishAgentSessionUpdate(event map[string]interface{}) {
	if ws.eventBus == nil {
		return
	}
	clientID := defaultWebClientID
	chatID := ""
	if v, ok := event["chat_id"].(string); ok {
		chatID = v
	}

	// Find the client context that owns this chat, so the event routes to
	// the right browser tab in multi-client service mode.
	ws.mutex.RLock()
	for id, ctx := range ws.clientContexts {
		if ctx == nil {
			continue
		}
		if _, ok := ctx.ChatSessions[chatID]; ok {
			clientID = id
			break
		}
	}
	ws.mutex.RUnlock()

	ws.publishClientEventWithChat(clientID, chatID, "agent_session_update", event)
}

// installSessionUpdateHook wires the TerminalManager's lifecycle hook to
// publishAgentSessionUpdate. Called wherever a client context receives a
// TerminalManager (creation and workspace switch).
func (ws *ReactWebServer) installSessionUpdateHook(tm *TerminalManager) {
	if tm == nil {
		return
	}
	tm.SetSessionUpdateHook(ws.publishAgentSessionUpdate)
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

// handleAPIQuery handles API queries to the agent. It is a thin
// wrapper over runChatQuery — the body parsing and chat-id resolution
// stay here so the request schema (query, chat_id, provider, model,
// workspace_root, system_prompt) is documented in one place. The
// shared runner handles locking, agent creation, override application,
// the slash-command-in-chat path, and the async ProcessQueryWithContinuity
// goroutine with cost recording and state sync.
//
// Keeping the slash-command-in-chat path inside the shared runner
// (gated by opts.AllowSlashCommands=true here) means the legacy
// /api/query surface still lets users type `/info` in a fresh chat
// input; the runner rejects destructive commands via SteerCapable.
// See SP-114 Phase 2 for the gating rationale.
func (ws *ReactWebServer) handleAPIQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
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
		ws.log().Warn("invalid query JSON", slog.String("err", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	if query.Query == "" {
		writeJSONErr(w, http.StatusBadRequest, "query_required", "Query is required")
		return
	}

	clientID := ws.resolveClientID(r)

	// Resolve chat_id: prefer body parameter, fall back to query parameter
	chatID := strings.TrimSpace(query.ChatID)
	if chatID == "" {
		chatID = ws.resolveChatID(r, clientID)
	}

	ws.runChatQuery(w, r, clientID, chatID, query.Query, chatQueryOptions{
		Provider:           query.Provider,
		Model:              query.Model,
		WorkspaceRoot:      query.WorkspaceRoot,
		SystemPrompt:       query.SystemPrompt,
		AllowSlashCommands: true,
		EchoQueryInAccept:  true,
		LogTag:             "handleAPIQuery",
	})
}

// handleAPIQueryStop interrupts the currently running query loop. Thin
// wrapper over the shared stopActiveQuery helper — the body parsing
// and chat-id resolution stay here so the HTTP method gate runs first,
// then the shared helper handles active-state lookup, agent
// resolution, TriggerInterrupt, and subagent cancellation.
func (ws *ReactWebServer) handleAPIQueryStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := ws.resolveChatID(r, clientID)

	ws.stopActiveQuery(w, r, clientID, chatID)
}

// handleAPIQueryStatus handles GET /api/query/status?chat_id=xxx
// Returns whether a query is currently active for the specified chat.
// This is a polling fallback for when the WebSocket drops and reconnects.
// Thin wrapper over the shared chatQueryStatus helper.
func (ws *ReactWebServer) handleAPIQueryStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
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
