//go:build !js

package webui

import (
	"log"
	"strconv"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// Reattach control message sent at the start of a replay so the client
// knows it's catching up rather than receiving fresh events. Mirrors
// terminal_websocket.go's `session_restored` shape.
const wsMessageTypeChatRunRestored = "chat_run_restored"

// parseAfterSeqQuery interprets the `after_seq` query param. Bad input is
// treated as 0 — the caller will then receive every retained event, which
// is the safe default for clients that didn't track seq before SP-034-2.
func parseAfterSeqQuery(raw string) int64 {
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// buildChatRunReplayMessages assembles the wire messages to send for a
// reattach: a leading `chat_run_restored` control frame, followed by the
// buffered events with seq > afterSeq in order.
//
// Always emits the restored frame (even when no events to replay) so the
// client can clear "reconnecting…" UI without guessing whether the server
// understood the reattach request. The `gap` flag tells the client its
// local state predates the oldest retained event and it should hard-
// refresh instead of splicing partial chunks.
//
// Pulling this out as a pure function keeps the WS handler's plumbing
// (which needs a real *SafeConn) separate from the replay decision, so
// tests can verify message shape without a live socket.
func (ws *ReactWebServer) buildChatRunReplayMessages(clientID, chatID string, afterSeq int64) []map[string]interface{} {
	ws.mutex.RLock()
	cctx := ws.clientContexts[clientID]
	ws.mutex.RUnlock()

	var (
		replay []events.UIEvent
		gap    bool
		last   int64
	)

	if cctx != nil {
		cs := cctx.getChatSession(chatID)
		if cs != nil {
			cs.mu.Lock()
			buf := cs.runBuffer
			cs.mu.Unlock()
			if buf != nil {
				replay, gap = buf.After(afterSeq)
				last = buf.LastSeq()
			}
		}
	}

	out := make([]map[string]interface{}, 0, len(replay)+1)
	out = append(out, map[string]interface{}{
		"type": wsMessageTypeChatRunRestored,
		"data": map[string]interface{}{
			"chat_id":             chatID,
			"after_seq":           afterSeq,
			"last_seq":            last,
			"missed_chunks_count": len(replay),
			"gap":                 gap,
		},
	})
	for _, ev := range replay {
		out = append(out, map[string]interface{}{
			"type": ev.Type,
			"data": ev.Data,
		})
	}
	return out
}

// deliverChatRunReplay sends the buffered chat events with seq > afterSeq
// to the freshly-connected WebSocket. Thin wrapper over
// buildChatRunReplayMessages so the WS handler doesn't have to know the
// replay shape — and tests don't have to spin up a socket.
func (ws *ReactWebServer) deliverChatRunReplay(safeConn *SafeConn, clientID, chatID string, afterSeq int64) {
	for _, msg := range ws.buildChatRunReplayMessages(clientID, chatID, afterSeq) {
		if err := safeConn.WriteJSON(msg); err != nil {
			log.Printf("WebSocket failed to send chat-run replay frame for chat %s: %v", chatID, err)
			return
		}
	}
}
