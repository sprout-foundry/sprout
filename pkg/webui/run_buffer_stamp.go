//go:build !js

package webui

import (
	"strings"
)

// installRunBufferStamping hooks the event bus so every reattach-relevant
// event for a chat is appended to that chat's run buffer and stamped with
// its "__seq" before fan-out.
//
// The agent publishes straight to the bus (a.publishEvent →
// eventBus.Publish), bypassing publishClientEventWithChat. Stamping in the
// publish hook, rather than in a separate bus subscriber, gives live frames
// and the replay buffer the same seq for the same event: a reconnecting
// client asks for "after my last seq" and deduplicates by it.
func (ws *ReactWebServer) installRunBufferStamping() {
	if ws.eventBus == nil {
		return
	}
	ws.eventBus.SetPublishHook(ws.stampChatEvent)
}

// stampChatEvent is the bus publish hook. Payloads it does not buffer are
// returned untouched; buffered ones are shallow-copied before stamping so a
// publisher that kept a reference to its map never sees it change.
func (ws *ReactWebServer) stampChatEvent(eventType string, data any) any {
	payload, ok := data.(map[string]interface{})
	if !ok {
		return data
	}
	ws.noteAgentEvent(eventType, payload)
	if _, buffered := reattachBufferedEventTypes[eventType]; !buffered {
		return data
	}
	if _, stamped := payload["__seq"]; stamped {
		return data
	}
	clientID, _ := payload["client_id"].(string)
	chatID, _ := payload["chat_id"].(string)
	clientID = strings.TrimSpace(clientID)
	chatID = strings.TrimSpace(chatID)
	if clientID == "" || chatID == "" {
		return data
	}

	out := make(map[string]interface{}, len(payload)+1)
	for k, v := range payload {
		out[k] = v
	}
	if ws.appendChatEventToRunBufferWith(clientID, chatID, eventType, out, ws.tryLookupChatSession) == 0 {
		return data
	}
	return out
}
