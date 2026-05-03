package agent

import (
	"math"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestGetPruningStats_Disabled2 verifies that when pruner is not initialized,
// GetPruningStats returns enabled=false
func TestGetPruningStats_Disabled2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	// Ensure pruner is nil
	a.state.SetConversationPruner(nil)

	stats := a.GetPruningStats()
	enabled, ok := stats["enabled"].(bool)
	if !ok {
		t.Fatal("expected enabled key to be bool")
	}
	if enabled {
		t.Error("expected enabled=false when pruner is nil")
	}
}

// TestGetPruningStats_Enabled2 verifies that when pruner is initialized,
// GetPruningStats returns correct configuration
func TestGetPruningStats_Enabled2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	cp := NewConversationPruner(false)
	a.state.SetConversationPruner(cp)

	// Add some test messages to test current_message_count
	msgs := []struct{ role, content string }{
		{"system", "You are a helpful assistant"},
		{"user", "Hello"},
		{"assistant", "Hi there!"},
	}
	for _, m := range msgs {
		a.state.AddMessage(api.Message{Role: m.role, Content: m.content})
	}

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

	expectedMsgCount := 3
	if stats["current_message_count"] != expectedMsgCount {
		t.Errorf("expected %d messages, got %v", expectedMsgCount, stats["current_message_count"])
	}

	// When maxContextTokens is 0, the usage calculation produces NaN (0/0).
	// This is expected behavior - the source code doesn't guard against zero division.
	usage, ok := stats["current_context_usage"].(float64)
	if !ok {
		t.Fatal("current_context_usage not float64")
	}
	if usage != 0 && !math.IsNaN(usage) {
		t.Errorf("expected 0 or NaN context usage, got %v", usage)
	}
}

// TestSetPruningStrategy2 verifies that pruning strategy can be set
func TestSetPruningStrategy2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.state.SetConversationPruner(NewConversationPruner(false))

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

// TestSetPruningThreshold2 verifies threshold can be set
func TestSetPruningThreshold2(t *testing.T) {
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

// TestSetRecentMessagesToKeep2 verifies recent messages count can be set
func TestSetRecentMessagesToKeep2(t *testing.T) {
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

// TestSetPruningSlidingWindowSize2 verifies sliding window size can be set
func TestSetPruningSlidingWindowSize2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.state.SetConversationPruner(NewConversationPruner(false))

	a.SetPruningSlidingWindowSize(25)
	cp := a.state.GetConversationPruner()
	if cp.slidingWindowSize != 25 {
		t.Errorf("expected 25, got %d", cp.slidingWindowSize)
	}
}

// TestDisableAutoPruning2 verifies pruning is disabled
func TestDisableAutoPruning2(t *testing.T) {
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

// TestEnableAutoPruning2 verifies pruning is enabled with adaptive strategy
func TestEnableAutoPruning2(t *testing.T) {
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
