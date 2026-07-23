//go:build !js

package webui

import (
	"strings"
)

// startRunBufferSubscriber subscribes to the event bus and writes
// reattach-relevant events into the appropriate chat's run buffer.
//
// This is needed because the agent publishes events directly to the bus
// (via a.publishEvent → a.eventBus.Publish), bypassing
// publishClientEventWithChat which is the only path that calls
// appendChatEventToRunBuffer. Without this subscriber, events published
// by the agent (query_started, tool_start, tool_end, stream_chunk,
// agent_message, error, etc.) are never written to the run buffer, so a
// client reconnecting mid-query gets no replay of what it missed.
//
// Events that already carry __seq (published by
// publishClientEventWithChat) are skipped to avoid double-buffering.
//
// The subscriber does NOT stamp __seq onto the live event data — that
// would race with the WebSocket write goroutine which may be concurrently
// JSON-marshaling the same map. The buffer's internal seq is sufficient
// for replay; the live WS path doesn't need it for agent-published events.
func (ws *ReactWebServer) startRunBufferSubscriber() {
	if ws.eventBus == nil {
		return
	}

	ch := ws.eventBus.Subscribe("run-buffer-subscriber")

	go func() {
		for ev := range ch {
			// Skip events that don't need buffering.
			if _, ok := reattachBufferedEventTypes[ev.Type]; !ok {
				continue
			}

			data, ok := ev.Data.(map[string]interface{})
			if !ok {
				continue
			}

			// Skip events that were already buffered by
			// publishClientEventWithChat (they carry __seq).
			if _, alreadyBuffered := data["__seq"]; alreadyBuffered {
				continue
			}

			clientID, _ := data["client_id"].(string)
			chatID, _ := data["chat_id"].(string)
			clientID = strings.TrimSpace(clientID)
			chatID = strings.TrimSpace(chatID)
			if clientID == "" || chatID == "" {
				continue
			}

			// Append to the run buffer. appendChatEventToRunBuffer also
			// handles TTL timer management for query_started/query_completed.
			// Do NOT mutate the data map — the WS write goroutine may be
			// concurrently reading it for JSON serialization.
			ws.appendChatEventToRunBuffer(clientID, chatID, ev.Type, data)
		}
		ws.log().Warn("run buffer subscriber channel closed")
	}()
}
