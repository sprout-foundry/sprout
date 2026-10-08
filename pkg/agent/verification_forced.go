// verification_forced.go — the forced-verification seam. The turn-end
// hook gates its run on the turn's own application-code changes
// (Agent.TurnChangedApplicationPaths); that gate is a good default for an
// interactive session (a docs-only turn must not start a build), but it
// is the wrong gate for a harness that must score a run
// (pkg/benchmark's runner): a turn whose changes never reached the
// tracker's window would then be scored as "no verification", losing the
// outcome of a turn that did change the workspace.
//
// RunForcedTurnEndVerification is that seam: it runs the same trusted
// verification the hook runs — the starter manifest's commands and the
// plan's acceptance, never a model-proposed command — and stores the
// result as the turn's verification state, so
// Agent.LastVerificationResult and the per-turn metrics read it exactly
// as they read a hook-run result. The caller decides when the change
// gate is wrong; this method never consults it.

package agent

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/verify"
)

// RunForcedTurnEndVerification runs the turn-end verification without
// consulting the turn's change window, and stores the result as the
// turn's verification state. It is the escape hatch for a caller that has
// independent evidence the turn changed the workspace (the benchmark
// runner's git baseline) but whose changes never reached
// Agent.TurnChangedPaths.
//
// It shares the hook's inputs and storage: a snapshot of the starter
// manifest and the active plan is taken if the turn has none
// (getTurnVerifySnapshot), the runner's commands come only from the
// manifest and the explicit project configuration
// (verify.ConfigurationCommands), and the stored state is the same
// turnVerification the hook writes — so LastVerificationResult,
// LastTurnVerification, and the per-turn repair metrics see this result
// exactly as they see a hook-run one.
//
// Unlike the hook it runs once: it does not feed a failing check back to
// the model for repair (the turn is already over). The result is stored
// and returned.
//
// It no-ops (returns nil, nil) when verification is disabled for this
// turn's configuration, or the agent has no configuration: the forced
// seam still honors the verification switch — a benchmark run forces the
// switch on, but the seam is general and a disabled run must never verify.
// A setup error (no executor, empty root) is returned so the caller can
// record why no result was produced; nothing is stored in that case.
func (a *Agent) RunForcedTurnEndVerification(ctx context.Context) (*verify.Result, error) {
	if a == nil {
		return nil, nil
	}
	if a.configManager == nil {
		return nil, nil
	}
	cfg := a.configManager.GetConfig()
	if cfg == nil || !cfg.VerificationEnabled() {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	runner := verify.New()
	runner.ConfigCommands = verify.ConfigurationCommands(cfg)

	snap := a.getTurnVerifySnapshot()
	if snap == nil {
		snap = runner.Snapshot(a.GetWorkspaceRoot())
		a.setTurnVerifySnapshot(snap)
	}

	// The require-a-test-for-new-behavior input mirrors the hook's: the
	// mechanical input is frozen from the plan snapshot, and the changed
	// application paths come from the turn window (empty here — this
	// seam exists precisely because the window missed the changes, so a
	// new-behavior run without a test item is reported the same way the
	// hook would report it).
	runner.RequireTest = verify.RequireTestInput{
		Enabled:                 cfg.RequireTest(),
		PlanHasTestItem:         snapshotHasTestItem(snap),
		ChangedApplicationPaths: a.TurnChangedApplicationPaths(),
	}

	res, err := runner.RunSnapshot(ctx, a.GetWorkspaceRoot(), snap)
	if err != nil {
		return nil, fmt.Errorf("forced turn-end verification: %w", err)
	}

	limit := cfg.VerificationRepairAttempts()
	a.setTurnVerification(turnVerification{
		result: res,
		limit:  limit,
	})
	if res.Passed() {
		a.CaptureVerificationCheckpoint(res)
	}
	return res, nil
}
