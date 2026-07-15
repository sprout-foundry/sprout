//go:build !js

package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// makeAgentPtr builds a sentinel *agent.Agent for cache verification.
// We never call methods on this pointer — tests assert identity only
// (was it cleared or not?). Returning &agent.Agent{} is the cheapest
// non-nil pointer that satisfies the type without going through
// NewReactWebServer's provider setup.
func makeAgentPtr() *agent.Agent { return &agent.Agent{} }

// ---------------------------------------------------------------------------
// Tests: cleanupAfterPanicSession (Mode 2 / daemon)
// ---------------------------------------------------------------------------

// TestCleanupAfterPanicSession_ScopesToOneChat verifies that
// cleanupAfterPanicSession only clears the chat that panicked, leaving
// sibling chats on the same clientID untouched. Mode 1's
// cleanupAfterPanicAgent nukes ALL chats; Mode 2's bounded version is
// what lets two windows coexist when one of them panics.
func TestCleanupAfterPanicSession_ScopesToOneChat(t *testing.T) {
	ws, err := NewReactWebServer(nil, newTestEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	clientID := "client-A"

	// Build a context with TWO chat sessions, one active and one idle.
	// After cleanup targeting chat-1, only chat-1 should be cleared.
	ws.mutex.Lock()
	ctx := &webClientContext{
		ChatSessions: map[string]*chatSession{
			"chat-1": {},
			"chat-2": {},
		},
		DefaultChatID: "chat-1",
		ActiveQuery:   true,
		CurrentQuery:  "active",
	}
	ctx.ChatSessions["chat-1"].setQueryActive(true, "in-progress")
	ctx.ChatSessions["chat-2"].setQueryActive(false, "")
	ws.clientContexts[clientID] = ctx
	ws.mutex.Unlock()

	// Register the user connection so Count(userID) <= 1 holds —
	// otherwise the cached-agent clear is skipped (which is correct,
	// tested elsewhere).
	ws.userConnections.Add("client-A", UserConnection{Raw: "s1", SessionID: "s1"})

	ws.cleanupAfterPanicSession(clientID, "client-A", "chat-1", "session-1")

	ws.mutex.RLock()
	cs1 := ws.clientContexts[clientID].ChatSessions["chat-1"]
	cs2 := ws.clientContexts[clientID].ChatSessions["chat-2"]
	ws.mutex.RUnlock()

	if cs1.ActiveQuery {
		t.Errorf("chat-1 should be cleared after cleanup, but is still active")
	}
	if cs2.ActiveQuery {
		t.Errorf("chat-2 was idle, must remain idle after sibling cleanup")
	}
}

// TestCleanupAfterPanicSession_ClearsCachedAgentWhenLastWindow verifies
// the cached-agent clear fires when this is the only window for the
// user — preserving the Mode 1 corruption defense in the single-window
// case.
func TestCleanupAfterPanicSession_ClearsCachedAgentWhenLastWindow(t *testing.T) {
	ws, err := NewReactWebServer(nil, newTestEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	clientID := "client-A"

	sentinel := makeAgentPtr()
	ws.mutex.Lock()
	ws.clientContexts[clientID] = &webClientContext{Agent: sentinel}
	ws.mutex.Unlock()

	// Register exactly one connection.
	ws.userConnections.Add("client-A", UserConnection{Raw: "s1", SessionID: "s1"})

	ws.cleanupAfterPanicSession(clientID, "client-A", "chat-1", "session-1")

	ws.mutex.RLock()
	got := ws.clientContexts[clientID].Agent
	ws.mutex.RUnlock()
	if got != nil {
		t.Errorf("expected cached agent cleared when Count <= 1, got %p", got)
	}
}

// TestCleanupAfterPanicSession_PreservesAgentsWithMultipleWindows verifies
// the cached-agent clear is SKIPPED when sibling windows are still
// open. The siblings' agents must keep working.
func TestCleanupAfterPanicSession_PreservesAgentsWithMultipleWindows(t *testing.T) {
	ws, err := NewReactWebServer(nil, newTestEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	clientID := "client-A"

	sentinel := makeAgentPtr()
	ws.mutex.Lock()
	ws.clientContexts[clientID] = &webClientContext{Agent: sentinel}
	ws.mutex.Unlock()

	// Two connections — this panic is on one of them.
	ws.userConnections.Add("client-A", UserConnection{Raw: "s1", SessionID: "s1"})
	ws.userConnections.Add("client-A", UserConnection{Raw: "s2", SessionID: "s2"})

	ws.cleanupAfterPanicSession(clientID, "client-A", "chat-1", "session-1")

	ws.mutex.RLock()
	got := ws.clientContexts[clientID].Agent
	ws.mutex.RUnlock()
	if got != sentinel {
		t.Errorf("expected cached agent PRESERVED with multiple windows, got %p (sentinel=%p)",
			got, sentinel)
	}
}

// TestCleanupAfterPanicSession_EmptyClientID_DoesNotPanic mirrors the
// Mode 1 guard: empty clientID is a no-op, not a panic.
func TestCleanupAfterPanicSession_EmptyClientID_DoesNotPanic(t *testing.T) {
	ws, err := NewReactWebServer(nil, newTestEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.cleanupAfterPanicSession("", "user-A", "chat-1", "session-1")
	ws.cleanupAfterPanicSession("  ", "", "", "")
}

// TestCleanupAfterPanicSession_UnknownClientID_DoesNotPanic verifies the
// cleanup function tolerates a clientID with no context — common when
// the read goroutine panics before ConnectionInfo is stored.
func TestCleanupAfterPanicSession_UnknownClientID_DoesNotPanic(t *testing.T) {
	ws, err := NewReactWebServer(nil, newTestEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.cleanupAfterPanicSession("nonexistent", "user", "chat-1", "session-1")
}

// TestCleanupAfterPanicAgent_StillClearsAllChats verifies the Mode 1
// regression guard: cleanupAfterPanicAgent must STILL clear all chats
// for the clientID. Phase 2 must not have softened this — the contract
// is "Mode 1 = blast everything because there's only one window."
func TestCleanupAfterPanicAgent_StillClearsAllChats(t *testing.T) {
	ws, err := NewReactWebServer(nil, newTestEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	clientID := "client-A"

	ws.mutex.Lock()
	ctx := &webClientContext{
		ChatSessions: map[string]*chatSession{
			"chat-1": {},
			"chat-2": {},
		},
		DefaultChatID: "chat-1",
	}
	ctx.ChatSessions["chat-1"].setQueryActive(true, "a")
	ctx.ChatSessions["chat-2"].setQueryActive(true, "b")
	ws.clientContexts[clientID] = ctx
	ws.mutex.Unlock()

	ws.cleanupAfterPanicAgent(clientID, "session-1")

	ws.mutex.RLock()
	cs1 := ws.clientContexts[clientID].ChatSessions["chat-1"]
	cs2 := ws.clientContexts[clientID].ChatSessions["chat-2"]
	ws.mutex.RUnlock()

	if cs1.ActiveQuery {
		t.Errorf("Mode 1 cleanup must clear chat-1, but it's still active")
	}
	if cs2.ActiveQuery {
		t.Errorf("Mode 1 cleanup must clear chat-2 (all chats), but it's still active")
	}
}