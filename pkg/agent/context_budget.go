package agent

import core "github.com/sprout-foundry/seed/core"

// The compaction budget is seed's (core.CompactionBudgetFor); the agent
// supplies its context profile's trigger and its configured ceiling.

const (
	reservedForResponseFraction    = core.ReservedForResponseFraction
	reservedForThinkingFraction    = core.ReservedForThinkingFraction
	reservedForToolIOFraction      = core.ReservedForToolIOFraction
	defaultCompactionCeilingTokens = core.DefaultCompactionCeilingTokens
)

func totalReservedFraction() float64 {
	return reservedForResponseFraction + reservedForThinkingFraction + reservedForToolIOFraction
}

// compactionBudgetFor is the compaction budget for a window of contextLimit
// tokens: when to compact, where to settle, and the continuation summary.
func (a *Agent) compactionBudgetFor(contextLimit int) core.CompactionBudget {
	return core.CompactionBudgetFor(contextLimit, a.contextProfile.CompactionTriggerFraction, a.compactionCeilingFor())
}

// computeCompactionTriggerFraction is the window-share trigger, before the
// window's own edges and the ceiling apply.
func (a *Agent) computeCompactionTriggerFraction() float64 {
	return a.compactionBudgetFor(0).TriggerFraction
}

// compactionTriggerFractionFor is the trigger for a window of contextLimit
// tokens. A context cap is already reflected in contextLimit, so a cap below
// the ceiling binds instead of it.
func (a *Agent) compactionTriggerFractionFor(contextLimit int) float64 {
	return a.compactionBudgetFor(contextLimit).TriggerFraction
}

// compactionCeilingFor returns the configured absolute live-context ceiling,
// or the built-in default when unset (0 means the default).
func (a *Agent) compactionCeilingFor() int {
	if cfg := a.GetConfig(); cfg != nil && cfg.CompactionCeilingTokens > 0 {
		return cfg.CompactionCeilingTokens
	}
	return defaultCompactionCeilingTokens
}

// CompactionCeilingTokens returns the effective absolute live-context ceiling:
// the configured value when set, otherwise the built-in default.
func (a *Agent) CompactionCeilingTokens() int {
	return a.compactionCeilingFor()
}
