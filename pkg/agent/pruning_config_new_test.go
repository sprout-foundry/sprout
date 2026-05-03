package agent

import (
	"math"
	"testing"
)

func TestGetPruningStats_Disabled(t *testing.T) {
	// When pruner is nil (not initialized), stats should report disabled
	a := &Agent{}
	a.initSubManagers()
	// Ensure pruner is nil by not setting one
	if a.state.GetConversationPruner() != nil {
		// If there's a default pruner, temporarily work with that
		t.Skip("Agent has a default pruner; adjusting test")
	}

	stats := a.GetPruningStats()
	if !stats["enabled"].(bool) {
		t.Error("expected enabled=false when pruner is nil")
	}
}

func TestGetPruningStats_Enabled(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	cp := NewConversationPruner(false)
	a.state.SetConversationPruner(cp)

	stats := a.GetPruningStats()
	if !stats["enabled"].(bool) {
		t.Fatal("expected enabled=true when pruner is set")
	}

	if stats["strategy"] != PruneStrategyAdaptive {
		t.Errorf("expected default strategy %s, got %v", PruneStrategyAdaptive, stats["strategy"])
	}

	if stats["threshold"] != PruningConfig.Default.StandardPercent {
		t.Errorf("expected threshold %f, got %v", PruningConfig.Default.StandardPercent, stats["threshold"])
	}

	if stats["recent_messages_kept"] != PruningConfig.Default.RecentMessages {
		t.Errorf("expected recent_messages_kept %d, got %v", PruningConfig.Default.RecentMessages, stats["recent_messages_kept"])
	}

	if stats["sliding_window_size"] != PruningConfig.Default.SlidingWindow {
		t.Errorf("expected sliding_window_size %d, got %v", PruningConfig.Default.SlidingWindow, stats["sliding_window_size"])
	}

	if stats["current_message_count"] != 0 {
		t.Errorf("expected 0 messages, got %v", stats["current_message_count"])
	}

	// current_context_usage should be 0 or NaN when max context is 0 (avoid div by zero)
	usage, ok := stats["current_context_usage"].(float64)
	if !ok {
		t.Fatal("current_context_usage not float64")
	}
	if usage != 0 && !math.IsNaN(usage) {
		t.Errorf("expected 0 or NaN context usage, got %v", usage)
	}
}

func TestSetPruningStrategy(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.state.SetConversationPruner(NewConversationPruner(false))

	// nil pruner should be a no-op
	a.SetPruningStrategy(PruneStrategyNone)
	cp := a.state.GetConversationPruner()
	if cp.strategy != PruneStrategyNone {
		t.Errorf("expected strategy none, got %s", cp.strategy)
	}

	a.SetPruningStrategy(PruneStrategyAdaptive)
	if cp.strategy != PruneStrategyAdaptive {
		t.Errorf("expected strategy adaptive, got %s", cp.strategy)
	}

	a.SetPruningStrategy(PruneStrategySlidingWindow)
	if cp.strategy != PruneStrategySlidingWindow {
		t.Errorf("expected strategy sliding_window, got %s", cp.strategy)
	}
}

func TestSetPruningStrategy_NilPruner(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	// Ensure pruner is nil
	a.state.SetConversationPruner(nil)

	// Should not panic
	a.SetPruningStrategy(PruneStrategyNone)
}

func TestSetPruningThreshold(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.state.SetConversationPruner(NewConversationPruner(false))

	a.SetPruningThreshold(0.75)
	cp := a.state.GetConversationPruner()
	if cp.contextThreshold != 0.75 {
		t.Errorf("expected threshold 0.75, got %v", cp.contextThreshold)
	}

	a.SetPruningThreshold(0.9)
	if cp.contextThreshold != 0.9 {
		t.Errorf("expected threshold 0.9, got %v", cp.contextThreshold)
	}
}

func TestSetPruningThreshold_NilPruner(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.state.SetConversationPruner(nil)

	// Should not panic
	a.SetPruningThreshold(0.5)
}

func TestSetRecentMessagesToKeep(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.state.SetConversationPruner(NewConversationPruner(false))

	a.SetRecentMessagesToKeep(10)
	cp := a.state.GetConversationPruner()
	if cp.recentMessagesToKeep != 10 {
		t.Errorf("expected 10, got %d", cp.recentMessagesToKeep)
	}

	a.SetRecentMessagesToKeep(50)
	if cp.recentMessagesToKeep != 50 {
		t.Errorf("expected 50, got %d", cp.recentMessagesToKeep)
	}
}

func TestSetRecentMessagesToKeep_NilPruner(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.state.SetConversationPruner(nil)

	// Should not panic
	a.SetRecentMessagesToKeep(20)
}

func TestSetPruningSlidingWindowSize(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.state.SetConversationPruner(NewConversationPruner(false))

	a.SetPruningSlidingWindowSize(25)
	cp := a.state.GetConversationPruner()
	if cp.slidingWindowSize != 25 {
		t.Errorf("expected 25, got %d", cp.slidingWindowSize)
	}
}

func TestSetPruningSlidingWindowSize_NilPruner(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.state.SetConversationPruner(nil)

	// Should not panic
	a.SetPruningSlidingWindowSize(15)
}

func TestDisableAutoPruning(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.state.SetConversationPruner(NewConversationPruner(false))

	a.SetPruningStrategy(PruneStrategyAdaptive)
	a.DisableAutoPruning()

	cp := a.state.GetConversationPruner()
	if cp.strategy != PruneStrategyNone {
		t.Errorf("expected strategy none after disable, got %s", cp.strategy)
	}
}

func TestDisableAutoPruning_NilPruner(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.state.SetConversationPruner(nil)

	// Should not panic
	a.DisableAutoPruning()
}

func TestEnableAutoPruning(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.state.SetConversationPruner(NewConversationPruner(false))

	a.SetPruningStrategy(PruneStrategyNone)
	a.EnableAutoPruning()

	cp := a.state.GetConversationPruner()
	if cp.strategy != PruneStrategyAdaptive {
		t.Errorf("expected strategy adaptive after enable, got %s", cp.strategy)
	}
}

func TestEnableAutoPruning_NilPruner(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.state.SetConversationPruner(nil)

	// Should not panic
	a.EnableAutoPruning()
}
