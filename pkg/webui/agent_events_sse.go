//go:build !js

package webui

// agent_events_sse.go serves the daemon's agent-event stream over plain
// HTTP as Server-Sent Events. The WebSocket bridge at /ws forwards the
// same events, but a WebSocket upgrade is bidirectional and long-lived,
// so it cannot ride the runner relay's request/response stream model
// (pkg/runner/relay/mux.go). An SSE response is ordinary HTTP, so the
// relay's existing transport carries it unchanged — the runner's host
// server already reverse-proxies /daemon/{workspaceID}/... with
// FlushInterval: -1, streaming bytes as they arrive.
//
// Security boundary: the auth middleware lets GET through unauthenticated
// (only write methods require the bearer), but the agent-event stream
// carries prompts and model output, so it must not be readable by a bare
// GET. The handler therefore enforces the same bearer itself whenever
// SPROUT_AUTH_TOKEN is configured, using the same constant-time compare
// the middleware uses. The relay path is additionally gated by the
// runner's per-workspace secret before the daemon is reached.

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// sseHeartbeatInterval bounds how long an idle stream looks dead: with no
// agent events flowing (a slow model call, a stalled turn), a comment
// frame keeps proxies and clients from timing the connection out.
const sseHeartbeatInterval = 15 * time.Second

// handleAPIAgentEvents serves GET /api/agent/events as an SSE stream of the
// daemon's agent-turn events. Query parameters mirror the /ws handshake:
//
//	chat_id    scope the stream to one chat (empty = the client's active chat)
//	client_id  the WebUI client/window identifier (also honored as a header)
//	after_seq  replay buffered events with a sequence past this value
//
// A subscriber only sees events the WebSocket path would forward to it:
// the same shouldForwardEventToConnection policy decides each event, so a
// stream scoped to chat A never receives chat B's events. The stream is
// per-connection (it has no multi-tab chat-subscription registry), so a
// consumer must send the client_id that owns the events it wants.
func (ws *ReactWebServer) handleAPIAgentEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	if !ws.agentEventsAuthorized(r) {
		ws.log().Warn("unauthorized agent-event stream request", "remote_addr", r.RemoteAddr)
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":   "unauthorized",
			"message": "valid auth token required",
		})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming_unsupported"})
		return
	}

	clientID := ws.resolveClientID(r)
	chatID := strings.TrimSpace(r.URL.Query().Get("chat_id"))
	if chatID == "" {
		chatID = strings.TrimSpace(r.URL.Query().Get("reattach"))
	}
	if chatID == "" {
		// An omitted chat scopes to the client's active chat, matching the
		// documented "empty = active chat" semantics (and the /ws handshake,
		// which resolves the active chat when no chat_id is supplied).
		ws.mutex.RLock()
		if ctx := ws.clientContexts[clientID]; ctx != nil {
			chatID = ctx.getActiveChatID()
		}
		ws.mutex.RUnlock()
	}
	afterSeq := parseAfterSeqQuery(r.URL.Query().Get("after_seq"))

	// The event bus is keyed by a subscriber name; the session id scopes
	// the subscription the same way the /ws live loop does. A per-connection
	// name keeps many concurrent relay streams independent.
	sessionID := fmt.Sprintf("sse_%s_%d", clientID, time.Now().UnixNano())
	eventCh := ws.eventBus.Subscribe(sessionID)
	defer ws.eventBus.Unsubscribe(sessionID)

	connInfo := &ConnectionInfo{
		SessionID:          sessionID,
		ClientID:           clientID,
		ChatID:             chatID,
		Type:               "sse",
		UserID:             ws.ExtractUserID(r),
		ConnectedAt:        time.Now(),
		subscribedChannels: make(map[string]bool),
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Replay buffered events before the live loop so a reconnecting
	// consumer (after_seq) catches up, then continues live. A leading
	// `restored` control frame reports whether the requested position
	// predates the oldest retained event (gap), so the consumer can reset
	// rather than splice an incomplete turn — mirroring the WebSocket
	// reattach's chat_run_restored frame.
	if chatID != "" {
		replay, gap := ws.agentEventReplay(clientID, chatID, afterSeq)
		lastSeq := ws.agentEventLastSeq(clientID, chatID)
		if err := writeSSEControl(w, map[string]interface{}{
			"type":      "restored",
			"chat_id":   chatID,
			"after_seq": afterSeq,
			"last_seq":  lastSeq,
			"gap":       gap,
		}); err != nil {
			return
		}
		for _, ev := range replay {
			if !ws.shouldForwardEventToConnection(ev, connInfo) {
				continue
			}
			if err := writeSSEEvent(w, ev); err != nil {
				return
			}
		}
		flusher.Flush()
	}

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-eventCh:
			if !ws.shouldForwardEventToConnection(ev, connInfo) {
				continue
			}
			if err := writeSSEEvent(w, ev); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// agentEventsAuthorized reports whether the request may read the agent-event
// stream. When no token is configured the endpoint is open (localhost/dev),
// matching the rest of the daemon's read surface. When a token is configured
// the request must present it as a bearer — even though it is a GET, which
// the auth middleware would otherwise wave through.
func (ws *ReactWebServer) agentEventsAuthorized(r *http.Request) bool {
	if ws.authToken == "" {
		return true
	}
	authHeader := r.Header.Get("Authorization")
	expectedBearer := "Bearer " + ws.authToken
	return subtle.ConstantTimeCompare([]byte(authHeader), []byte(expectedBearer)) == 1
}

// agentEventReplay returns the buffered agent events for a chat with a
// sequence past afterSeq, in order, for a reconnecting consumer. The second
// return reports a gap: the requested position predates the oldest retained
// event, so the consumer should reset rather than splice partial state.
func (ws *ReactWebServer) agentEventReplay(clientID, chatID string, afterSeq int64) ([]events.UIEvent, bool) {
	buf := ws.agentEventBuffer(clientID, chatID)
	if buf == nil {
		return nil, false
	}
	return buf.After(afterSeq)
}

// agentEventLastSeq returns the most recent buffered sequence for a chat, or
// 0 when the chat has no buffer. The SSE restored frame reports it so a
// consumer knows where the live stream resumes.
func (ws *ReactWebServer) agentEventLastSeq(clientID, chatID string) int64 {
	buf := ws.agentEventBuffer(clientID, chatID)
	if buf == nil {
		return 0
	}
	return buf.LastSeq()
}

// agentEventBuffer resolves a chat's run buffer, or nil when the client or
// chat is unknown (or no run has buffered anything yet).
func (ws *ReactWebServer) agentEventBuffer(clientID, chatID string) *chatRunRingBuffer {
	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	ws.mutex.RUnlock()
	if ctx == nil {
		return nil
	}
	cs := ctx.getChatSession(chatID)
	if cs == nil {
		return nil
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.runBuffer
}

// writeSSEEvent serializes one UIEvent as an SSE data frame. The whole
// event is emitted as a single JSON line so the consumer reconstructs the
// same {id,type,timestamp,data} shape the WebSocket path sends. A payload
// that cannot be encoded is skipped rather than tearing the stream down.
func writeSSEEvent(w http.ResponseWriter, ev events.UIEvent) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return nil
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", payload)
	return err
}

// writeSSEControl emits a named control frame (an `event:` line plus a
// JSON data line) used for the replay handshake rather than an agent event.
func writeSSEControl(w http.ResponseWriter, payload map[string]interface{}) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", payload["type"], b)
	return err
}
