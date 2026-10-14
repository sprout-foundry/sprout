package agent

// economy_mode.go — per-session economy mode.
//
// Economy mode routes more delegated work to the cheaper worker model. It is
// a per-session setting a user turns on with a CLI flag or the web UI, or a
// host sets through the session API (e.g. a platform switching a session to
// economy as a user nears their credit limit).
//
// Two effects, deliberately asymmetric:
//
//   - Subagents: with economy mode on, a subagent that would otherwise run on
//     the coder model runs on the worker model instead. This applies whenever
//     the mode is on, including mid-session, and is the only effect a
//     mid-session change has.
//   - Main loop: for a NEW session only (the mode set before the first turn),
//     the main loop runs on the worker model. Changing the mode MID-SESSION
//     never switches the main loop's model — a switch would re-read the whole
//     conversation cold and erase the prompt-cache saving the mode exists to
//     capture. The session records whether the main loop was started in
//     economy mode so a later toggle cannot move it. Moving the main loop
//     back to the coder model is an explicit /model or /provider choice; there
//     is no automatic escalation.

import (
	"sync"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// economyState is the agent's per-session economy-mode state.
type economyState struct {
	mu sync.Mutex
	// enabled is the current mode.
	enabled bool
	// mainLoopEconomy records whether the main loop was started in economy
	// mode (the mode was set before the first turn). Once true it stays true
	// for the session: a later toggle changes only subagent routing, never
	// the main-loop model.
	mainLoopEconomy bool
	// mainLoopApplied records that the main-loop model switch has run. It is
	// a once-guard: the switch must happen at most once per session, because
	// SetProvider resets the current-context token count and normalizes the
	// conversation for the model's syntax — re-running it every turn would
	// degrade context tracking and defeat the prompt-cache preservation the
	// mode exists to protect.
	mainLoopApplied bool
	// started reports whether the session's first turn has begun. Set by
	// markSessionStarted at the first turn; before that a mode change is a
	// "new session" change and may set mainLoopEconomy.
	started bool
}

// SetEconomyMode enables or disables economy mode for this session. Called
// before the first turn it also starts the main loop in economy mode (for a
// new session); called mid-session it changes only which subagents get the
// worker model, leaving the main-loop model where it is.
func (a *Agent) SetEconomyMode(enabled bool) {
	if a == nil {
		return
	}
	a.economy.mu.Lock()
	defer a.economy.mu.Unlock()
	a.economy.enabled = enabled
	if !a.economy.started {
		// New session: the main loop starts in economy mode when the mode is
		// on. A later disable mid-session does not move it back (the switch
		// is one-way for the session so the prompt cache is never invalidated
		// by a model change).
		a.economy.mainLoopEconomy = enabled
	}
}

// GetEconomyMode reports whether economy mode is currently on.
func (a *Agent) GetEconomyMode() bool {
	if a == nil {
		return false
	}
	a.economy.mu.Lock()
	defer a.economy.mu.Unlock()
	return a.economy.enabled
}

// GetMainLoopEconomy reports whether the main loop is running in economy mode
// for this session (the mode was on when the session started).
func (a *Agent) GetMainLoopEconomy() bool {
	if a == nil {
		return false
	}
	a.economy.mu.Lock()
	defer a.economy.mu.Unlock()
	return a.economy.mainLoopEconomy
}

// markSessionStarted records that the session's first turn has begun, so a
// later economy-mode change is a mid-session change (subagent routing only)
// rather than a new-session change. Idempotent.
func (a *Agent) markSessionStarted() {
	if a == nil {
		return
	}
	a.economy.mu.Lock()
	a.economy.started = true
	a.economy.mu.Unlock()
}

// MarkSessionStarted is the exported form of markSessionStarted, for a caller
// that has loaded a prior conversation (a resumed session) before the first
// turn: it tells the agent the session is already under way, so a subsequent
// SetEconomyMode is a mid-session change that re-routes subagents without
// switching the main-loop model the resumed prompt cache was built on.
func (a *Agent) MarkSessionStarted() {
	a.markSessionStarted()
}

// economyWorkerRoute resolves the worker role's (provider, model) — the model
// a subagent or the main loop runs on under economy mode. Falls back to the
// coder role when the worker role is unset (ResolveWorkerRole), so economy
// mode with no worker configured behaves exactly as before.
func (a *Agent) economyWorkerRoute() (string, string) {
	if cfg := a.GetConfig(); cfg != nil {
		return cfg.ResolveWorkerRole()
	}
	return "", ""
}

// applyEconomyMainLoopModel switches the main loop onto the worker model when
// the session started in economy mode. It runs at most ONCE per session (a
// once-guard), because SetProvider resets the current-context token count and
// normalizes the conversation for the model's syntax; re-running it every turn
// would degrade context tracking and defeat the prompt-cache preservation the
// mode exists to protect. A no-op when economy mode is off, the worker route
// is empty, or the switch already ran. Returns true when it switched.
func (a *Agent) applyEconomyMainLoopModel() bool {
	if a == nil {
		return false
	}
	a.economy.mu.Lock()
	if !a.economy.mainLoopEconomy || a.economy.mainLoopApplied {
		a.economy.mu.Unlock()
		return false
	}
	// Claim the once-guard before doing the work, so a concurrent turn cannot
	// run the switch twice. A failed switch leaves the guard set: retrying a
	// provider/model switch every turn is exactly what the guard prevents.
	a.economy.mainLoopApplied = true
	a.economy.mu.Unlock()

	cfg := a.GetConfig()
	if cfg == nil || !cfg.HasExplicitRole(configuration.RoleWorker) {
		// No explicit worker role: ResolveWorkerRole falls back to the coder
		// role, so switching would be a no-op in effect (and would wrongly
		// move a session off an explicitly chosen coder model). Leave the main
		// loop where it is.
		return false
	}
	provider, model := a.economyWorkerRoute()
	if model == "" {
		return false
	}
	// SetProvider/SetModel install the session override; the coder model is
	// still reachable for escalation through ResolveRole(RoleCoder).
	if provider != "" {
		clientType, err := configuration.MapProviderStringToClientType(a.GetConfig(), provider)
		if err != nil {
			a.Logger().Debug("economy mode: resolve provider %q failed: %v\n", provider, err)
			return false
		}
		if err := a.SetProvider(clientType); err != nil {
			a.Logger().Debug("economy mode: set provider %q failed: %v\n", provider, err)
			return false
		}
	}
	if err := a.SetModel(model); err != nil {
		a.Logger().Debug("economy mode: set model %q failed: %v\n", model, err)
		return false
	}
	a.Logger().Info("economy mode: main loop started on the worker model (%s/%s)\n", provider, model)
	return true
}

// economySubagentRoute reports whether a subagent that would otherwise run on
// the coder role should instead run on the worker role under economy mode,
// and returns the worker role's (provider, model). It applies only when the
// worker role is EXPLICITLY configured and resolves to a different pair than
// the coder role: with no worker role set, ResolveWorkerRole falls back to
// the coder role's pair, and routing to it would be a no-op in model terms
// while still re-labelling the task as worker-role (pulling it into worker
// verification and worker cost attribution). The caller only applies it when
// the subagent's resolution fell through to the coder role (no explicit
// persona provider/model, not a reviewer, not already the worker).
func (a *Agent) economySubagentRoute() (string, string, bool) {
	if a == nil || !a.GetEconomyMode() {
		return "", "", false
	}
	cfg := a.GetConfig()
	if cfg == nil || !cfg.HasExplicitRole(configuration.RoleWorker) {
		return "", "", false
	}
	provider, model := cfg.ResolveWorkerRole()
	if model == "" {
		return "", "", false
	}
	// No-op when the worker route equals the coder route: there is nothing
	// cheaper to route to, so leave the task on the coder role.
	coderProvider, coderModel := cfg.ResolveRole(configuration.RoleCoder)
	if provider == coderProvider && model == coderModel {
		return "", "", false
	}
	return provider, model, true
}

// EconomyDescription is the human-readable effect of the current mode, for
// the session info surfaces (the UI shows the mode and its effect plainly).
func (a *Agent) EconomyDescription() string {
	if a == nil || !a.GetEconomyMode() {
		return "off"
	}
	cfg := a.GetConfig()
	if cfg == nil || !cfg.HasExplicitRole(configuration.RoleWorker) {
		// No worker model configured: the worker role falls back to the coder
		// model, so the mode is a no-op in effect. Say so rather than naming a
		// route that is not actually applied.
		return "on (no cheaper worker model configured — no effect)"
	}
	provider, model := cfg.ResolveWorkerRole()
	route := model
	if provider != "" {
		route = provider + "/" + model
	}
	if a.GetMainLoopEconomy() {
		return "on (main loop + subagents on " + route + ")"
	}
	return "on (subagents on " + route + "; main loop unchanged)"
}
