//go:build !js

package webui

import (
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// withShortRunBufferTTL temporarily shortens the global TTL so tests don't
// wait the full 60s. Restores on cleanup.
func withShortRunBufferTTL(t *testing.T, d time.Duration) {
	t.Helper()
	original := defaultRunBufferTTLAfterCompletion
	defaultRunBufferTTLAfterCompletion = d
	t.Cleanup(func() {
		defaultRunBufferTTLAfterCompletion = original
	})
}

// setupTTLFixture builds a ReactWebServer with one chat session, returns
// the chatSession for direct buffer inspection.
func setupTTLFixture(t *testing.T) (ws *ReactWebServer, clientID, chatID string, cs *chatSession) {
	t.Helper()
	ws = &ReactWebServer{
		eventBus:       events.NewEventBus(),
		clientContexts: map[string]*webClientContext{},
	}
	clientID = "client-A"
	chatID = "chat-1"
	cs = newChatSession(chatID, "Chat 1")
	ws.clientContexts[clientID] = &webClientContext{
		ChatSessions:  map[string]*chatSession{chatID: cs},
		DefaultChatID: chatID,
	}
	return ws, clientID, chatID, cs
}

// TestRunBufferTTL_QueryCompletedSchedulesReset proves the buffer drops
// its contents some time after the last query_completed event.
func TestRunBufferTTL_QueryCompletedSchedulesReset(t *testing.T) {
	withShortRunBufferTTL(t, 50*time.Millisecond)
	ws, clientID, chatID, cs := setupTTLFixture(t)

	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeQueryStarted, map[string]interface{}{
		"query": "hello",
	})
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeStreamChunk, map[string]interface{}{
		"content": "hi",
	})
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeQueryCompleted, map[string]interface{}{
		"response": "done",
	})

	if got := cs.runBuffer.Len(); got != 3 {
		t.Fatalf("pre-TTL Len = %d, want 3", got)
	}

	// Wait past the TTL with margin for scheduler jitter.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && cs.runBuffer.Len() != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := cs.runBuffer.Len(); got != 0 {
		t.Errorf("post-TTL Len = %d, want 0 (Reset should have fired)", got)
	}
	// LastSeq survives the reset so a re-publish doesn't collide with
	// already-delivered seqs.
	if got := cs.runBuffer.LastSeq(); got != 3 {
		t.Errorf("LastSeq after Reset = %d, want 3", got)
	}
}

// TestRunBufferTTL_QueryStartedCancelsPendingReset proves that a new run
// keeps the buffer alive even if a TTL was scheduled for the prior run.
func TestRunBufferTTL_QueryStartedCancelsPendingReset(t *testing.T) {
	withShortRunBufferTTL(t, 100*time.Millisecond)
	ws, clientID, chatID, cs := setupTTLFixture(t)

	// Complete a run, scheduling a reset for ~100ms out.
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeQueryStarted, map[string]interface{}{})
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeStreamChunk, map[string]interface{}{"content": "a"})
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeQueryCompleted, map[string]interface{}{})

	// Start a new run before the TTL fires.
	time.Sleep(30 * time.Millisecond)
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeQueryStarted, map[string]interface{}{})
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeStreamChunk, map[string]interface{}{"content": "b"})

	// Wait past when the cancelled timer would have fired.
	time.Sleep(120 * time.Millisecond)

	// Buffer should still hold all 5 events: started/chunk/completed
	// from run 1 + started/chunk from run 2.
	if got := cs.runBuffer.Len(); got != 5 {
		t.Errorf("Len after new query_started cancels pending reset = %d, want 5", got)
	}
	if cs.runBufferResetTimer != nil {
		t.Error("runBufferResetTimer should be cleared after a new query_started")
	}
}

// TestRunBufferTTL_BackToBackCompletionsRescheduleReset proves that two
// completes in a row (e.g. two short queries) don't leave a stale timer
// from the first one — only the second's TTL applies.
func TestRunBufferTTL_BackToBackCompletionsRescheduleReset(t *testing.T) {
	withShortRunBufferTTL(t, 80*time.Millisecond)
	ws, clientID, chatID, cs := setupTTLFixture(t)

	// First run completes, schedules reset at ~80ms.
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeQueryStarted, map[string]interface{}{})
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeQueryCompleted, map[string]interface{}{})

	// Second run completes 40ms later — should reschedule, not let the
	// first timer fire mid-second-run.
	time.Sleep(40 * time.Millisecond)
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeQueryStarted, map[string]interface{}{})
	ws.publishClientEventWithChat(clientID, chatID, events.EventTypeQueryCompleted, map[string]interface{}{})

	// 50ms later (90ms total) — first timer would have fired by now; the
	// second timer should not have. Buffer still populated.
	time.Sleep(50 * time.Millisecond)
	if got := cs.runBuffer.Len(); got == 0 {
		t.Error("buffer was emptied by stale timer from first run — second query_completed should have rescheduled")
	}

	// Now wait past the second timer.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && cs.runBuffer.Len() != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := cs.runBuffer.Len(); got != 0 {
		t.Errorf("buffer not cleared after second TTL: Len=%d", got)
	}
}
