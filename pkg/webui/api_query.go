//go:build !js

package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sprout-foundry/sprout/pkg/agent"
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

// handleAPIQuerySteer injects user input into the currently running query loop.
func (ws *ReactWebServer) handleAPIQuerySteer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var query struct {
		Query string `json:"query"`
	}

	if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	query.Query = strings.TrimSpace(query.Query)
	if query.Query == "" {
		writeJSONErr(w, http.StatusBadRequest, "query_required", "Query is required")
		return
	}

	// Handle slash commands during active query
	if strings.HasPrefix(query.Query, "/") {
		clientID := ws.resolveClientID(r)
		chatID := ws.resolveChatID(r, clientID)

		ws.mutex.RLock()
		ctx := ws.clientContexts[clientID]
		hasActiveQuery := ctx != nil && ctx.hasActiveQueryForChat(chatID)
		ws.mutex.RUnlock()

		if !hasActiveQuery {
			writeJSONErr(w, http.StatusConflict, "no_active_query", "No active query to steer")
			return
		}

		clientAgent, err := ws.getChatAgent(clientID, chatID)
		if err != nil {
			if isProviderConfigError(err) {
				writeJSONErr(w, http.StatusServiceUnavailable, "no_provider", "AI features require a provider. Please configure one in settings.")
			} else {
				writeJSONErr(w, http.StatusInternalServerError, "agent_access_failed", fmt.Sprintf("Failed to access chat agent: %v", err))
			}
			return
		}

		// Try to execute safe steer command
		cmd, output, cmdErr := ws.executeSafeSteerCommand(query.Query, clientAgent)
		if cmd != nil {
			// Command was found and executed (success or error)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			resp := map[string]interface{}{
				"accepted": cmdErr == nil,
				"mode":     "steer",
				"command":  cmd.Name(),
				"target":   "primary",
			}
			if output != "" {
				resp["output"] = output
			}
			if cmdErr != nil {
				resp["error"] = cmdErr.Error()
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// Command not found or not safe to run mid-turn
		writeJSONErr(w, http.StatusBadRequest, "slash_command_not_steerable", "Slash commands cannot be steered while a query is running")
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := ws.resolveChatID(r, clientID)

	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil || !ctx.hasActiveQueryForChat(chatID) {
		ws.mutex.RUnlock()
		writeJSONErr(w, http.StatusConflict, "no_active_query", "No active query to steer")
		return
	}
	ws.mutex.RUnlock()

	clientAgent, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		if isProviderConfigError(err) {
			writeJSONErr(w, http.StatusServiceUnavailable, "no_provider", "AI features require a provider. Please configure one in settings.")
		} else {
			writeJSONErr(w, http.StatusInternalServerError, "agent_access_failed", fmt.Sprintf("Failed to access chat agent: %v", err))
		}
		return
	}

	// SP-059 Phase 1b / SP-094-8: if a subagent is the active executor,
	// route the steer via InjectInputIntoActive. This now prefers the
	// primary agent first (the parent decides whether to abort subagents,
	// redirect them, or fold the steer into its own plan). Only if the
	// primary's channel is full does it fall back to the deepest running
	// subagent.
	target := "primary"
	subagentID := ""
	delivered := false
	if runner := clientAgent.GetSubagentRunner(); runner != nil {
		if id, ok := runner.InjectInputIntoActive(query.Query); ok {
			delivered = true
			if id == "primary" {
				target = "primary"
			} else {
				target = "subagent"
				subagentID = id
			}
		}
	}
	if !delivered {
		// No runner or runner couldn't deliver — fall back to primary directly.
		if err := clientAgent.InjectInputContext(query.Query); err != nil {
			ws.log().Error("steer failed",
				slog.String("handler", "handleAPIQuerySteer"),
				slog.String("chat_id", chatID),
				slog.String("client_id", clientID),
				slog.Any("err", err),
			)
			writeJSONErr(w, http.StatusConflict, "steer_failed", fmt.Sprintf("Failed to steer active query: %v", err))
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	resp := map[string]interface{}{
		"accepted":  true,
		"mode":      "steer",
		"query":     query.Query,
		"target":    target,
		"timestamp": time.Now().Unix(),
	}
	if subagentID != "" {
		resp["subagent_id"] = subagentID
	}
	ws.log().Info("query steered",
		slog.String("handler", "handleAPIQuerySteer"),
		slog.String("chat_id", chatID),
		slog.String("client_id", clientID),
		slog.String("target", target),
	)
	json.NewEncoder(w).Encode(resp)
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

// executeSafeSteerCommand tries to execute a slash command mid-turn.
// Returns (cmd, output, error) where:
//   - cmd is the command if found and executed
//   - output is the captured stdout from the command
//   - error is any execution error
//   - (nil, "", nil) is returned if the command was not found or not safe
func (ws *ReactWebServer) executeSafeSteerCommand(input string, chatAgent *agent.Agent) (agent_commands.Command, string, error) {
	return ws.executeSafeSteerCommandStreaming(input, chatAgent, nil)
}

// executeSafeSteerCommandStreaming is the streaming variant of
// executeSafeSteerCommand (SP-114 Phase 2c). When onChunk is non-nil it
// receives each UTF-8-safe chunk from the command's configured output
// writer, in addition to being appended to the aggregated output string.
// When onChunk is nil the behavior is byte-for-byte identical to the
// non-streaming executeSafeSteerCommand — the /api/query/steer call
// site relies on this and uses the non-streaming entry point.
//
// onChunk is invoked from a goroutine that reads the command output pipe
// concurrently with Execute. It MUST be safe to call concurrently with
// the rest of the program; in particular it must not block on slow
// consumers (callers are expected to fan out to the WebSocket
// non-blockingly via the event bus). The reader goroutine exits once
// Execute returns and writeEnd is closed; onChunk will not be called
// after this function returns.
func (ws *ReactWebServer) executeSafeSteerCommandStreaming(input string, chatAgent *agent.Agent, onChunk func(string)) (agent_commands.Command, string, error) {
	// Parse command name from input
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return nil, "", nil
	}
	parts := strings.Fields(trimmed[1:]) // Remove leading /
	if len(parts) == 0 {
		return nil, "", nil
	}
	cmdName := parts[0]

	// Get the registry from the agent
	registryRaw := chatAgent.SlashCommands()
	if registryRaw == nil {
		return nil, "", nil
	}
	registry, ok := registryRaw.(*agent_commands.CommandRegistry)
	if !ok {
		return nil, "", nil
	}

	cmd, ok := registry.GetCommand(cmdName)
	if !ok {
		return nil, "", nil
	}

	// Check if command is safe to run mid-turn
	sc, ok := cmd.(agent_commands.SteerCapable)
	if !ok || !sc.SafeDuringSteer() {
		return nil, "", nil
	}

	// Capture command output without redirecting process-global os.Stdout.
	// The registry wires this invocation-local writer into OutputCommand
	// implementations, so commands from different clients can run concurrently.
	readEnd, writeEnd, pipeErr := os.Pipe()
	if pipeErr != nil {
		ws.log().Error("command output pipe creation failed",
			slog.String("handler", "executeSafeSteerCommandStreaming"),
			slog.Any("err", pipeErr),
		)
		return cmd, "", fmt.Errorf("create command output pipe: %w", pipeErr)
	}
	registry.SetOutput(writeEnd)

	var cmdErr error

	// Always drain concurrently: a command can exceed the OS pipe buffer even
	// when no streaming callback was requested.
	buf := new(strings.Builder)
	done := make(chan struct{})
	go func() {
		defer close(done)
		streamPipeChunks(readEnd, buf, onChunk)
	}()

	// Execute through the registry so it wires the output writer using the
	// same path as normal slash-command dispatch. Panic recovery ensures the
	// pipe and retained command writer are still cleaned up.
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				cmdErr = fmt.Errorf("command panicked: %v", rec)
			}
		}()
		cmdErr = registry.Execute(input, chatAgent)
	}()

	registry.SetOutput(nil)
	_ = writeEnd.Close()
	<-done
	_ = readEnd.Close()

	return cmd, buf.String(), cmdErr
}

// streamPipeChunks drains r into buf while invoking onChunk for each
// UTF-8-safe chunk. It buffers trailing partial runes so onChunk never
// receives an incomplete multi-byte rune, then emits a single event per
// pipe read containing every complete rune from that read. Callers can
// batch events from multiple reads (the WebUI panel will append
// monotonically). The chunk size (4 KB) is large enough to amortize
// per-chunk overhead but small enough that WebSocket latency stays low.
// Public for tests; production code uses it via
// executeSafeSteerCommandStreaming.
func streamPipeChunks(r io.Reader, buf *strings.Builder, onChunk func(string)) {
	const chunkSize = 4096
	pending := make([]byte, 0, chunkSize)
	scratch := make([]byte, chunkSize)
	for {
		n, err := r.Read(scratch)
		if n > 0 {
			buf.Write(scratch[:n])
			pending = append(pending, scratch[:n]...)
			// Walk the pending buffer once and emit each complete
			// rune. We collect them into a per-read builder so a
			// 4 KB pipe-read becomes ONE onChunk call (not 4096
			// per-byte calls), which keeps the event bus / WS
			// pipeline from drowning under flood pressure during
			// normal-speed commands. Trailing partial runes stay in
			// `pending` for the next read.
			i := 0
			out := make([]byte, 0, len(pending))
			for i < len(pending) {
				if !utf8.FullRune(pending[i:]) {
					// Cap pending at UTFMax to prevent unbounded growth
					// from broken UTF-8 (e.g. continuation bytes with no
					// leading byte). Emit replacement character and reset.
					if len(pending)-i >= utf8.UTFMax {
						out = append(out, "\uFFFD"...)
						pending = pending[:0]
					}
					break
				}
				rn, size := utf8.DecodeRune(pending[i:])
				var rb [utf8.UTFMax]byte
				sz := utf8.EncodeRune(rb[:], rn)
				out = append(out, rb[:sz]...)
				i += size
			}
			if len(out) > 0 && onChunk != nil {
				onChunk(string(out))
			}
			if i > 0 {
				pending = pending[:copy(pending, pending[i:])]
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			// Pipe closed mid-read or other read error. We can't do
			// much — the command's writer is gone. Stop streaming
			// and let the caller assemble whatever we captured.
			break
		}
	}
}
