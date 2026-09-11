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

// TestShouldForwardEventToConnection_PersistentWSChatSwitch verifies the
// fix for the multi-chat-switch bug: when a user switches chats in the UI
// over a persistent WebSocket, connInfo.ChatID still reflects the original
// chat but the connection has subscribed to the new chat via chatSubscribers.
// Events for the new chat with a matching clientID should be forwarded.
func TestShouldForwardEventToConnection_PersistentWSChatSwitch(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
	}

	conn := fakeConn()
	connInfo := &ConnectionInfo{
		ClientID: "client-1",
		ChatID:   "chat-A", // Original chat from handshake
		Conn:     conn,
	}

	// User switches to chat-B in the UI — subscribes via chatSubscribers.
	ws.chatSubscribers.Subscribe("chat-B", conn)

	// Event for chat-B with matching clientID.
	ev := events.UIEvent{
		Type: events.EventTypeStreamChunk,
		Data: map[string]interface{}{
			"client_id": "client-1",
			"chat_id":   "chat-B",
			"content":   "streaming output",
		},
	}

	if !ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("event for subscribed chat-B should be forwarded even though connInfo.ChatID is chat-A")
	}
}

// TestShouldForwardEventToConnection_PersistentWSChatSwitch_NoSubscribe
// verifies cross-chat isolation is preserved: without an explicit Subscribe
// call, events for a different chat are still dropped even when clientID
// matches.
func TestShouldForwardEventToConnection_PersistentWSChatSwitch_NoSubscribe(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
	}

	conn := fakeConn()
	connInfo := &ConnectionInfo{
		ClientID: "client-1",
		ChatID:   "chat-A",
		Conn:     conn,
	}
	// NOTE: No Subscribe("chat-B", conn) call here.

	ev := events.UIEvent{
		Type: events.EventTypeStreamChunk,
		Data: map[string]interface{}{
			"client_id": "client-1",
			"chat_id":   "chat-B",
			"content":   "streaming output",
		},
	}

	if ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("event for chat-B should be dropped when connection is NOT subscribed to chat-B")
	}
}

// TestShouldForwardEventToConnection_SecurityEventStrictWithSubscribe
// verifies that security-scoped events (ask_user_request, security_approval_request,
// security_prompt_request) do NOT benefit from chatSubscribers subscription.
// Even when the connection has subscribed to the target chat, security events
// still require the connection's primary chatID to match (or be unfiltered).
func TestShouldForwardEventToConnection_SecurityEventStrictWithSubscribe(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
	}

	conn := fakeConn()
	connInfo := &ConnectionInfo{
		ClientID: "client-1",
		ChatID:   "chat-A",
		Conn:     conn,
	}

	// Connection subscribed to chat-B.
	ws.chatSubscribers.Subscribe("chat-B", conn)

	// Security event for chat-B with matching clientID.
	// This should NOT be forwarded because security events are strict.
	ev := events.UIEvent{
		Type: events.EventTypeAskUserRequest,
		Data: map[string]interface{}{
			"client_id":  "client-1",
			"chat_id":    "chat-B",
			"request_id": "ask-1",
			"question":   "approve this?",
		},
	}

	if ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("ask_user_request must NOT be forwarded via chatSubscribers even when subscribed")
	}

	// Same for security_approval_request.
	ev2 := events.UIEvent{
		Type: events.EventTypeSecurityApprovalRequest,
		Data: map[string]interface{}{
			"client_id":  "client-1",
			"chat_id":    "chat-B",
			"request_id": "sec-1",
			"tool_name":  "shell_command",
		},
	}
	if ws.shouldForwardEventToConnection(ev2, connInfo) {
		t.Error("security_approval_request must NOT be forwarded via chatSubscribers even when subscribed")
	}

	// Same for security_prompt_request.
	ev3 := events.UIEvent{
		Type: events.EventTypeSecurityPromptRequest,
		Data: map[string]interface{}{
			"client_id":  "client-1",
			"chat_id":    "chat-B",
			"request_id": "prompt-1",
		},
	}
	if ws.shouldForwardEventToConnection(ev3, connInfo) {
		t.Error("security_prompt_request must NOT be forwarded via chatSubscribers even when subscribed")
	}

	// Same for edit_approval_request — it is also security-scoped.
	ev4 := events.UIEvent{
		Type: events.EventTypeEditApprovalRequest,
		Data: map[string]interface{}{
			"client_id":  "client-1",
			"chat_id":    "chat-B",
			"request_id": "edit-1",
		},
	}
	if ws.shouldForwardEventToConnection(ev4, connInfo) {
		t.Error("edit_approval_request must NOT be forwarded via chatSubscribers even when subscribed")
	}
}

// TestShouldForwardEventToConnection_NoClientID_UsesChatSubscribers
// verifies that events with chat_id but NO client_id also honor
// chatSubscribers when the connection's primary chatID differs.
func TestShouldForwardEventToConnection_NoClientID_UsesChatSubscribers(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
	}

	conn := fakeConn()
	connInfo := &ConnectionInfo{
		ClientID: "client-1",
		ChatID:   "chat-A",
		Conn:     conn,
	}

	// Connection subscribed to chat-B.
	ws.chatSubscribers.Subscribe("chat-B", conn)

	// Event with chat_id but no client_id.
	ev := events.UIEvent{
		Type: events.EventTypeQueryProgress,
		Data: map[string]interface{}{
			"chat_id": "chat-B",
			"message": "query progress update",
		},
	}

	if !ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("chat-only event should be forwarded when connection is subscribed to the chat")
	}
}

// TestShouldForwardEventToConnection_NoClientID_NoSubscribe
// verifies that events with chat_id but no client_id are still dropped
// when the connection is NOT subscribed to the target chat.
func TestShouldForwardEventToConnection_NoClientID_NoSubscribe(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
	}

	conn := fakeConn()
	connInfo := &ConnectionInfo{
		ClientID: "client-1",
		ChatID:   "chat-A",
		Conn:     conn,
	}
	// NOTE: No Subscribe("chat-B", conn).

	ev := events.UIEvent{
		Type: events.EventTypeQueryProgress,
		Data: map[string]interface{}{
			"chat_id": "chat-B",
			"message": "query progress update",
		},
	}

	if ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("chat-only event should be dropped when connection is NOT subscribed to chat-B")
	}
}

// ── Stale connInfo.ChatID fallback (live active chat) ────────────────
//
// Regression: ask_user_request (and the other security-scoped events) were
// silently dropped when the connection's connect-time ChatID snapshot
// (chat_id / reattach query param) disagreed with the event's chat_id —
// e.g. the socket reattached to chat-X mid-query, the user then switched
// to chat-Y, and the next query's dialog was discarded. connInfo.ChatID
// is never refreshed on chat switch, so the dialog never appeared and the
// agent blocked until timeout. The filter now falls back to the client
// context's live active chat before dropping.

// Stale snapshot, but the client's live active chat IS the event's chat →
// forward. This is the ask_user-dropped-after-chat-switch scenario.
func TestShouldForwardEventToConnection_SecurityEventStaleChatIDLiveChatMatch(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
		clientContexts: map[string]*webClientContext{
			"client-A": {DefaultChatID: "chat-Y"},
		},
	}
	connInfo := &ConnectionInfo{
		ClientID: "client-A",
		ChatID:   "chat-X", // stale connect-time snapshot (reattach to chat-X)
	}
	ev := events.UIEvent{
		Type: events.EventTypeAskUserRequest,
		Data: map[string]interface{}{
			"client_id":  "client-A",
			"chat_id":    "chat-Y", // current active chat
			"request_id": "ask_1",
			"question":   "Proceed?",
		},
	}

	if !ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("ask_user_request for the live active chat must not be dropped by a stale connect-time ChatID")
	}
}

// Stale snapshot AND the live active chat differs → still dropped (the
// dialog belongs to another chat's pane).
func TestShouldForwardEventToConnection_SecurityEventStaleChatIDLiveChatMiss(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
		clientContexts: map[string]*webClientContext{
			"client-A": {DefaultChatID: "chat-Z"},
		},
	}
	connInfo := &ConnectionInfo{
		ClientID: "client-A",
		ChatID:   "chat-X",
	}
	ev := events.UIEvent{
		Type: events.EventTypeAskUserRequest,
		Data: map[string]interface{}{
			"client_id":  "client-A",
			"chat_id":    "chat-Y",
			"request_id": "ask_2",
			"question":   "Proceed?",
		},
	}

	if ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("ask_user_request for a chat that is neither the snapshot nor the live active chat must stay dropped")
	}
}

// The live-chat fallback must NOT reopen cross-client fan-out: a
// security-scoped event from client-A is still dropped on client-B's
// connection even if both point at the same live chat.
func TestShouldForwardEventToConnection_SecurityEventLiveChatFallbackNoCrossClient(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
		clientContexts: map[string]*webClientContext{
			"client-B": {DefaultChatID: "chat-Y"},
		},
	}
	connInfo := &ConnectionInfo{
		ClientID: "client-B",
		ChatID:   "chat-X",
	}
	ev := events.UIEvent{
		Type: events.EventTypeSecurityApprovalRequest,
		Data: map[string]interface{}{
			"client_id":  "client-A",
			"chat_id":    "chat-Y",
			"request_id": "sec_1",
			"tool_name":  "shell_command",
			"risk_level": "CAUTION",
		},
	}

	if ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("security-scoped event from another client must stay dropped even when the live active chat matches")
	}
}

// No client context for the connection's client → fallback finds nothing →
// behavior matches the old strict rule (drop).
func TestShouldForwardEventToConnection_SecurityEventStaleChatIDNoContext(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
		clientContexts:  map[string]*webClientContext{},
	}
	connInfo := &ConnectionInfo{
		ClientID: "client-A",
		ChatID:   "chat-X",
	}
	ev := events.UIEvent{
		Type: events.EventTypeAskUserRequest,
		Data: map[string]interface{}{
			"client_id":  "client-A",
			"chat_id":    "chat-Y",
			"request_id": "ask_3",
			"question":   "Proceed?",
		},
	}

	if ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("stale-snapshot mismatch with no client context must keep dropping the event")
	}
}

// chat_id-only security event (no client_id) takes the same fallback:
// stale snapshot + live chat match → forward.
func TestShouldForwardEventToConnection_SecurityEventNoClientIDStaleChatIDLiveChatMatch(t *testing.T) {
	ws := &ReactWebServer{
		chatSubscribers: newChatSubscribersRegistry(),
		clientContexts: map[string]*webClientContext{
			"client-A": {DefaultChatID: "chat-Y"},
		},
	}
	connInfo := &ConnectionInfo{
		ClientID: "client-A",
		ChatID:   "chat-X",
	}
	ev := events.UIEvent{
		Type: events.EventTypeAskUserRequest,
		Data: map[string]interface{}{
			"chat_id":    "chat-Y",
			"request_id": "ask_4",
			"question":   "Proceed?",
		},
	}

	if !ws.shouldForwardEventToConnection(ev, connInfo) {
		t.Error("client-less ask_user_request for the live active chat must not be dropped by a stale ChatID")
	}
}
