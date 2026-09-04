//go:build !js

package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// checkAndResume must reach every live agent — the shared CLI agent AND
// per-chat agents in daemon mode. The original implementation polled only
// ws.agent (nil in daemon mode), so background completions queued on a
// chat agent sat dormant until the user typed again.
func TestCheckAndResume_ReachesChatAgents(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked("poller-client")
	ctx.ensureDefaultChatSession()
	chatID := ctx.DefaultChatID
	ws.mutex.Unlock()

	// Use a sentinel agent we can observe in the snapshot.
	a := &agent.Agent{}

	ws.mutex.Lock()
	if cs, ok := ctx.ChatSessions[chatID]; ok {
		cs.mu.Lock()
		cs.Agent = a
		cs.mu.Unlock()
	}
	ws.mutex.Unlock()

	agents := ws.liveAgents()
	found := false
	for _, cand := range agents {
		if cand == a {
			found = true
		}
	}
	if !found {
		t.Fatalf("liveAgents did not include the chat agent (got %d agents)", len(agents))
	}

	// checkAndResume itself must not panic across the full agent set.
	ws.checkAndResume()
}

// liveAgents deduplicates: ctx.Agent aliases the active chat's agent, so
// the same agent must not be returned twice.
func TestLiveAgents_Deduplicates(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked("dedup-client")
	ctx.ensureDefaultChatSession()
	chatID := ctx.DefaultChatID
	ws.mutex.Unlock()

	a := &agent.Agent{}

	ws.mutex.Lock()
	ctx.Agent = a
	if cs, ok := ctx.ChatSessions[chatID]; ok {
		cs.mu.Lock()
		cs.Agent = a
		cs.mu.Unlock()
	}
	ws.mutex.Unlock()

	agents := ws.liveAgents()
	count := 0
	for _, cand := range agents {
		if cand == a {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("aliased agent returned %d times, want 1", count)
	}
}
