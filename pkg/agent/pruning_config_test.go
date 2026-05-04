package agent

import (
	"testing"
)

func TestSetPruningStrategyNilPruner(t *testing.T) {
	t.Parallel()

	a := &Agent{
		state: NewAgentStateManager(false),
	}
	// Should not panic when pruner is nil
	a.SetPruningStrategy(PruneStrategySlidingWindow)
}

func TestSetPruningStrategyWithPruner(t *testing.T) {
	t.Parallel()

	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.state.SetConversationPruner(NewConversationPruner(false))
	a.SetPruningStrategy(PruneStrategySlidingWindow)
}

func TestSetPruningThresholdNilPruner(t *testing.T) {
	t.Parallel()

	a := &Agent{
		state: NewAgentStateManager(false),
	}
	// Should not panic when pruner is nil
	a.SetPruningThreshold(0.85)
}

func TestSetPruningThresholdWithPruner(t *testing.T) {
	t.Parallel()

	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.state.SetConversationPruner(NewConversationPruner(false))
	a.SetPruningThreshold(0.85)
}

func TestSetRecentMessagesToKeepNilPruner(t *testing.T) {
	t.Parallel()

	a := &Agent{
		state: NewAgentStateManager(false),
	}
	// Should not panic when pruner is nil
	a.SetRecentMessagesToKeep(10)
}

func TestSetPruningSlidingWindowSizeNilPruner(t *testing.T) {
	t.Parallel()

	a := &Agent{
		state: NewAgentStateManager(false),
	}
	// Should not panic when pruner is nil
	a.SetPruningSlidingWindowSize(20)
}

func TestGetPruningStatsNilPruner(t *testing.T) {
	t.Parallel()

	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.state.SetConversationPruner(nil)
	stats := a.GetPruningStats()
	if stats == nil {
		t.Fatal("GetPruningStats returned nil")
	}
	if enabled, ok := stats["enabled"].(bool); !ok || enabled {
		t.Errorf("GetPruningStats with nil pruner should have enabled=false, got: %v", stats["enabled"])
	}
}

func TestGetPruningStatsWithPruner(t *testing.T) {
	t.Parallel()

	a := &Agent{
		state: NewAgentStateManager(false),
	}
	pruner := NewConversationPruner(false)
	a.state.SetConversationPruner(pruner)

	stats := a.GetPruningStats()
	if stats == nil {
		t.Fatal("GetPruningStats returned nil")
	}

	if enabled, ok := stats["enabled"].(bool); !ok || !enabled {
		t.Errorf("GetPruningStats with pruner should have enabled=true, got: %v", stats["enabled"])
	}

	if strategy, ok := stats["strategy"].(PruningStrategy); !ok || strategy != PruneStrategyAdaptive {
		t.Errorf("GetPruningStats strategy = %v, want %v", stats["strategy"], PruneStrategyAdaptive)
	}

	if _, ok := stats["threshold"]; !ok {
		t.Error("GetPruningStats should contain 'threshold'")
	}

	if _, ok := stats["current_message_count"]; !ok {
		t.Error("GetPruningStats should contain 'current_message_count'")
	}

	if _, ok := stats["current_context_usage"]; !ok {
		t.Error("GetPruningStats should contain 'current_context_usage'")
	}
}

func TestDisableAutoPruningNilPruner(t *testing.T) {
	t.Parallel()

	a := &Agent{
		state: NewAgentStateManager(false),
	}
	// Should not panic when pruner is nil
	a.DisableAutoPruning()
}

func TestEnableAutoPruningNilPruner(t *testing.T) {
	t.Parallel()

	a := &Agent{
		state: NewAgentStateManager(false),
	}
	// Should not panic when pruner is nil
	a.EnableAutoPruning()
}
