// runner_config.go — the runner's per-run configuration and the agent
// factory it builds from: the narrow config writes that force the run's
// measurement inputs (verification and change tracking on, the spec's
// model/provider in place), the default production agent factory, and the
// runs-per-model normalization. Split from runner.go to keep that file
// under the repo's per-file line budget.
package benchmark

import (
	"errors"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// configureRun applies the run's requirements to the runner's
// ConfigManager: verification forced on — the
// benchmark's pass/fail source must run for every run — change tracking
// forced on (both the subsystem switch and the per-shell_command walk),
// and the spec's model/provider where set. The manager's other
// verification settings (the repair-attempt limit, the explicit
// build/test commands) are preserved, and an empty spec field leaves the
// manager's current value: the spec narrows, it never clears. A nil
// ConfigManager skips the step (a custom AgentFactory owns the run's
// configuration entirely).
//
// Change tracking is forced on for the same reason verification is: the
// turn-end hook gates its run on the turn's own application-code changes
// (Agent.TurnChangedApplicationPaths), and that window is fed by the
// tracker. A user configuration that disabled change tracking (or the
// shell walk) would close the gate for every run and silently score
// every task as "no code changes" — Result nil, never a verdict. The
// switch is a measurement input for a benchmark, not a user preference,
// so the run sets it regardless of what the configuration said.
func (r *Runner) configureRun(spec ModelSpec) error {
	if r.ConfigManager == nil {
		return nil
	}
	return r.ConfigManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.Verification == nil {
			cfg.Verification = &configuration.VerificationConfig{}
		}
		cfg.Verification.Enabled = true
		if cfg.ChangeTracking == nil {
			cfg.ChangeTracking = &configuration.ChangeTrackingConfig{}
		}
		enabled := true
		cfg.ChangeTracking.Enabled = &enabled
		cfg.ChangeTracking.ShellWalkEnabled = &enabled
		if spec.Provider != "" {
			cfg.LastUsedProvider = spec.Provider
		}
		if spec.Model != "" {
			provider := spec.Provider
			if provider == "" {
				provider = cfg.LastUsedProvider
			}
			if provider != "" {
				cfg.SetModelForProvider(provider, spec.Model)
			}
		}
		return nil
	})
}

// defaultAgentFactory builds the headless agent for a run from the
// runner's ConfigManager (the run is already configured: verification
// enabled, the spec's model/provider in place). It mirrors the production
// SDK path (cmd/wasm): a real provider client from pkg/factory plus
// agent.NewAgentWithClient with the runner's config manager — so the
// agent sees exactly the run's configuration — with the workspace root
// pinned to the run's fresh copy.
func (r *Runner) defaultAgentFactory(runDir string, spec ModelSpec) (*agent.Agent, error) {
	if r.ConfigManager == nil {
		return nil, errors.New("benchmark: the default agent factory needs a ConfigManager to target the run's model and provider (set Runner.ConfigManager or supply an AgentFactory)")
	}
	provider, err := r.ConfigManager.GetProvider()
	if err != nil {
		return nil, fmt.Errorf("benchmark: resolve the run's provider: %w (set ModelSpec.Provider or select a provider in the run's configuration)", err)
	}
	model := spec.Model
	if model == "" {
		model = r.ConfigManager.GetModelForProvider(provider)
	}
	client, err := factory.CreateProviderClient(provider, model)
	if err != nil {
		return nil, fmt.Errorf("benchmark: create client for provider %q model %q: %w", provider, model, err)
	}
	ag, err := agent.NewAgentWithClient(client, provider, r.ConfigManager)
	if err != nil {
		return nil, fmt.Errorf("benchmark: build the headless agent: %w", err)
	}
	ag.SetWorkspaceRoot(runDir)
	return ag, nil
}

// runsPerTask is the effective runs-per-model count:
// RunsPerTask where positive, defaultRunsPerTask (3) otherwise.
func (r *Runner) runsPerTask() int {
	if r != nil && r.RunsPerTask > 0 {
		return r.RunsPerTask
	}
	return defaultRunsPerTask
}
