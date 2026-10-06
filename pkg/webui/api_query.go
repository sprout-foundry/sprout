//go:build !js

package webui

// api_query.go — the webui query API: the active-query counters, the
// client-event publishing + session-update hooks, the run-buffer append, and
// the query handlers. handleAPIQuery remains a plain handler (tests call it by
// name); /api/query/stop and /api/query/status are now Huma operations that
// drive the buildAPIQueryStop / buildAPIQueryStatus builders. The steer +
// steer-retract handlers and their streaming execution live in
// api_query_steer.go.

import (
	"context"
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
	events.EventTypeSteerDelivered: {},
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

// queryRequest is the request body for POST /api/query. It mirrors the
// QueryRequest schema documented in docs/api/openapi.base.yaml.
type queryRequest struct {
	Query         string `json:"query"`
	ChatID        string `json:"chat_id,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Model         string `json:"model,omitempty"`
	WorkspaceRoot string `json:"workspace_root,omitempty"`
	SystemPrompt  string `json:"system_prompt,omitempty"`
}

// buildAPIQuery is the shared backend for POST /api/query. It validates the
// request, resolves the client and chat IDs, and hands off to the shared
// runChatQuery runner — which owns the full query lifecycle (locking, agent
// creation, per-query overrides, the slash-command-in-chat path, and the async
// ProcessQueryWithContinuity goroutine) and writes the response itself. The
// response is written through w, so the caller must pass the live
// ResponseWriter. Keeping the slash-command-in-chat path inside the shared
// runner (gated by AllowSlashCommands=true) means the /api/query surface still
// lets users type `/info` in a fresh chat input; the runner rejects
// destructive commands via SteerCapable. See SP-114 Phase 2.
func (ws *ReactWebServer) buildAPIQuery(w http.ResponseWriter, r *http.Request, q queryRequest) {
	if q.Query == "" {
		writeJSONErr(w, http.StatusBadRequest, "query_required", "Query is required")
		return
	}

	clientID := ws.resolveClientID(r)

	// Resolve chat_id: prefer body parameter, fall back to query parameter.
	chatID := strings.TrimSpace(q.ChatID)
	if chatID == "" {
		chatID = ws.resolveChatID(r, clientID)
	}

	ws.runChatQuery(w, r, clientID, chatID, q.Query, chatQueryOptions{
		Provider:           q.Provider,
		Model:              q.Model,
		WorkspaceRoot:      q.WorkspaceRoot,
		SystemPrompt:       q.SystemPrompt,
		AllowSlashCommands: true,
		EchoQueryInAccept:  true,
		LogTag:             "handleAPIQuery",
	})
}

// handleAPIQuery handles POST /api/query. It is a thin wrapper over
// buildAPIQuery — the body parsing and method gate stay here (the plain
// route) so the request schema is documented in one place, and the shared
// builder runs the real logic. The Huma operation (registerHumaOperations in
// routes.go) drives the same builder, so the two surfaces cannot drift.
func (ws *ReactWebServer) handleAPIQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var q queryRequest
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		ws.log().Warn("invalid query JSON", slog.String("err", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	ws.buildAPIQuery(w, r, q)
}

// queryInput is the input for the Huma POST /api/query operation. It carries
// the raw request and response writer (via humaRequestInput) so the handler
// can parse the body and reuse the existing helpers, keeping the request
// schema in the hand-written seed rather than the generated doc.
type queryInput struct {
	humaRequestInput
}

// queryHumaHandler is the Huma handler for POST /api/query. It parses the body
// exactly as the plain handler did (MaxBytesReader + JSON decode) and writes
// the response through in.Resp, so the migrated operation is byte-identical
// to the plain handler it replaces. The response is written by the runner, so
// the output body is a no-op callback (Huma writes nothing of its own).
func (ws *ReactWebServer) queryHumaHandler(ctx context.Context, in *queryInput) (*writtenResponseOutput, error) {
	w := in.Resp
	r := in.Req

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var q queryRequest
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		ws.log().Warn("invalid query JSON", slog.String("err", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return &writtenResponseOutput{Body: noopWrittenResponse}, nil
	}

	ws.buildAPIQuery(w, r, q)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// buildAPIQueryStop is the shared backend for POST /api/query/stop. It resolves
// the client and chat IDs and delegates to the shared stopActiveQuery helper,
// which owns the active-state lookup, agent resolution, TriggerInterrupt, and
// subagent cancellation, and writes the response itself through w. The HTTP
// method gate is enforced by the Huma operation (registerHumaOperations in
// routes.go); a wrong method reaches the SPA catch-all and 404s.
func (ws *ReactWebServer) buildAPIQueryStop(w http.ResponseWriter, r *http.Request) {
	clientID := ws.resolveClientID(r)
	chatID := ws.resolveChatID(r, clientID)

	ws.stopActiveQuery(w, r, clientID, chatID)
}

// queryStopInput is the input for the Huma POST /api/query/stop operation.
type queryStopInput struct {
	humaRequestInput
}

// queryStopHumaHandler is the Huma handler for POST /api/query/stop. It drives
// the shared builder through the live ResponseWriter so the response is
// byte-identical to the plain handler; the body is a no-op callback.
func (ws *ReactWebServer) queryStopHumaHandler(ctx context.Context, in *queryStopInput) (*writtenResponseOutput, error) {
	ws.buildAPIQueryStop(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// buildAPIQueryStatus is the shared backend for GET /api/query/status. It
// reports whether a query is currently active for the chat (the polling
// fallback for when the WebSocket drops and reconnects) and writes the result.
// The HTTP method gate is enforced by the Huma operation (registerHumaOperations
// in routes.go); a wrong method reaches the SPA catch-all and 404s.
func (ws *ReactWebServer) buildAPIQueryStatus(w http.ResponseWriter, r *http.Request) {
	clientID := ws.resolveClientID(r)
	chatID := ws.resolveChatID(r, clientID)

	active := ws.chatQueryStatus(clientID, chatID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active":  active,
		"chat_id": chatID,
	})
}

// queryStatusInput is the input for the Huma GET /api/query/status operation.
type queryStatusInput struct {
	humaRequestInput
}

// queryStatusHumaHandler is the Huma handler for GET /api/query/status. It
// drives the shared builder through the live ResponseWriter so the response is
// byte-identical to the plain handler; the body is a no-op callback.
func (ws *ReactWebServer) queryStatusHumaHandler(ctx context.Context, in *queryStatusInput) (*writtenResponseOutput, error) {
	ws.buildAPIQueryStatus(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}
