// delegation.go — the benchmark's delegation axis: the run mode that
// selects how a run's work is delegated (coder only, worker delegation, or
// economy mode) and the per-run delegation metrics the report compares.
//
// The mode is a measurement input like the model: "coder" (the default)
// changes nothing; "delegated" points the worker role at a cheaper model so
// subagents route to it; "economy" turns on economy mode, which routes
// subagents to the worker model and starts a fresh session's main loop on it.
// The metrics come from the agent's delegated-task outcome records, so a run
// reports what actually happened (how many delegated tasks were verified,
// escalated, or failed) rather than what the mode intended.
package benchmark

import (
	"errors"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// Run modes a benchmark run can use. The empty string means the default
// ("coder"), so an existing run configuration is unchanged.
const (
	// RunModeCoder runs every step on the coder model — the baseline.
	RunModeCoder = "coder"
	// RunModeDelegated points the worker role at the spec's worker model so
	// subagents route to it (worker delegation without economy mode).
	RunModeDelegated = "delegated"
	// RunModeEconomy turns on economy mode: subagents route to the worker
	// model and a fresh session's main loop starts on it.
	RunModeEconomy = "economy"
)

// DelegationMetrics is a run's delegation outcome: how the run's delegated
// tasks finished, and the run's share of work done on the worker model.
// Zero for a coder-only run (no delegation happened).
type DelegationMetrics struct {
	// TaskCount is the number of delegated tasks the run recorded.
	TaskCount int `json:"task_count"`
	// Verified / Escalated / Failed are the per-outcome task counts.
	Verified  int `json:"verified"`
	Escalated int `json:"escalated"`
	Failed    int `json:"failed"`
	// WorkerTokens is the tokens the worker model completed (verified
	// without escalation), and TotalTokens the tokens across all delegated
	// tasks.
	WorkerTokens int `json:"worker_tokens"`
	TotalTokens  int `json:"total_tokens"`
	// EscalationRate is Escalated/TaskCount (0 when there is no delegated
	// work): the share of ALL delegated tasks the cheap model could not
	// finish and the coder model had to take over. The denominator is every
	// delegated task (not only worker-role ones), so the rate reads as the
	// run's overall escalation share. This is the headline number for
	// whether a cheaper model "holds up".
	EscalationRate float64 `json:"escalation_rate"`
	// WorkerTokenShare is WorkerTokens/TotalTokens (0 when no delegated
	// work): the share of delegated work the worker model handled.
	WorkerTokenShare float64 `json:"worker_token_share"`
}

// delegationMetricsFromOutcomes folds an agent's delegated-task outcome
// records into the run's delegation metrics. Pure: the same records always
// yield the same metrics.
func delegationMetricsFromOutcomes(outcomes []agent.DelegatedTaskOutcome) DelegationMetrics {
	m := DelegationMetrics{TaskCount: len(outcomes)}
	for _, o := range outcomes {
		m.TotalTokens += o.Tokens
		switch o.Outcome {
		case agent.OutcomeVerified:
			m.Verified++
		case agent.OutcomeEscalated:
			m.Escalated++
		case agent.OutcomeFailed:
			m.Failed++
		}
		if o.Role == configuration.RoleWorker && o.Outcome == agent.OutcomeVerified {
			m.WorkerTokens += o.Tokens
		}
	}
	if m.TaskCount > 0 {
		m.EscalationRate = float64(m.Escalated) / float64(m.TaskCount)
	}
	if m.TotalTokens > 0 {
		m.WorkerTokenShare = float64(m.WorkerTokens) / float64(m.TotalTokens)
	}
	return m
}

// applyRunMode writes the run mode's requirements into the run's
// configuration: "delegated" and "economy" point the worker role at the
// spec's worker model (so there is a cheaper model to route to), and
// "economy" additionally turns economy mode on for the agent. "coder" (or
// empty) leaves the worker role unset — explicitly cleared, so a shared
// ConfigManager cannot leak a previous spec's worker role into a coder-mode
// baseline run. A nil ConfigManager skips the config writes (a custom
// AgentFactory owns the run's configuration).
func (r *Runner) applyRunMode(spec ModelSpec) error {
	mode := spec.RunMode
	if mode == "" {
		if spec.WorkerModel != "" {
			// A worker model with no run mode is almost certainly a mistake:
			// the run would silently execute as a coder baseline and the
			// worker fields would be ignored. Fail loudly rather than let a
			// misleading benchmark run through.
			return errors.New("benchmark: ModelSpec.WorkerModel is set but RunMode is empty; set RunMode to RunModeDelegated or RunModeEconomy (or clear WorkerModel) so the run's delegation mode is explicit")
		}
		mode = RunModeCoder
	}
	if r.ConfigManager == nil {
		return nil
	}
	return r.ConfigManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		// Each spec's mode is a COMPLETE expression of the run's worker-routing
		// state. RunSuite drives every spec through one runner and one shared
		// manager, and configureRun narrows rather than clears, so without the
		// clear a coder-mode (or worker-less) spec running after a delegated
		// one would inherit the stale worker role and route its subagents to
		// the wrong model — exactly the baseline the comparison needs clean.
		if mode == RunModeCoder || spec.WorkerModel == "" {
			cfg.SetRole(configuration.RoleWorker, configuration.RoleConfig{})
			return nil
		}
		workerProvider := spec.WorkerProvider
		if workerProvider == "" {
			workerProvider = cfg.LastUsedProvider
		}
		// Only the worker role entry is written. It alone drives worker
		// routing (ResolveWorkerRole reads it), so no ProviderModels write is
		// needed — and a ProviderModels write would be harmful: with an empty
		// WorkerProvider the provider is the run's coder provider
		// (LastUsedProvider, pinned by configureRun), so writing it would
		// clobber the coder provider's model entry, silently moving the
		// escalation model and suppressing economy-mode routing.
		cfg.SetRole(configuration.RoleWorker, configuration.RoleConfig{
			Provider: workerProvider,
			Model:    spec.WorkerModel,
		})
		return nil
	})
}

// applyRunModeToAgent applies the mode to the freshly built agent: economy
// mode is a per-session setting on the agent, so it is set here rather than
// in the shared configuration.
func applyRunModeToAgent(ag *agent.Agent, spec ModelSpec) {
	if ag == nil {
		return
	}
	if spec.RunMode == RunModeEconomy {
		ag.SetEconomyMode(true)
	}
}
