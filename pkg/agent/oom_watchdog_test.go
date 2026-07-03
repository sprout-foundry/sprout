//go:build !js

package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestNewOOMWatchdog_Defaults(t *testing.T) {
	eb := events.NewEventBus()
	w := NewOOMWatchdog(eb)

	if w.interval != 30*time.Second {
		t.Errorf("interval = %v; want 30s", w.interval)
	}
	if w.nodeCountThreshold != 500 {
		t.Errorf("nodeCountThreshold = %d; want 500", w.nodeCountThreshold)
	}
	if w.rssThresholdBytes != 50*1024*1024*1024 {
		t.Errorf("rssThresholdBytes = %d; want 50GB", w.rssThresholdBytes)
	}
	if w.cooldownDuration != 5*time.Minute {
		t.Errorf("cooldownDuration = %v; want 5m", w.cooldownDuration)
	}
	if w.lastAlertState != "none" {
		t.Errorf("lastAlertState = %q; want %q", w.lastAlertState, "none")
	}
	if w.probeFn != nil {
		t.Error("probeFn should be nil by default")
	}
}

func TestOOMWatchdog_ProbeNoAlert_WhenBelowThresholds(t *testing.T) {
	eb := events.NewEventBus()
	w := NewOOMWatchdog(eb)

	// Subscribe before probing so we don't miss the event.
	collected := collectEvents(t, eb)

	// Mock probe returning low values.
	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return &OOMProbeResult{
			NodeCount:     5,
			TotalRSSBytes: 100 * 1024 * 1024, // 100 MB
		}, nil
	})

	// Run a few probe cycles.
	for i := 0; i < 3; i++ {
		w.doProbe()
	}

	// Verify no events were published.
	events := collected()
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
		for _, e := range events {
			t.Logf("unexpected event: %+v", e)
		}
	}
}

func TestOOMWatchdog_Alert_NodeCountExceeded(t *testing.T) {
	eb := events.NewEventBus()
	w := NewOOMWatchdog(eb)

	collected := collectEvents(t, eb)

	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return &OOMProbeResult{
			NodeCount:     600,
			TotalRSSBytes: 100 * 1024 * 1024, // 100 MB (well below threshold)
		}, nil
	})

	w.doProbe()

	events := collected()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	data := events[0].Data.(map[string]interface{})
	if data["trigger_reason"] != "node_count" {
		t.Errorf("trigger_reason = %q; want %q", data["trigger_reason"], "node_count")
	}
}

func TestOOMWatchdog_Alert_RSSExceeded(t *testing.T) {
	eb := events.NewEventBus()
	w := NewOOMWatchdog(eb)

	collected := collectEvents(t, eb)

	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return &OOMProbeResult{
			NodeCount:     5,
			TotalRSSBytes: 60 * 1024 * 1024 * 1024, // 60 GB
		}, nil
	})

	w.doProbe()

	events := collected()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	data := events[0].Data.(map[string]interface{})
	if data["trigger_reason"] != "rss" {
		t.Errorf("trigger_reason = %q; want %q", data["trigger_reason"], "rss")
	}
}

func TestOOMWatchdog_Alert_BothExceeded(t *testing.T) {
	eb := events.NewEventBus()
	w := NewOOMWatchdog(eb)

	collected := collectEvents(t, eb)

	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return &OOMProbeResult{
			NodeCount:     600,
			TotalRSSBytes: 60 * 1024 * 1024 * 1024, // 60 GB
		}, nil
	})

	w.doProbe()

	events := collected()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	data := events[0].Data.(map[string]interface{})
	if data["trigger_reason"] != "both" {
		t.Errorf("trigger_reason = %q; want %q", data["trigger_reason"], "both")
	}
}

func TestOOMWatchdog_Cooldown_SuppressesDuplicate(t *testing.T) {
	eb := events.NewEventBus()
	w := NewOOMWatchdog(eb)
	w.cooldownDuration = 5 * time.Minute // Long cooldown to ensure suppression

	collected := collectEvents(t, eb)

	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return &OOMProbeResult{
			NodeCount:     600,
			TotalRSSBytes: 100 * 1024 * 1024,
		}, nil
	})

	// First probe should emit.
	w.doProbe()
	// Second probe (same state, within cooldown) should be suppressed.
	w.doProbe()

	events := collected()
	if len(events) != 1 {
		t.Errorf("expected 1 event (second suppressed by cooldown), got %d", len(events))
		for i, e := range events {
			t.Logf("event %d: %+v", i, e.Data)
		}
	}
}

func TestOOMWatchdog_Cooldown_ReAlertsAfterDuration(t *testing.T) {
	eb := events.NewEventBus()
	w := NewOOMWatchdog(eb)
	w.cooldownDuration = 100 * time.Millisecond

	collected := collectEvents(t, eb)

	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return &OOMProbeResult{
			NodeCount:     600,
			TotalRSSBytes: 100 * 1024 * 1024,
		}, nil
	})

	// First probe: event emitted.
	w.doProbe()

	// Wait for cooldown to expire.
	time.Sleep(150 * time.Millisecond)

	// Second probe: event should be emitted again (cooldown expired).
	w.doProbe()

	events := collected()
	if len(events) != 2 {
		t.Errorf("expected 2 events (re-alert after cooldown), got %d", len(events))
	}
}

func TestOOMWatchdog_StateChange_AlertsImmediately(t *testing.T) {
	eb := events.NewEventBus()
	w := NewOOMWatchdog(eb)
	w.cooldownDuration = 5 * time.Minute // Long cooldown so state change is the only reason for second alert

	collected := collectEvents(t, eb)

	// First probe: node_count exceeded.
	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return &OOMProbeResult{
			NodeCount:     600,
			TotalRSSBytes: 100 * 1024 * 1024,
		}, nil
	})
	w.doProbe()

	// Second probe: state changes to rss (nodes below, RSS above).
	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return &OOMProbeResult{
			NodeCount:     5,
			TotalRSSBytes: 60 * 1024 * 1024 * 1024, // 60 GB
		}, nil
	})
	w.doProbe()

	events := collected()
	if len(events) != 2 {
		t.Fatalf("expected 2 events (state change bypasses cooldown), got %d", len(events))
	}

	data1 := events[0].Data.(map[string]interface{})
	data2 := events[1].Data.(map[string]interface{})

	if data1["trigger_reason"] != "node_count" {
		t.Errorf("first event trigger_reason = %q; want %q", data1["trigger_reason"], "node_count")
	}
	if data2["trigger_reason"] != "rss" {
		t.Errorf("second event trigger_reason = %q; want %q", data2["trigger_reason"], "rss")
	}
}

func TestOOMWatchdog_Recovery_ClearsState(t *testing.T) {
	eb := events.NewEventBus()
	w := NewOOMWatchdog(eb)
	w.cooldownDuration = 5 * time.Minute // Long cooldown

	collected := collectEvents(t, eb)

	// First probe: 600 nodes → "node_count" event.
	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return &OOMProbeResult{
			NodeCount:     600,
			TotalRSSBytes: 100 * 1024 * 1024,
		}, nil
	})
	w.doProbe()

	// Second probe: all below thresholds → "none" state, cooldown reset.
	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return &OOMProbeResult{
			NodeCount:     5,
			TotalRSSBytes: 100 * 1024 * 1024,
		}, nil
	})
	w.doProbe()

	// Third probe: 600 nodes again → immediate re-alert (state changed from "none" to "node_count").
	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return &OOMProbeResult{
			NodeCount:     600,
			TotalRSSBytes: 100 * 1024 * 1024,
		}, nil
	})
	w.doProbe()

	events := collected()
	if len(events) != 2 {
		t.Fatalf("expected 2 events (first alert + re-alert after recovery), got %d", len(events))
	}

	data1 := events[0].Data.(map[string]interface{})
	data2 := events[1].Data.(map[string]interface{})

	if data1["trigger_reason"] != "node_count" {
		t.Errorf("first event trigger_reason = %q; want %q", data1["trigger_reason"], "node_count")
	}
	if data2["trigger_reason"] != "node_count" {
		t.Errorf("second event trigger_reason = %q; want %q", data2["trigger_reason"], "node_count")
	}
}

func TestOOMWatchdog_ProbeError_IsHandled(t *testing.T) {
	eb := events.NewEventBus()
	w := NewOOMWatchdog(eb)

	collected := collectEvents(t, eb)

	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return nil, &probeError{}
	})

	// Must not panic.
	w.doProbe()

	events := collected()
	if len(events) != 0 {
		t.Errorf("expected 0 events on probe error, got %d", len(events))
	}
}

// probeError is a simple error type for tests.
type probeError struct{}

func (e *probeError) Error() string { return "probe failed" }

// --- Test helpers ---

func collectEvents(t *testing.T, eb *events.EventBus) func() []events.UIEvent {
	t.Helper()

	ch := eb.Subscribe("test-collector")
	var collected []events.UIEvent
	var mu sync.Mutex

	go func() {
		for evt := range ch {
			mu.Lock()
			collected = append(collected, evt)
			mu.Unlock()
		}
	}()

	collect := func() []events.UIEvent {
		// Small pause for async delivery.
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		out := make([]events.UIEvent, len(collected))
		copy(out, collected)
		return out
	}

	return collect
}

// --- Background goroutine tests ---

func TestOOMWatchdog_Start_StopsOnContextCancel(t *testing.T) {
	eb := events.NewEventBus()
	w := NewOOMWatchdog(eb)
	w.interval = 10 * time.Millisecond

	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return &OOMProbeResult{
			NodeCount:     0,
			TotalRSSBytes: 0,
		}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	w.Start(ctx)

	// Let a couple of probes fire.
	time.Sleep(35 * time.Millisecond)

	// Cancel context; goroutine should exit.
	cancel()

	// Give the goroutine time to observe the cancellation.
	time.Sleep(20 * time.Millisecond)

	// If we get here without a leak/goroutine panic, the test passes.
	// We can't directly observe goroutine exit, but if the context
	// cancellation didn't work, the goroutine would keep running and
	// the test would be flaky on shutdown.
}

func TestOOMWatchdog_Start_EmitsAlertsInBackground(t *testing.T) {
	eb := events.NewEventBus()
	w := NewOOMWatchdog(eb)
	w.interval = 20 * time.Millisecond
	w.cooldownDuration = 5 * time.Minute

	collected := collectEvents(t, eb)

	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return &OOMProbeResult{
			NodeCount:     600,
			TotalRSSBytes: 100 * 1024 * 1024,
		}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.Start(ctx)

	// Wait for the first probe (runs immediately) + one more cycle.
	time.Sleep(60 * time.Millisecond)

	cancel()
	time.Sleep(20 * time.Millisecond)

	events := collected()
	if len(events) < 1 {
		t.Errorf("expected at least 1 event from background probes, got %d", len(events))
	}

	// Only the first probe should emit; subsequent ones are suppressed by cooldown.
	if len(events) > 1 {
		t.Errorf("expected 1 event (cooldown should suppress duplicates), got %d", len(events))
	}
}

// --- Event payload shape tests ---

func TestOOMWatchdog_AlertEventPayload(t *testing.T) {
	eb := events.NewEventBus()
	w := NewOOMWatchdog(eb)
	w.nodeCountThreshold = 500
	w.rssThresholdBytes = 50 * 1024 * 1024 * 1024

	collected := collectEvents(t, eb)

	w.setProbeFunc(func() (*OOMProbeResult, error) {
		return &OOMProbeResult{
			NodeCount:     750,
			TotalRSSBytes: 60 * 1024 * 1024 * 1024,
		}, nil
	})

	w.doProbe()

	events := collected()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	data := events[0].Data.(map[string]interface{})

	// node_count is stored as int in the map[string]interface{} (not JSON-encoded).
	nodeCount, ok := data["node_count"].(int)
	if !ok {
		nodeCountF, ok2 := data["node_count"].(float64)
		if !ok2 {
			t.Errorf("node_count has unexpected type %T", data["node_count"])
		} else {
			nodeCount = int(nodeCountF)
		}
	}
	if nodeCount != 750 {
		t.Errorf("node_count = %d; want 750", nodeCount)
	}

	thresholdNodeCount, ok := data["threshold_node_count"].(int)
	if !ok {
		thresholdNodeCountF, ok2 := data["threshold_node_count"].(float64)
		if !ok2 {
			t.Errorf("threshold_node_count has unexpected type %T", data["threshold_node_count"])
		} else {
			thresholdNodeCount = int(thresholdNodeCountF)
		}
	}
	if thresholdNodeCount != 500 {
		t.Errorf("threshold_node_count = %d; want 500", thresholdNodeCount)
	}
	if data["trigger_reason"] != "both" {
		t.Errorf("trigger_reason = %q; want %q", data["trigger_reason"], "both")
	}
	if _, ok := data["total_rss_bytes"]; !ok {
		t.Error("missing total_rss_bytes in event payload")
	}
	if _, ok := data["threshold_rss_bytes"]; !ok {
		t.Error("missing threshold_rss_bytes in event payload")
	}
	if _, ok := data["timestamp"]; !ok {
		t.Error("missing timestamp in event payload")
	}
}
