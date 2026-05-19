package agent

import (
	"github.com/sprout-foundry/seed/core"
)

// ConversationPruner is aliased to seed's core.ConversationPruner so
// sprout's existing callers (submanager_state.go, pruning_config.go,
// tests) continue to compile against the same type while the
// implementation lives in seed and is available to other consumers.
type ConversationPruner = core.ConversationPruner

// PruningStrategy is aliased to seed's strategy type. Sprout's constants
// below mirror seed's so call sites need no rewrite.
type PruningStrategy = core.PruningStrategy

// Pruning strategy constants — re-exported from seed for backward
// compatibility with sprout call sites that reference them unqualified.
const (
	PruneStrategyNone          = core.PruneStrategyNone
	PruneStrategySlidingWindow = core.PruneStrategySlidingWindow
	PruneStrategyImportance    = core.PruneStrategyImportance
	PruneStrategyHybrid        = core.PruneStrategyHybrid
	PruneStrategyAdaptive      = core.PruneStrategyAdaptive
)

// MessageImportance is aliased from seed for tests/diagnostics that inspect
// the structured score output of the importance scorer.
type MessageImportance = core.MessageImportance

// NewConversationPruner constructs a pruner with sprout's traditional
// defaults: adaptive strategy with seed-default thresholds. The debug flag
// is retained for caller compatibility but unused — seed routes
// observability through the EventPublisher instead of stderr prints.
func NewConversationPruner(debug bool) *core.ConversationPruner {
	_ = debug
	return core.NewConversationPruner(core.PrunerOptions{
		Strategy: core.PruneStrategyAdaptive,
	})
}

// PruningConfig preserves the historical "single source of truth" symbol
// some sprout tests reference. Values come from seed's defaults so any
// drift between sprout and seed is impossible by construction.
//
// New code should not read from this — query the pruner instance directly
// or use seed's exported constants. Retained as a thin shim only.
var PruningConfig = struct {
	Default struct {
		StandardPercent float64
		MinMessages     int
		RecentMessages  int
		SlidingWindow   int
	}
	Structural struct {
		RecentMessagesToKeep int
		MinMessagesToCompact int
		MinMiddleMessages    int
	}
	AgenticRequiredAvailableTokens int
}{
	Default: struct {
		StandardPercent float64
		MinMessages     int
		RecentMessages  int
		SlidingWindow   int
	}{
		StandardPercent: 0.87,
		MinMessages:     5,
		RecentMessages:  24,
		SlidingWindow:   30,
	},
	Structural: struct {
		RecentMessagesToKeep int
		MinMessagesToCompact int
		MinMiddleMessages    int
	}{
		RecentMessagesToKeep: core.StructuralRecentToKeep,
		MinMessagesToCompact: core.StructuralMinMessagesToCompact,
		MinMiddleMessages:    core.StructuralMinMiddleMessages,
	},
	AgenticRequiredAvailableTokens: 12000,
}
