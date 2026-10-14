// delegation_test.go — the delegation axis: run-mode config application,
// the pure outcome→metrics fold, and the report's by-mode aggregation.
// Mock-provider only: no network, no real model.

package benchmark

import (
	"context"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestDelegationMetricsFromOutcomes(t *testing.T) {
	tests := []struct {
		name     string
		outcomes []agent.DelegatedTaskOutcome
		want     DelegationMetrics
	}{
		{
			name:     "no outcomes is zero",
			outcomes: nil,
			want:     DelegationMetrics{},
		},
		{
			name: "verified worker task counts toward the worker share",
			outcomes: []agent.DelegatedTaskOutcome{
				{Role: configuration.RoleWorker, Outcome: agent.OutcomeVerified, Tokens: 100},
			},
			want: DelegationMetrics{
				TaskCount: 1, Verified: 1, WorkerTokens: 100, TotalTokens: 100,
				WorkerTokenShare: 1,
			},
		},
		{
			name: "escalated task counts toward the escalation rate, not the worker share",
			outcomes: []agent.DelegatedTaskOutcome{
				{Role: configuration.RoleWorker, Outcome: agent.OutcomeVerified, Tokens: 100},
				{Role: configuration.RoleCoder, Outcome: agent.OutcomeEscalated, Tokens: 300},
			},
			want: DelegationMetrics{
				TaskCount: 2, Verified: 1, Escalated: 1,
				WorkerTokens: 100, TotalTokens: 400,
				EscalationRate:   0.5,
				WorkerTokenShare: 0.25,
			},
		},
		{
			name: "failed task counts as failed",
			outcomes: []agent.DelegatedTaskOutcome{
				{Role: configuration.RoleCoder, Outcome: agent.OutcomeFailed, Tokens: 50},
			},
			want: DelegationMetrics{TaskCount: 1, Failed: 1, TotalTokens: 50},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := delegationMetricsFromOutcomes(tt.outcomes)
			if got != tt.want {
				t.Errorf("delegationMetricsFromOutcomes() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestApplyRunMode_CoderIsNoOp pins that the default mode writes nothing.
func TestApplyRunMode_CoderIsNoOp(t *testing.T) {
	mgr, err := configuration.NewManagerWithDir(t.TempDir() + "/.sprout")
	if err != nil {
		t.Fatal(err)
	}
	r := &Runner{ConfigManager: mgr}
	if err := r.applyRunMode(ModelSpec{RunMode: RunModeCoder}); err != nil {
		t.Fatalf("applyRunMode(coder): %v", err)
	}
	if got := mgr.GetConfig().GetRole(configuration.RoleWorker); got.Provider != "" || got.Model != "" {
		t.Errorf("coder mode must not configure the worker role, got %+v", got)
	}
}

// TestApplyRunMode_DelegatedConfiguresWorkerRole pins that the delegated
// mode points the worker role at the spec's worker model.
func TestApplyRunMode_DelegatedConfiguresWorkerRole(t *testing.T) {
	mgr, err := configuration.NewManagerWithDir(t.TempDir() + "/.sprout")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.LastUsedProvider = "openrouter"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r := &Runner{ConfigManager: mgr}
	if err := r.applyRunMode(ModelSpec{RunMode: RunModeDelegated, WorkerModel: "cheap-model"}); err != nil {
		t.Fatalf("applyRunMode(delegated): %v", err)
	}
	got := mgr.GetConfig().GetRole(configuration.RoleWorker)
	if got.Model != "cheap-model" || got.Provider != "openrouter" {
		t.Errorf("worker role = %+v, want openrouter/cheap-model", got)
	}
}

// TestApplyRunModeToAgent_EconomyTurnsModeOn pins that the economy mode is a
// per-session agent setting.
func TestApplyRunModeToAgent_EconomyTurnsModeOn(t *testing.T) {
	ag := agent.NewTestAgent()
	applyRunModeToAgent(ag, ModelSpec{RunMode: RunModeEconomy})
	if !ag.GetEconomyMode() {
		t.Error("RunModeEconomy must turn economy mode on for the agent")
	}

	other := agent.NewTestAgent()
	applyRunModeToAgent(other, ModelSpec{RunMode: RunModeDelegated})
	if other.GetEconomyMode() {
		t.Error("RunModeDelegated must not turn economy mode on")
	}
}

// TestRunner_CapturesRunModeAndDelegation pins the runner's end-to-end
// capture (mock provider, no network): a full task run records its run mode
// on the Run, and the delegation metrics are folded from the agent's
// delegated-task records. A coder-only run records the mode and zero
// delegation; the fold itself is unit-tested above with non-zero outcomes.
func TestRunner_CapturesRunModeAndDelegation(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory:  scriptedFactory(t, mgr, true, "Done."),
		ShapeCopy:     shapeManifest("echo fixture-bench-ok"),
		RunsPerTask:   1,
	}

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{
		Model:   "bench-model",
		RunMode: RunModeCoder,
	})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if runs[0].RunMode != RunModeCoder {
		t.Errorf("Run.RunMode = %q, want %q", runs[0].RunMode, RunModeCoder)
	}
	// A coder-only run delegates nothing, so the fold is the zero value.
	if runs[0].Delegation != (DelegationMetrics{}) {
		t.Errorf("coder-only Run.Delegation = %+v, want the zero value", runs[0].Delegation)
	}
}

// TestRunner_CapturesEconomyMode pins that an economy-mode run records the
// mode and turns economy mode on for its agent (mock provider).
func TestRunner_CapturesEconomyMode(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	var sawEconomy bool
	base := scriptedFactory(t, mgr, true, "Done.")
	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory: func(runDir string, spec ModelSpec) (*agent.Agent, error) {
			ag, err := base(runDir, spec)
			if err != nil {
				return nil, err
			}
			applyRunModeToAgent(ag, spec)
			sawEconomy = ag.GetEconomyMode()
			return ag, nil
		},
		ShapeCopy:   shapeManifest("echo fixture-bench-ok"),
		RunsPerTask: 1,
	}

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{
		Model:          "bench-model",
		RunMode:        RunModeEconomy,
		WorkerModel:    "cheap-model",
		WorkerProvider: "openrouter",
	})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if runs[0].RunMode != RunModeEconomy {
		t.Errorf("Run.RunMode = %q, want %q", runs[0].RunMode, RunModeEconomy)
	}
	if !sawEconomy {
		t.Error("an economy-mode run must turn economy mode on for its agent")
	}
}

// TestDelegationReports_GroupsByMode pins the report aggregation: runs pool
// by mode in a fixed order, with the pooled pass and escalation rates.
func TestDelegationReports_GroupsByMode(t *testing.T) {
	runs := []Run{
		{RunMode: RunModeCoder, Passed: true, Tokens: 100, Cost: 0.01},
		{RunMode: RunModeCoder, Passed: false, Tokens: 200, Cost: 0.02},
		{RunMode: RunModeDelegated, Passed: true, Tokens: 150, Cost: 0.005,
			Delegation: DelegationMetrics{TaskCount: 2, Escalated: 1}},
	}
	got := delegationReports(runs)
	if len(got) != 2 {
		t.Fatalf("got %d mode rows, want 2: %+v", len(got), got)
	}
	if got[0].RunMode != RunModeCoder || got[0].Runs != 2 || got[0].Passed != 1 {
		t.Errorf("coder row = %+v, want 2 runs / 1 passed", got[0])
	}
	if got[0].PassRate != 0.5 {
		t.Errorf("coder pass rate = %v, want 0.5", got[0].PassRate)
	}
	if got[1].RunMode != RunModeDelegated || got[1].DelegatedTasks != 2 || got[1].Escalated != 1 {
		t.Errorf("delegated row = %+v, want 2 tasks / 1 escalated", got[1])
	}
	if got[1].EscalationRate != 0.5 {
		t.Errorf("delegated escalation rate = %v, want 0.5", got[1].EscalationRate)
	}
	if got[1].Tokens != 150 {
		t.Errorf("delegated tokens = %d, want 150", got[1].Tokens)
	}
}

// TestApplyRunMode_WorkerModelWithoutModeRejected pins the guard: a worker
// model with no run mode is a mistake (the run would silently be a coder
// baseline), so it is rejected rather than ignored.
func TestApplyRunMode_WorkerModelWithoutModeRejected(t *testing.T) {
	mgr, err := configuration.NewManagerWithDir(t.TempDir() + "/.sprout")
	if err != nil {
		t.Fatal(err)
	}
	r := &Runner{ConfigManager: mgr}
	if err := r.applyRunMode(ModelSpec{WorkerModel: "cheap-model"}); err == nil {
		t.Fatal("WorkerModel without RunMode must be rejected, not silently ignored")
	}
}

// TestApplyRunMode_CoderClearsStaleWorkerRole pins the shared-manager
// contamination guard: a coder-mode spec must clear a worker role left by a
// previous delegated spec, so the coder baseline does not silently route
// subagents to the stale worker model.
func TestApplyRunMode_CoderClearsStaleWorkerRole(t *testing.T) {
	mgr, err := configuration.NewManagerWithDir(t.TempDir() + "/.sprout")
	if err != nil {
		t.Fatal(err)
	}
	r := &Runner{ConfigManager: mgr}

	// A delegated spec installs the worker role.
	if err := r.applyRunMode(ModelSpec{RunMode: RunModeDelegated, WorkerModel: "cheap-model", WorkerProvider: "zai"}); err != nil {
		t.Fatal(err)
	}
	if got := mgr.GetConfig().GetRole(configuration.RoleWorker); got.Model != "cheap-model" {
		t.Fatalf("precondition: worker role = %+v, want cheap-model", got)
	}

	// A following coder spec must clear it.
	if err := r.applyRunMode(ModelSpec{RunMode: RunModeCoder}); err != nil {
		t.Fatal(err)
	}
	if got := mgr.GetConfig().GetRole(configuration.RoleWorker); got.Provider != "" || got.Model != "" {
		t.Errorf("coder spec left a stale worker role: %+v", got)
	}
}

// TestDelegationReports_UnknownModeGetsItsOwnRow pins that a run mode
// outside the known set is still accounted for, so the delegation section
// sums to the total runs.
func TestDelegationReports_UnknownModeGetsItsOwnRow(t *testing.T) {
	got := delegationReports([]Run{
		{RunMode: RunModeCoder, Passed: true},
		{RunMode: "custom-mode", Passed: true},
	})
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 (coder + custom-mode): %+v", len(got), got)
	}
	if got[1].RunMode != "custom-mode" || got[1].Runs != 1 {
		t.Errorf("unknown-mode row = %+v, want 1 run of custom-mode", got[1])
	}
}

// TestApplyRunMode_DoesNotMoveActiveProvider pins that routing the worker
// model leaves the run's active provider (LastUsedProvider) on the coder
// provider, so the main-loop client is built on the coder provider and only
// subagents route to the worker.
func TestApplyRunMode_DoesNotMoveActiveProvider(t *testing.T) {
	mgr, err := configuration.NewManagerWithDir(t.TempDir() + "/.sprout")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.LastUsedProvider = "openrouter"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r := &Runner{ConfigManager: mgr}
	if err := r.applyRunMode(ModelSpec{
		RunMode:        RunModeDelegated,
		WorkerModel:    "cheap-model",
		WorkerProvider: "zai",
	}); err != nil {
		t.Fatalf("applyRunMode(delegated): %v", err)
	}
	if got := mgr.GetConfig().LastUsedProvider; got != "openrouter" {
		t.Errorf("LastUsedProvider = %q, want the coder provider %q (worker routing must not move it)", got, "openrouter")
	}
	if got := mgr.GetConfig().GetRole(configuration.RoleWorker); got.Provider != "zai" || got.Model != "cheap-model" {
		t.Errorf("worker role = %+v, want zai/cheap-model", got)
	}
}

// TestApplyRunMode_EmptyWorkerProviderKeepsCoderModel pins the clobber
// regression: with an empty WorkerProvider the worker provider is the run's
// coder provider, and applyRunMode must NOT overwrite the coder provider's
// model entry (that would move the escalation model and suppress economy
// routing). The run's coder model must survive unchanged.
func TestApplyRunMode_EmptyWorkerProviderKeepsCoderModel(t *testing.T) {
	mgr, err := configuration.NewManagerWithDir(t.TempDir() + "/.sprout")
	if err != nil {
		t.Fatal(err)
	}
	// Mirror the state configureRun leaves: the run's provider + its model.
	if err := mgr.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.LastUsedProvider = "openrouter"
		c.ProviderModels = map[string]string{"openrouter": "coder-model"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r := &Runner{ConfigManager: mgr}
	if err := r.applyRunMode(ModelSpec{
		RunMode:     RunModeDelegated,
		WorkerModel: "cheap-model",
		// WorkerProvider empty → falls back to the run's provider.
	}); err != nil {
		t.Fatalf("applyRunMode(delegated): %v", err)
	}
	if got := mgr.GetConfig().GetModelForProvider("openrouter"); got != "coder-model" {
		t.Errorf("coder provider model = %q, want coder-model (worker routing must not clobber it)", got)
	}
	if got := mgr.GetConfig().GetRole(configuration.RoleWorker); got.Provider != "openrouter" || got.Model != "cheap-model" {
		t.Errorf("worker role = %+v, want openrouter/cheap-model", got)
	}
}

// TestDelegationReports_EmptyModeDefaultsToCoder pins that a zero-value
// RunMode aggregates as coder.
func TestDelegationReports_EmptyModeDefaultsToCoder(t *testing.T) {
	got := delegationReports([]Run{{Passed: true}})
	if len(got) != 1 || got[0].RunMode != RunModeCoder {
		t.Errorf("empty RunMode must aggregate as coder, got %+v", got)
	}
}
