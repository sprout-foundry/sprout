package agent

// agent_budgets.go — fleet/USD budget callbacks, the per-session
// computer-use app allowlist, and the slash-command registry accessors,
// split out of agent.go.
import (
	"sync/atomic"
)

// SetFleetBudget enables per-LLM-call fleet budget tracking for this agent.
func (a *Agent) SetFleetBudget(tracker *atomic.Int64, limit int64) {
	a.fleetBudgetTracker = tracker
	a.fleetBudgetLimit = limit
	a.fleetBudgetTrunc.Store(false)
}

// FleetBudgetExceeded reports whether the fleet budget was exceeded (mid-run truncation).
func (a *Agent) FleetBudgetExceeded() bool {
	return a.fleetBudgetTrunc.Load()
}

// SetFleetUsdBudget attaches a shared USD budget to this agent. The budget
// is shared by reference, so all agents (primary + subagents) that hold
// the same pointer debit to the same counter.
func (a *Agent) SetFleetUsdBudget(b *FleetUsdBudget) {
	a.fleetUsdBudget = b
	a.fleetBudgetTrunc.Store(false)
}

// GetFleetUsdBudget returns the agent's USD budget, or nil if none is set.
// Used by the SubagentRunner to propagate the same budget to spawned
// subagents (so the cap is workflow-wide, not per-agent).
func (a *Agent) GetFleetUsdBudget() *FleetUsdBudget {
	return a.fleetUsdBudget
}

// SetBudgetWarningCallback registers a function invoked when the USD budget
// first crosses each configured warning threshold (fired at most once per
// threshold). Pass nil to unregister.
func (a *Agent) SetBudgetWarningCallback(fn func(threshold, spent, limit float64)) {
	if fn == nil {
		a.budgetWarningCallback.Store((func(threshold, spent, limit float64))(nil))
		return
	}
	a.budgetWarningCallback.Store(fn)
}

// SetBudgetExceededCallback registers a function invoked when the USD budget
// is first reached or surpassed. Pass nil to unregister.
func (a *Agent) SetBudgetExceededCallback(fn func(spent, limit float64)) {
	if fn == nil {
		a.budgetExceededCallback.Store((func(spent, limit float64))(nil))
		return
	}
	a.budgetExceededCallback.Store(fn)
}

// ---------------------------------------------------------------------------
// Per-session app allowlist for destructive-app gate.
// ---------------------------------------------------------------------------

// IsAppAllowedForComputerUse reports whether the given app key is in the
// per-session allowlist. The key is a bundle ID (macOS) or a window class
// (Linux). Guarded by computerUseMu.
func (a *Agent) IsAppAllowedForComputerUse(key string) bool {
	if a == nil {
		return false
	}
	a.computerUseMu.Lock()
	defer a.computerUseMu.Unlock()
	return a.computerUseAppAllowlist != nil && a.computerUseAppAllowlist[key]
}

// AllowAppForComputerUse adds the given app key to the per-session
// allowlist. Guarded by computerUseMu.
func (a *Agent) AllowAppForComputerUse(key string) {
	if a == nil {
		return
	}
	a.computerUseMu.Lock()
	defer a.computerUseMu.Unlock()
	if a.computerUseAppAllowlist == nil {
		a.computerUseAppAllowlist = make(map[string]bool)
	}
	a.computerUseAppAllowlist[key] = true
}

// ---------------------------------------------------------------------------
// Command Classification and Steer Allowlist.
// ---------------------------------------------------------------------------

// SetSlashCommands stores the command registry on the agent.
// Called after the registry is created in cmd/agent_mode_interactive.go.
func (a *Agent) SetSlashCommands(registry any) {
	a.slashCommands = registry
}

// SlashCommands returns the agent's command registry, or nil if not set.
func (a *Agent) SlashCommands() any {
	if a == nil {
		return nil
	}
	return a.slashCommands
}
