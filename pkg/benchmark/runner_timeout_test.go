//go:build !js

// runner_timeout_test.go — the per-run timeout: a run whose turn hangs
// past the runner's Timeout is stopped through the agent's real interrupt
// (the same cancel the CLI's Ctrl+C path fires) and recorded as a failed
// run wrapping ErrRunTimeout, while fast runs and their neighbours are
// untouched. Also pinned: the timeout default and its resolution, and the
// runner returning (never hanging) after a stop.
//
// The harness reuses runner_test.go's fixtures (benchTask, the scripted
// factory, shapeManifest) and simulates the hung turn with the scripted
// client's response Delay — the same seam the CLI's interrupt dance relies
// on, because the delay's wait derives from the agent's interrupt context
// and aborts the moment TriggerInterrupt fires. What is covered: the
// timer-per-run behavior, the stop, the failure record, and the runner
// returning. What is not covered by the delay seam: a turn wedged inside
// something the interrupt cancel cannot reach (the bounded secondary wait's
// expiry path) — that path is exercised only at the unit level via the
// grace constants' existence, not end to end.
package benchmark

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// hungFactory builds an AgentFactory whose every run hangs: the scripted
// model's first response carries a Delay far longer than the test's
// timeout, so the turn sits in the provider call until the interrupt
// fires (the delay aborts on the agent's interrupt context).
func hungFactory(t *testing.T, mgr *configuration.Manager, hang time.Duration) AgentFactory {
	t.Helper()
	return func(runDir string, spec ModelSpec) (*agent.Agent, error) {
		client := agent.NewScriptedClient(&agent.ScriptedResponse{
			Content:      "Working on it...",
			FinishReason: "stop",
			Delay:        hang,
		})
		if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.ContextMode = configuration.ContextModeFull
			cfg.SkipPrompt = true
			return nil
		}); err != nil {
			return nil, err
		}
		ag, err := agent.NewAgentWithClient(client, "test", mgr)
		if err != nil {
			return nil, err
		}
		ag.SetWorkspaceRoot(runDir)
		return ag, nil
	}
}

// fastFactory builds an AgentFactory whose turns complete immediately (a
// plain stop response, no write — the change gate stays closed and the run
// records as fail, which is fine: these tests pin timing and the timeout,
// not pass/fail).
func fastFactory(t *testing.T, mgr *configuration.Manager) AgentFactory {
	t.Helper()
	return func(runDir string, spec ModelSpec) (*agent.Agent, error) {
		client := agent.NewScriptedClient(agent.NewStopResponse("Done immediately."))
		if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.ContextMode = configuration.ContextModeFull
			cfg.SkipPrompt = true
			return nil
		}); err != nil {
			return nil, err
		}
		ag, err := agent.NewAgentWithClient(client, "test", mgr)
		if err != nil {
			return nil, err
		}
		ag.SetWorkspaceRoot(runDir)
		return ag, nil
	}
}

// TestRunnerTimeout_StopsHungTurnAndRecordsFailed pins the core contract: a
// run whose turn hangs past the timeout is stopped (the runner returns —
// it must not hang), recorded with Run.Err wrapping ErrRunTimeout, and
// never Passed — even though the scripted model's reply claims it kept
// working. The stop must arrive near the timeout, not near the hang
// duration.
func TestRunnerTimeout_StopsHungTurnAndRecordsFailed(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	const hang = 2 * time.Minute
	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory:  hungFactory(t, mgr, hang),
		Timeout:       250 * time.Millisecond,
		RunsPerTask:   1,
	}

	started := time.Now()
	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "bench-model"})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("RunTask: %v (a timed-out run is a failed run, not a suite error)", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if elapsed >= hang/2 {
		t.Errorf("RunTask took %v; want near the 250ms timeout, not the %v hang (the turn was not stopped)", elapsed, hang)
	}

	run := runs[0]
	if run.Passed {
		t.Error("Passed = true, want false (a timed-out run is never Passed)")
	}
	if run.Err == nil {
		t.Fatal("Err = nil, want the timeout error")
	}
	if !errors.Is(run.Err, ErrRunTimeout) {
		t.Errorf("Err = %v, want it to wrap ErrRunTimeout (callers detect the timeout with errors.Is)", run.Err)
	}
	if !strings.Contains(run.Err.Error(), "timed out") {
		t.Errorf("Err = %v, want a message that explains the timeout", run.Err)
	}
	// The run records WHY it has no verification result: a timed-out turn
	// is the runner's own knowledge and gets its own reason, not a silent
	// fail.
	if run.Result != nil {
		t.Errorf("Result = %+v, want nil (a timed-out turn stored no verification result)", run.Result)
	}
	if run.NotVerifiedReason != "verify timed out" {
		t.Errorf("NotVerifiedReason = %q, want %q", run.NotVerifiedReason, "verify timed out")
	}
	assertWallTime(t, run)
}

// TestRunnerTimeout_DoesNotFireOnFastTurn pins the no-false-positive side:
// a turn that finishes well inside the timeout completes normally, with no
// timeout error, and the runner's own wall time shows the timer did not
// add its stop path.
func TestRunnerTimeout_DoesNotFireOnFastTurn(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory:  fastFactory(t, mgr),
		Timeout:       30 * time.Second,
		RunsPerTask:   2,
	}

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "bench-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(runs))
	}
	for i, run := range runs {
		if run.Err != nil {
			t.Errorf("runs[%d].Err = %v, want nil (the turn finished inside the timeout)", i, run.Err)
		}
		if errors.Is(run.Err, ErrRunTimeout) {
			t.Errorf("runs[%d].Err wraps ErrRunTimeout — a false positive", i)
		}
		assertWallTime(t, run)
	}
}

// TestRunnerTimeout_DefaultWhenUnset pins the resolution: the zero value
// and negative values resolve to the default (documented as 10 minutes),
// a positive value stands, and a nil runner resolves to the default too.
func TestRunnerTimeout_DefaultWhenUnset(t *testing.T) {
	if got := defaultTaskTimeout; got != 10*time.Minute {
		t.Errorf("defaultTaskTimeout = %v, want 10m (the documented default)", got)
	}
	if got := (&Runner{}).timeoutDuration(); got != defaultTaskTimeout {
		t.Errorf("(&Runner{}).timeoutDuration() = %v, want the default", got)
	}
	if got := (&Runner{Timeout: -1 * time.Second}).timeoutDuration(); got != defaultTaskTimeout {
		t.Errorf("Timeout=-1s → timeoutDuration() = %v, want the default (negative resolves to it)", got)
	}
	if got := (&Runner{Timeout: 90 * time.Second}).timeoutDuration(); got != 90*time.Second {
		t.Errorf("Timeout=90s → timeoutDuration() = %v, want 90s (a positive value stands)", got)
	}
	var nilRunner *Runner
	if got := nilRunner.timeoutDuration(); got != defaultTaskTimeout {
		t.Errorf("nil runner timeoutDuration() = %v, want the default", got)
	}
}

// TestRunnerTimeout_PerRunTimersDoNotAbortNeighbours pins that each run
// carries its own timer: a suite where the first run hangs stops that run
// alone and the second run still executes to its own completion.
func TestRunnerTimeout_PerRunTimersDoNotAbortNeighbours(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	builds := 0
	factory := func(runDir string, spec ModelSpec) (*agent.Agent, error) {
		builds++
		if builds == 1 {
			return hungFactory(t, mgr, 2*time.Minute)(runDir, spec)
		}
		return fastFactory(t, mgr)(runDir, spec)
	}

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory:  factory,
		Timeout:       250 * time.Millisecond,
		RunsPerTask:   2,
	}

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "bench-model"})
	if err != nil {
		t.Fatalf("RunTask: %v (a timed-out run never aborts the task)", err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want 2 (the timeout stops run 1, run 2 still runs)", len(runs))
	}
	if !errors.Is(runs[0].Err, ErrRunTimeout) {
		t.Errorf("runs[0].Err = %v, want the timeout error (only the hung run fails)", runs[0].Err)
	}
	if runs[0].Passed {
		t.Error("runs[0].Passed = true, want false (timed out)")
	}
	if runs[1].Err != nil {
		t.Errorf("runs[1].Err = %v, want nil (the second run's own timer never fired — per-run timers)", runs[1].Err)
	}
}

// TestRunnerTimeout_SuiteContinuesPastTimedOutModel pins the suite level:
// a model whose every run hangs is recorded as timed-out failures and the
// next model's runs still complete (record-and-continue holds at the
// timeout boundary).
func TestRunnerTimeout_SuiteContinuesPastTimedOutModel(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory: func(runDir string, spec ModelSpec) (*agent.Agent, error) {
			if spec.Model == "slow" {
				return hungFactory(t, mgr, 2*time.Minute)(runDir, spec)
			}
			return fastFactory(t, mgr)(runDir, spec)
		},
		Timeout:     250 * time.Millisecond,
		RunsPerTask: 1,
		Models: []ModelSpec{
			{Model: "slow", Provider: "prov-a"},
			{Model: "quick", Provider: "prov-b"},
		},
	}

	runs, err := runner.RunSuite(context.Background(), []*Task{benchTask()})
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want 2 (one per model)", len(runs))
	}
	if !errors.Is(runs[0].Err, ErrRunTimeout) {
		t.Errorf("runs[0].Err = %v, want the timeout error (model slow hung)", runs[0].Err)
	}
	if runs[1].Model != "quick" || runs[1].Err != nil {
		t.Errorf("runs[1] = model %q err %v, want model quick completing cleanly (the suite moved on)", runs[1].Model, runs[1].Err)
	}

	// The report reads the timed-out runs as failed with the benchmark's
	// "error" category (a run-level failure is the plumbing's, not the
	// agent's work).
	rep := BuildReport(runs, runner.SuiteModels(), Meta{RunDate: "2026-10-06", Version: "v0.0.0-test"})
	if len(rep.Starters) != 2 {
		t.Fatalf("Starters = %d, want 2", len(rep.Starters))
	}
	if rep.Starters[0].Passed != 0 {
		t.Errorf("Starters[0].Passed = %d, want 0 (every run of the hung model timed out)", rep.Starters[0].Passed)
	}
	if rep.FailureCategories == nil || rep.FailureCategories["error"] != 1 {
		t.Errorf("FailureCategories = %v, want {error: 1} (the timed-out run is a run-level failure)", rep.FailureCategories)
	}
}
