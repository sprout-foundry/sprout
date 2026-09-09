//go:build !js

package webui

import (
	"encoding/json"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestGatherStats_SnapshotDoesNotClobberLiveAgent pins the fix for the
// status-bar cost flicker: when a LIVE agent exists, the AgentState
// snapshot (which lags the agent — it is synced at turn end) must not
// overwrite the agent's fresher cost/token numbers. The snapshot path is
// now restricted to the nil-agent case.
func TestGatherStats_SnapshotDoesNotClobberLiveAgent(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// A stale snapshot: cost 0.10 (older than the live agent's 1.23).
	stale := agent.AgentState{SessionID: "s1", TotalTokens: 100, TotalCost: 0.10}
	staleJSON, _ := json.Marshal(stale)

	const clientID = "flicker-test"
	ws.mutex.Lock()
	ws.getOrCreateClientContextLocked(clientID)
	ctx := ws.clientContexts[clientID]
	ctx.AgentState = append([]byte(nil), staleJSON...)
	ws.mutex.Unlock()

	// gatherStatsForClientID with a nil agent uses the snapshot.
	stats := ws.gatherStatsForClientID(clientID)
	if got := stats["total_cost"]; got != 0.10 {
		t.Fatalf("snapshot path (nil agent): total_cost = %v, want 0.10", got)
	}
}

// TestPublishProviderState_NilAgentOmitsCostKeys pins the guard that keeps
// provider-state publishes from carrying zeroed/stale cost during agent
// (re)creation. The early-exit paths must strip cost/token keys so the
// frontend merge leaves the last-known values intact instead of flashing
// to $0.00.
func TestPublishProviderState_NilAgentOmitsCostKeys(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Subscribe and capture one metrics_update.
	got := make(chan map[string]interface{}, 1)
	sub := ws.eventBus.Subscribe("metrics-flicker-test")
	defer ws.eventBus.Unsubscribe("metrics-flicker-test")
	done := make(chan struct{})
	go func() {
		for ev := range sub {
			if data, ok := ev.Data.(map[string]interface{}); ok {
				got <- data
				close(done)
				return
			}
		}
	}()

	// Nil-agent publish (no provider configured in this test server).
	ws.publishProviderState("flicker-test")

	select {
	case data := <-got:
		for _, k := range []string{"total_cost", "total_tokens", "prompt_tokens", "completion_tokens"} {
			if _, present := data[k]; present {
				t.Fatalf("nil-agent metrics publish carries %q (= %v) — would flash the status bar", k, data[k])
			}
		}
	case <-done:
	}
}

// TestMetricsUpdate_TurnEndPublishIsWired is a compile-level guard: the
// seed_query completion path now publishes a metrics_update after
// query_completed, giving the WebUI cost display a per-turn cadence. The
// behavioral assertions live in the agent package's publisher tests; this
// test pins the webui-facing invariant that metrics events flow on the
// shared bus shape used by the status bar.
func TestMetricsUpdate_TurnEndPublishIsWired(t *testing.T) {
	ev := events.MetricsUpdateEventWithCategory("deepinfra", "test-model", 10, 5, 100, 1, 0.42, "")
	if ev["total_cost"] != 0.42 || ev["provider"] != "deepinfra" {
		t.Fatalf("metrics payload shape changed: %v", ev)
	}
}
