//go:build !js

// runner_contract_test.go — the SP-154 §154a/§154b pass/fail contract and
// repair-loop tests: the recorded pass/fail comes only from the SP-149
// verification result (never the model's reply), a bad setup is recorded and
// the run continues, and the three-runs-per-model / fresh-copy guarantees
// hold. The fixtures and helpers live in runner_test.go (same package).

package benchmark

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

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
	// 154.3 per-task metrics: the runner issued one turn; the hook ran
	// one repair round (the report fed the failing build back once) and
	// consumed the build check's attempt budget against the harness's
	// N=1 limit.
	if run.Turns != 1 {
		t.Errorf("Turns = %d, want 1 (the runner issued one turn)", run.Turns)
	}
	if run.RepairRounds != 1 {
		t.Errorf("RepairRounds = %d, want 1 (the failing build fed the report back once)", run.RepairRounds)
	}
	if len(run.RepairAttempts) != 1 || run.RepairAttempts["build"] != 1 {
		t.Errorf("RepairAttempts = %v, want {build: 1}", run.RepairAttempts)
	}
	if run.RepairLimit != 1 {
		t.Errorf("RepairLimit = %d, want 1 (the harness's N=1)", run.RepairLimit)
	}
	assertWallTime(t, run)
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
	// 154.3: a passing run stores the limit (the hook saves it on a
	// passing run too) and no repair rounds or attempts.
	if run.Turns != 1 {
		t.Errorf("Turns = %d, want 1 (the runner issued one turn)", run.Turns)
	}
	if run.RepairRounds != 0 {
		t.Errorf("RepairRounds = %d, want 0 (a passing run needs no repair)", run.RepairRounds)
	}
	if len(run.RepairAttempts) != 0 {
		t.Errorf("RepairAttempts = %v, want empty (no repair round ran)", run.RepairAttempts)
	}
	if run.RepairLimit != 1 {
		t.Errorf("RepairLimit = %d, want 1 (the configured limit, stored on a passing run too)", run.RepairLimit)
	}
	assertWallTime(t, run)
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
	// 154.3: verification never ran (the hook's change gate stayed
	// closed) → the repair metrics are zero and empty.
	if run.RepairRounds != 0 || run.RepairLimit != 0 || len(run.RepairAttempts) != 0 {
		t.Errorf("repair metrics = rounds %d / limit %d / attempts %v, want all zero (the hook never ran)",
			run.RepairRounds, run.RepairLimit, run.RepairAttempts)
	}
	if run.Turns != 1 {
		t.Errorf("Turns = %d, want 1 (the runner still issued the turn)", run.Turns)
	}
	assertWallTime(t, run)
}
