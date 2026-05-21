//go:build !js

package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestShouldForwardEventToConnection_MultiTabSameChat is the SP-034-3c
// regression test: a chat-scoped event whose originating clientID
// doesn't match this connection's clientID should still be forwarded
// when both connections are on the same chat. Without this, the OTHER
// browser tab viewing the chat would miss every event.
func TestShouldForwardEventToConnection_MultiTabSameChat(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
	}

	// Connection has clientID=B, on chat=X.
	connInfo := &ConnectionInfo{
		ClientID: "client-B",
		ChatID:   "chat-X",
	}
	// Event originated from clientID=A, targeting chat=X.
	ev := events.UIEvent{
		Type: events.EventTypeStreamChunk,
		Data: map[string]interface{}{
			"client_id": "client-A",
			"chat_id":   "chat-X",
			"content":   "hi from tab A",
		},
	}

	if !ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("event for same chat should fan out to other tab despite clientID mismatch")
	}
}

// TestShouldForwardEventToConnection_MultiTabDifferentChat verifies the
// fan-out does NOT leak into unrelated chats. Same clientID mismatch as
// above but the event's chatID differs from the connection's.
func TestShouldForwardEventToConnection_MultiTabDifferentChat(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
	}
	connInfo := &ConnectionInfo{
		ClientID: "client-B",
		ChatID:   "chat-Y",
	}
	ev := events.UIEvent{
		Type: events.EventTypeStreamChunk,
		Data: map[string]interface{}{
			"client_id": "client-A",
			"chat_id":   "chat-X",
		},
	}

	if ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("event for chat-X should NOT reach a tab viewing chat-Y")
	}
}

// TestShouldForwardEventToConnection_SecurityEventDoesNotFanOut verifies
// security-scoped events stay glued to their originating clientID even
// when the chat matches. Showing an approval prompt on another tab would
// let an attacker piggy-back on a confirmed action.
func TestShouldForwardEventToConnection_SecurityEventDoesNotFanOut(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
	}
	connInfo := &ConnectionInfo{
		ClientID: "client-B",
		ChatID:   "chat-X",
	}
	ev := events.UIEvent{
		Type: events.EventTypeSecurityApprovalRequest,
		Data: map[string]interface{}{
			"client_id":  "client-A",
			"chat_id":    "chat-X",
			"request_id": "req-1",
		},
	}

	if ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("security_approval_request must NOT fan out to other tabs even on same chat")
	}
}

// TestShouldForwardEventToConnection_ExplicitRegistrySubscription covers
// the case where a connection switched chats: its connInfo.ChatID still
// reflects the original chat, but it has explicitly subscribed to the
// new chat via the chatSubscribers registry.
func TestShouldForwardEventToConnection_ExplicitRegistrySubscription(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
	}
	conn := fakeConn()
	connInfo := &ConnectionInfo{
		ClientID: "client-B",
		ChatID:   "chat-original",
		Conn:     conn,
	}
	// Connection has subscribed to chat-X explicitly even though its
	// primary connInfo.ChatID is something else.
	ws.chatSubscribers.Subscribe("chat-X", conn)

	ev := events.UIEvent{
		Type: events.EventTypeStreamChunk,
		Data: map[string]interface{}{
			"client_id": "client-A",
			"chat_id":   "chat-X",
		},
	}

	if !ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("event should fan out via explicit chatSubscribers registration")
	}
}

// TestShouldForwardEventToConnection_NoChatScopeStillRequiresClientMatch
// verifies the existing tight semantic: an event with a client_id but no
// chat_id can't fan out. Without a chat scope there's no multi-tab
// hypothesis, so the clientID mismatch is decisive.
func TestShouldForwardEventToConnection_NoChatScopeStillRequiresClientMatch(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
	}
	connInfo := &ConnectionInfo{
		ClientID: "client-B",
		ChatID:   "chat-X",
	}
	ev := events.UIEvent{
		Type: events.EventTypeStreamChunk,
		Data: map[string]interface{}{
			"client_id": "client-A",
			// no chat_id
		},
	}
	if ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("clientID-only event must not fan out; only chat-scoped events do")
	}
}
