//go:build !js

package webui

// Package webui: WebSocket panic recovery and cleanup handlers (split from websocket_handler.go)

import (
	"log/slog"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// cleanupAfterPanicSession is the Mode 2 / daemon-side panic cleanup.
// It bounds the blast radius by what could plausibly share state with
// the panicked session:
//
//  1. Drop this session from the userConnections registry (so future
//     reads/writes don't try to use a half-closed socket).
//  2. Clear THIS session's chat query state — only the chat that
//     panicked (chatID), not the client's other chats.
//  3. Clear cached agents for clientID only when this was the LAST
//     window for the user (Count(userID) <= 1). The MCP-manager /
//     conversation-history corruption defense from cleanupAfterPanicAgent
//     still applies when the user has only one window; with multiple
//     windows open, we trust the other windows' agents are independent.
//
// When userID is empty (local mode), trackingKey falls back to clientID
// and the count check uses that, preserving Mode-1 parity for the
// single-window case.
func (ws *ReactWebServer) cleanupAfterPanicSession(clientID, userID, chatID, sessionID string) {
	if strings.TrimSpace(clientID) == "" {
		return
	}

	trackingKey := userID
	if trackingKey == "" {
		trackingKey = clientID
	}

	// 1. Decrement top-level active query counter for this client.
	//    Even when multiple windows are open, the counter is per-clientID,
	//    so this remains the right level of accounting.
	ws.decrementActiveQueries(clientID)

	// 2. Reset per-chat session query state — but only for the chat
	//    tied to this session, not the whole clientID. Other windows on
	//    the same clientID with their own chats are unaffected.
	ws.mutex.Lock()
	if ctx := ws.clientContexts[clientID]; ctx != nil && chatID != "" {
		if cs, ok := ctx.ChatSessions[chatID]; ok && cs != nil {
			cs.setQueryActive(false, "")
		}
	}
	ws.mutex.Unlock()

	// 3. Cached-agent clear is gated on Count(trackingKey) <= 1. With
	//    only one window open, the original Mode-1 corruption defense
	//    still applies — the agent might be half-initialized. With
	//    multiple windows, we trust the other windows to retain working
	//    agents and skip the clear so siblings don't lose their in-flight
	//    query state.
	if ws.userConnections != nil && ws.userConnections.Count(trackingKey) <= 1 {
		ws.clearCachedAgent(clientID)
	}

	// 4. Publish session_terminated event so the panicked client can
	//    tear down UI. Other windows on the same clientID are NOT
	//    notified — they continue serving the user.
	ws.publishClientEvent(clientID, events.EventTypeSessionTerminated, map[string]interface{}{
		"session_id": sessionID,
		"status":     "error",
		"code":       "internal_panic",
		"message":    "Session terminated due to internal error",
	})
}

// safeHandleGoroutine runs fn in a goroutine with panic recovery. If fn
// panics, an error event is written to the WebSocket, the active query
// state is reset (mode-appropriate blast radius), and the connection
// is closed.
//
// The userID, chatID, and daemon bool are used to dispatch the right
// cleanup function on panic:
//   - daemon=false (Mode 1 / sprout agent): cleanupAfterPanicAgent
//     nukes the whole clientID's state — safe because there is only
//     one window per user.
//   - daemon=true (Mode 2 / daemon): cleanupAfterPanicSession scopes
//     the clear to this session + clientID-clear only when this is
//     the last window for the user.
func (ws *ReactWebServer) safeHandleGoroutine(safeConn *SafeConn, sessionID, clientID, userID, chatID string, daemon bool, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ws.log().Error("WebSocket handler panicked", slog.String("session_id", sessionID), slog.Any("panic", r))
				safeConn.WritePanicError(sessionID, "message handler", r)
				if daemon {
					ws.cleanupAfterPanicSession(clientID, userID, chatID, sessionID)
				} else {
					ws.cleanupAfterPanicAgent(clientID, sessionID)
				}
				safeConn.Close() // Terminate the session since state is unreliable after a panic
			}
		}()
		fn()
	}()
}

// cleanupAfterPanicAgent resets the client's query state in Mode 1
// (single-session). This is the Mode 1 / SP-118-Mode1 path: only one
// browser window is active per user, so clearing the full clientID's
// state is safe and correct.
//
// Design note: this clears ALL chat sessions for the clientID, not just the
// one that panicked. This is intentional — a panicked goroutine may have
// corrupted shared agent state (e.g. the MCP manager or conversation history),
// so it's safer to force full agent recreation rather than risk using a
// half-initialized agent in other chat sessions.
//
// The session_terminated event is published to the event bus for any other
// subscribers (monitoring, multi-tab clients). The panicked connection
// itself already receives the event directly via WritePanicError.
func (ws *ReactWebServer) cleanupAfterPanicAgent(clientID, sessionID string) {
	if strings.TrimSpace(clientID) == "" {
		return
	}

	// 1. Decrement top-level active query counter and clear top-level state
	ws.decrementActiveQueries(clientID)

	// 2. Reset per-chat session query state — prevents individual chats
	// from being stuck in "running" state after a panic.
	// Hold ws.mutex to safely access ctx and its fields (ActiveQuery,
	// CurrentQuery, ChatSessions) which are always guarded by ws.mutex.
	ws.mutex.Lock()
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		ctx.clearAllChatQueryState()
	}
	ws.mutex.Unlock()

	// 3. Clear cached agents — a panicked goroutine may have left the
	// agent in a half-initialized or corrupted state. Force recreation.
	ws.clearCachedAgent(clientID)

	// 4. Publish session_terminated event so the client can tear down UI
	ws.publishClientEvent(clientID, events.EventTypeSessionTerminated, map[string]interface{}{
		"session_id": sessionID,
		"status":     "error",
		"code":       "internal_panic",
		"message":    "Session terminated due to internal error",
	})
}
