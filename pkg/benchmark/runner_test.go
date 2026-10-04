//go:build !js

// runner_test.go — the SP-154 §154a/§154b runner acceptance tests: the
// pass/fail contract (the recorded pass/fail comes only from the SP-149
// verification result — never from the model's own reply), the fresh
// starter copy per run, the 3-runs-per-model default, and the
// record-and-continue behavior (one bad setup never aborts the suite).
//
// The harness mirrors the SP-149 fixture tests (pkg/agent/
// verification_hook_test.go): a scripted model, a real workspace copy
// (the embedded fixture starter), and real shell execution of the
// deterministic build command that ShapeCopy writes into the copy's
// .sprout/starter.json. The copy's manifest wins over the configuration
// in the verify runner's command resolution (SP-149 §149b), so the run's
// only check is `sh -c <build>` — the tests never depend on npm, a dev
// server, or a browser. The fixture's page-check fixture task is NOT the
// test's task: an in-memory build-only plan keeps the verification run to
// that single shell command. Every shell-dependent test skips (rather
// than fails) where sh is absent, mirroring shAvailable in pkg/verify.

package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/startermanifest"
)

// ---------------------------------------------------------------------------
// Fixtures and helpers
// ---------------------------------------------------------------------------

// shAvailable mirrors pkg/verify's shAvailable: the deterministic build
// command runs through the default executor (sh -c), so the tests skip
// where sh does not exist (non-UNIX dev machines).
func shAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh available on this platform (the fixture build command runs through sh -c)")
	}
}

// benchPlan returns a valid SP-148 plan with a single build acceptance
// item (SP-154 §154a: acceptance criteria inside the plan). The
// build-only shape is deliberate: the verification run executes exactly
// one shell command (the one ShapeCopy pins into the copy's manifest)
// and nothing else — no dev server, no browser, no npm.
func benchPlan() plancontract.Plan {
	plan := plancontract.New("Make the bench change and keep its build green.", time.Now())
	plan.Starter = "fixture"
	plan.Scope = []plancontract.ScopeItem{
		{ID: "bench", Title: "Bench change"},
	}
	plan.Steps = []plancontract.Step{
		{Scope: "bench", Description: "Make the bench change."},
	}
	plan.Acceptance = []plancontract.Acceptance{
		{ID: "build-passes", Scope: "bench", Check: "npm run build", Kind: plancontract.KindBuild},
	}
	if err := plancontract.Validate(plan); err != nil {
		panic(fmt.Sprintf("benchPlan fixture is not a valid plan: %v", err))
	}
	return *plan
}

// benchTask is the in-memory task the runner tests run. It is not the
// committed fixture (benchmarks/tasks/fixture/add-version-badge.json):
// that task's page acceptance item would need a dev server and a
// browser, and the runner tests keep the verification run to a single
// deterministic shell command.
func benchTask() *Task {
	return &Task{
		ID:      "bench-test",
		Request: "Make the bench change.",
		Starter: "fixture",
		Plan:    benchPlan(),
	}
}

// shapeManifest returns the ShapeCopy that overwrites the copy's
// .sprout/starter.json with the given deterministic build command and no
// test command. The fixture starter's manifest carries npm commands
// (not runnable deterministically under go test), and the verify runner
// gives the manifest's commands precedence over the configuration
// (SP-149 §149b) — so pinning the manifest is what makes the run's only
// check a plain sh command.
func shapeManifest(build string) func(runDir string, task *Task) error {
	return func(runDir string, task *Task) error {
		manifest := startermanifest.StarterManifest{
			Starter: startermanifest.StarterRef{ID: "fixture", Version: "0.1.0"},
			Build:   build,
		}
		data, err := json.MarshalIndent(&manifest, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(
			filepath.Join(runDir, startermanifest.SproutDir, startermanifest.StarterJSONName),
			data, 0o644,
		)
	}
}

// scriptedTurnResponses builds the scripted responses for one benchmark
// turn: an optional write_file tool call (the model "does the work" — it
// opens the turn-end verification hook's change gate) followed by the
// model's own claims, one stop response each: the final answer, and one
// repair round where the verification loop feeds a failing check back.
func scriptedTurnResponses(runDir string, withWrite bool, claims ...string) []*agent.ScriptedResponse {
	responses := make([]*agent.ScriptedResponse, 0, 1+len(claims))
	if withWrite {
		args := fmt.Sprintf(`{"path":%q,"content":"function bench() { return 1; }"}`,
			filepath.Join(runDir, "src", "bench.js"))
		responses = append(responses, agent.NewToolCallResponse("write_file", args))
	}
	for _, claim := range claims {
		responses = append(responses, agent.NewStopResponse(claim))
	}
	return responses
}

// scriptedFactory builds the test AgentFactory (the "scripted model"):
// each run gets a fresh scripted client (the factory is called once per
// run, and the write path is the run-specific copy, so the script is
// built inside the factory). The run's shared test manager — the
// runner's ConfigManager, already carrying the runner's per-run
// configuration (verification enabled, the spec's model/provider) — is
// refined here with the harness settings: full context mode (the file
// tools stay on the roster), SkipPrompt (stdin is closed under go
// test), and verification with N=1 repair attempts, so the failing
// case's script (initial run + one repair round) is exactly sized for
// the stopping rule.
func scriptedFactory(t *testing.T, mgr *configuration.Manager, withWrite bool, claims ...string) AgentFactory {
	t.Helper()
	return func(runDir string, spec ModelSpec) (*agent.Agent, error) {
		client := agent.NewScriptedClient(scriptedTurnResponses(runDir, withWrite, claims...)...)
		if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.ContextMode = configuration.ContextModeFull
			cfg.SkipPrompt = true
			cfg.Verification = &configuration.VerificationConfig{Enabled: true, RepairAttempts: 1}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("harness: configure run config: %w", err)
		}
		ag, err := agent.NewAgentWithClient(client, api.TestClientType, mgr)
		if err != nil {
			return nil, fmt.Errorf("harness: build scripted agent: %w", err)
		}
		ag.SetMaxIterations(10)
		ag.SetWorkspaceRoot(runDir)
		return ag, nil
	}
}

// ---------------------------------------------------------------------------
// The pass/fail contract (SP-154 §154a/§154b)
// ---------------------------------------------------------------------------

// TestRunner_ClaimsSuccessFailingCheckRecordsFail pins the item's
// acceptance test: a scripted model claims success in its reply but the
// verification check fails → the run is recorded as fail. The reply text
// is never read for scoring: the recorded Result (the failing build
// check) is the proof.
func TestRunner_ClaimsSuccessFailingCheckRecordsFail(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory: scriptedFactory(t, mgr, true,
			"Done! The change is complete and all checks pass.",
			"I tried to fix the build, but it still fails."),
		ShapeCopy:   shapeManifest("echo fixture-bench-fail; exit 1"),
		RunsPerTask: 1,
	}

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "bench-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	run := runs[0]
	if run.Passed {
		t.Error("Passed = true, want false: the model claimed success but the build check failed")
	}
	if run.Result == nil {
		t.Fatal("Result = nil, want the failing verification run")
	}
	if !run.Result.Failed() {
		t.Errorf("Result.Failed() = false, want true (the build check failed)")
	}
	if run.Err != nil {
		t.Errorf("Err = %v, want nil (the turn completed cleanly; the failure is the check's, not the run's)", run.Err)
	}
	if run.TaskID != "bench-test" || run.Starter != "fixture" || run.Model != "bench-model" || run.RunNumber != 1 {
		t.Errorf("record = task %q / starter %q / model %q / run %d, want bench-test / fixture / bench-model / 1",
			run.TaskID, run.Starter, run.Model, run.RunNumber)
	}
	if len(run.Result.Checks) != 1 || run.Result.Checks[0].Kind != plancontract.KindBuild || run.Result.Checks[0].Passed {
		t.Errorf("result checks = %+v, want the single failing build check", run.Result.Checks)
	}
	if run.StartedAt.IsZero() || run.FinishedAt.Before(run.StartedAt) {
		t.Errorf("wall-time anchors = started %v finished %v, want a non-zero started and finished >= started",
			run.StartedAt, run.FinishedAt)
	}
}

// TestRunner_ClaimsFailurePassingCheckRecordsPass pins the mirror image:
// the scripted model claims failure but the check passes → the run is
// recorded as pass. The reply's failure claim is provably ignored.
func TestRunner_ClaimsFailurePassingCheckRecordsPass(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory: scriptedFactory(t, mgr, true,
			"I'm sorry, I could not complete the task."),
		ShapeCopy:   shapeManifest("echo fixture-bench-ok"),
		RunsPerTask: 1,
	}

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "bench-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	run := runs[0]
	if !run.Passed {
		t.Error("Passed = false, want true: the model claimed failure but the build check passed")
	}
	if run.Result == nil || !run.Result.Passed() {
		t.Fatalf("Result = %+v, want a passing verification run", run.Result)
	}
	if run.Err != nil {
		t.Errorf("Err = %v, want nil (the turn completed cleanly)", run.Err)
	}
}

// ---------------------------------------------------------------------------
// Fresh copies and the 3-runs-per-model default (SP-154 §154b)
// ---------------------------------------------------------------------------

// TestRunner_ThreeRunsPerModelInFreshCopies pins the run count and the
// per-run isolation: three runs, RunNumbers 1..3, all passing, each in a
// DISTINCT fresh directory that already carried the instantiated starter
// tree and the frozen plan when the copy hook ran.
func TestRunner_ThreeRunsPerModelInFreshCopies(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	var dirs []string
	shape := func(runDir string, task *Task) error {
		if _, err := os.Stat(filepath.Join(runDir, "index.html")); err != nil {
			t.Errorf("starter tree missing in copy %s: %v", runDir, err)
		}
		if _, err := os.Stat(filepath.Join(runDir, ".sprout", "plan.json")); err != nil {
			t.Errorf("frozen plan missing in copy %s: %v", runDir, err)
		}
		dirs = append(dirs, runDir)
		return shapeManifest("echo fixture-bench-ok")(runDir, task)
	}

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory:  scriptedFactory(t, mgr, true, "Done!"),
		ShapeCopy:     shape,
		RunsPerTask:   3,
	}

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "bench-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want 3 (SP-154 §154b: 3 runs per model)", len(runs))
	}
	for i, run := range runs {
		if run.RunNumber != i+1 {
			t.Errorf("runs[%d].RunNumber = %d, want %d", i, run.RunNumber, i+1)
		}
		if !run.Passed {
			t.Errorf("runs[%d].Passed = false, want true (the build command passes)", i)
		}
	}

	if len(dirs) != 3 {
		t.Fatalf("ShapeCopy saw %d copies, want 3 (one fresh copy per run)", len(dirs))
	}
	seen := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		if seen[d] {
			t.Fatalf("two runs shared the starter copy %s (each run must get a fresh copy)", d)
		}
		seen[d] = true
	}
}

// ---------------------------------------------------------------------------
// Record-and-continue: one bad run never aborts the suite
// ---------------------------------------------------------------------------

// TestRunner_AgentFactoryErrorRecordsFailAndContinues pins the agent-
// error path: a run whose agent build fails records Err and is never
// Passed, RunTask still returns the full slice (not the error), and the
// next run completes and passes (continuation).
func TestRunner_AgentFactoryErrorRecordsFailAndContinues(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	builds := 0
	factory := func(runDir string, spec ModelSpec) (*agent.Agent, error) {
		builds++
		if builds == 1 {
			return nil, errors.New("harness: agent build refused")
		}
		return scriptedFactory(t, mgr, true, "Done.")(runDir, spec)
	}

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory:  factory,
		ShapeCopy:     shapeManifest("echo fixture-bench-ok"),
		RunsPerTask:   2,
	}

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "bench-model"})
	if err != nil {
		t.Fatalf("RunTask: %v (a per-run failure is recorded on the run, not returned)", err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want 2 (record-and-continue)", len(runs))
	}
	if runs[0].Err == nil {
		t.Error("runs[0].Err = nil, want the agent build error")
	}
	if runs[0].Passed || runs[0].Result != nil {
		t.Errorf("runs[0] = Passed %v / Result %+v, want a failed run with no result", runs[0].Passed, runs[0].Result)
	}
	if !strings.Contains(runs[0].Err.Error(), "bench-test") || !strings.Contains(runs[0].Err.Error(), "run 1") {
		t.Errorf("runs[0].Err = %v, want the task id and run number named", runs[0].Err)
	}
	if runs[1].Err != nil || !runs[1].Passed {
		t.Errorf("runs[1] = %+v, want the second run to complete and pass (continuation)", runs[1])
	}
}

// TestRunner_UnknownStarterRecordsEveryRun pins the setup-error path at
// the first step: an unknown starter makes every run record the
// instantiation error (named) and stay unpassed, and RunTask still
// returns the full slice.
func TestRunner_UnknownStarterRecordsEveryRun(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	task := benchTask()
	task.Starter = "no-such-starter"

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory:  scriptedFactory(t, mgr, true, "Done."),
		RunsPerTask:   2,
	}

	runs, err := runner.RunTask(context.Background(), task, ModelSpec{Model: "bench-model"})
	if err != nil {
		t.Fatalf("RunTask: %v (a setup failure is recorded on the run, not returned)", err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want 2 (every run is reported)", len(runs))
	}
	for i, run := range runs {
		if run.Err == nil {
			t.Errorf("runs[%d].Err = nil, want the instantiation error", i)
		}
		if run.Passed || run.Result != nil {
			t.Errorf("runs[%d] = Passed %v / Result %+v, want a failed run with no result", i, run.Passed, run.Result)
		}
	}
	if !strings.Contains(runs[0].Err.Error(), "no-such-starter") {
		t.Errorf("runs[0].Err = %v, want the missing starter named", runs[0].Err)
	}
}

// TestRunner_ShapeCopyErrorFirstRunContinues pins a mid-setup failure
// (the copy hook) with continuation, and the per-run step ordering: when
// ShapeCopy fails on the first copy, that copy already carries the
// instantiated starter tree and the frozen plan (instantiate → plan
// write → shape).
func TestRunner_ShapeCopyErrorFirstRunContinues(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	copies := 0
	shape := func(runDir string, task *Task) error {
		copies++
		if copies == 1 {
			if _, err := os.Stat(filepath.Join(runDir, "index.html")); err != nil {
				t.Errorf("starter tree missing in copy %s before shape: %v", runDir, err)
			}
			if _, err := os.Stat(filepath.Join(runDir, ".sprout", "plan.json")); err != nil {
				t.Errorf("frozen plan missing in copy %s before shape: %v", runDir, err)
			}
			return errors.New("harness: shaping the first copy refused")
		}
		return shapeManifest("echo fixture-bench-ok")(runDir, task)
	}

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory:  scriptedFactory(t, mgr, true, "Done."),
		ShapeCopy:     shape,
		RunsPerTask:   2,
	}

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "bench-model"})
	if err != nil {
		t.Fatalf("RunTask: %v (a setup failure is recorded on the run, not returned)", err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want 2 (record-and-continue)", len(runs))
	}
	if runs[0].Err == nil || runs[0].Passed {
		t.Errorf("runs[0] = %+v, want the shape error recorded as a failed run", runs[0])
	}
	if !strings.Contains(runs[0].Err.Error(), "shaping the first copy refused") {
		t.Errorf("runs[0].Err = %v, want the shape error wrapped", runs[0].Err)
	}
	if runs[1].Err != nil || !runs[1].Passed {
		t.Errorf("runs[1] = %+v, want the second run to complete and pass (continuation)", runs[1])
	}
}

// ---------------------------------------------------------------------------
// Never-ran verification → fail (the strictness pin)
// ---------------------------------------------------------------------------

// TestRunner_NeverRanVerificationRecordsFail pins the strictness: with
// verification enabled but a text-only turn (the model changes no file,
// the hook's change gate stays closed), the hook never runs, no result
// is stored, and the run records as fail — no passing result, no pass
// (SP-154 §154a: pass/fail comes only from the SP-149 result).
func TestRunner_NeverRanVerificationRecordsFail(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory:  scriptedFactory(t, mgr, false, "Nothing to change here."),
		ShapeCopy:     shapeManifest("echo fixture-bench-ok"),
		RunsPerTask:   1,
	}

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "bench-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	run := runs[0]
	if run.Err != nil {
		t.Errorf("Err = %v, want nil (the turn itself completed cleanly)", run.Err)
	}
	if run.Result != nil {
		t.Errorf("Result = %+v, want nil (the hook never ran: the turn changed no code)", run.Result)
	}
	if run.Passed {
		t.Error("Passed = true, want false (no passing result, no pass)")
	}
}

// ---------------------------------------------------------------------------
// Normalization and the return contract
// ---------------------------------------------------------------------------

// TestRunner_DefaultRunsPerTask pins the runs-per-model normalization:
// 0 (the zero value) and negative values fall back to 3 (SP-154 §154b);
// a positive value stands.
func TestRunner_DefaultRunsPerTask(t *testing.T) {
	if got := (&Runner{}).runsPerTask(); got != 3 {
		t.Errorf("(&Runner{}).runsPerTask() = %d, want 3 (SP-154 §154b)", got)
	}
	if got := (&Runner{RunsPerTask: 5}).runsPerTask(); got != 5 {
		t.Errorf("RunsPerTask=5 → runsPerTask() = %d, want 5", got)
	}
	if got := (&Runner{RunsPerTask: -1}).runsPerTask(); got != 3 {
		t.Errorf("RunsPerTask=-1 → runsPerTask() = %d, want 3 (the default)", got)
	}
}

// TestRunner_CancelledContextStopsBetweenRuns pins the return contract:
// a ctx cancelled between runs stops the loop early and returns the
// completed runs alongside the wrapped context error.
func TestRunner_CancelledContextStopsBetweenRuns(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithCancel(context.Background())
	canceled := false
	shape := func(runDir string, task *Task) error {
		if !canceled {
			cancel()
			canceled = true
		}
		return shapeManifest("echo fixture-bench-ok")(runDir, task)
	}

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory:  scriptedFactory(t, mgr, true, "Done."),
		ShapeCopy:     shape,
		RunsPerTask:   3,
	}

	runs, err := runner.RunTask(ctx, benchTask(), ModelSpec{Model: "bench-model"})
	if err == nil {
		t.Fatal("RunTask: err = nil, want the wrapped context error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want the context cancellation wrapped", err)
	}
	// The first run completed (the cancellation is seen before run 2);
	// the remaining runs were never attempted.
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1 (the completed run, returned with the error)", len(runs))
	}
	if !runs[0].Passed {
		t.Errorf("runs[0].Passed = false, want true (it completed before the cancellation)")
	}
}

// TestRunner_NilOrEmptyTaskRejected pins the invalid-task side of the
// return contract: a nil task and a task with an empty request are
// errors (not recorded runs).
func TestRunner_NilOrEmptyTaskRejected(t *testing.T) {
	if _, err := (&Runner{}).RunTask(context.Background(), nil, ModelSpec{}); err == nil {
		t.Error("RunTask(nil task): err = nil, want the invalid-task error")
	}
	empty := benchTask()
	empty.Request = "   "
	if _, err := (&Runner{}).RunTask(context.Background(), empty, ModelSpec{}); err == nil {
		t.Error("RunTask(empty request): err = nil, want the invalid-task error")
	}
}

// TestRunner_ConfigureRun pins the runner's per-run configuration step:
// verification is forced on (the benchmark's pass/fail source must run)
// while the manager's other verification settings (repair limit, the
// explicit commands of SP-149 §149b) are preserved; the spec's
// model/provider are written into the manager (no save); an empty spec
// narrows nothing (the manager's values stand); a nil ConfigManager
// skips the step.
func TestRunner_ConfigureRun(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	runner := &Runner{ConfigManager: mgr}

	// The manager already carries run-specific verification settings;
	// configureRun must keep them and only force the gate on.
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.Verification = &configuration.VerificationConfig{
			RepairAttempts: 2,
			BuildCommand:   "custom-build",
		}
		return nil
	}); err != nil {
		t.Fatalf("seed manager config: %v", err)
	}

	if err := runner.configureRun(ModelSpec{Provider: "anthropic", Model: "claude-sonnet-4-5"}); err != nil {
		t.Fatalf("configureRun: %v", err)
	}
	cfg := mgr.GetConfig()
	if cfg == nil {
		t.Fatal("GetConfig = nil")
	}
	if cfg.Verification == nil || !cfg.Verification.Enabled {
		t.Error("verification not enabled by configureRun (it must be forced on)")
	}
	if cfg.Verification.RepairAttempts != 2 {
		t.Errorf("RepairAttempts = %d, want 2 (the manager's value, preserved)", cfg.Verification.RepairAttempts)
	}
	if cfg.Verification.BuildCommand != "custom-build" {
		t.Errorf("BuildCommand = %q, want custom-build (the manager's value, preserved)", cfg.Verification.BuildCommand)
	}
	if cfg.LastUsedProvider != "anthropic" {
		t.Errorf("LastUsedProvider = %q, want anthropic (the spec's provider)", cfg.LastUsedProvider)
	}
	if got := cfg.GetModelForProvider("anthropic"); got != "claude-sonnet-4-5" {
		t.Errorf("model for anthropic = %q, want claude-sonnet-4-5 (the spec's model)", got)
	}

	// An empty spec changes nothing but the forced-on verification.
	if err := runner.configureRun(ModelSpec{}); err != nil {
		t.Fatalf("configureRun(empty spec): %v", err)
	}
	cfg = mgr.GetConfig()
	if cfg.LastUsedProvider != "anthropic" {
		t.Errorf("LastUsedProvider = %q after the empty spec, want anthropic (unchanged)", cfg.LastUsedProvider)
	}
	if got := cfg.GetModelForProvider("anthropic"); got != "claude-sonnet-4-5" {
		t.Errorf("model = %q after the empty spec, want claude-sonnet-4-5 (unchanged)", got)
	}
	if cfg.Verification == nil || !cfg.Verification.Enabled {
		t.Error("verification not enabled after the empty spec (it must be forced on)")
	}
	if cfg.Verification.RepairAttempts != 2 || cfg.Verification.BuildCommand != "custom-build" {
		t.Errorf("verification section = %+v after the empty spec, want the manager's values preserved", cfg.Verification)
	}

	// A nil ConfigManager skips the step entirely.
	if err := (&Runner{}).configureRun(ModelSpec{Provider: "x", Model: "y"}); err != nil {
		t.Errorf("configureRun with a nil ConfigManager: %v, want nil (skipped)", err)
	}
}
