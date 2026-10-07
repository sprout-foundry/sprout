//go:build !js

// suite_test.go — the RunSuite acceptance tests: the
// suite runs every model of the override against each task in suite order
// and aggregates them into one report, a mid-suite error is recorded and
// the suite continues, and a nil runner or task list is rejected. The
// fixtures and helpers live in runner_test.go (same package); the run-level
// pass/fail contract and per-task metrics live there too.

package benchmark

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// RunSuite: models × tasks in suite order
// ---------------------------------------------------------------------------

// TestRunSuite_TwoModelsEndToEnd pins the spec's acceptance criterion:
// the runner produces a report for two models end to end. The suite
// runs every model of the override against the fixture task
// (RunsPerTask default: 3 runs per pair — 6 runs in suite order: model
// A's runs 1–3, then model B's runs 1–3), all scripted to pass, and
// BuildReport aggregates them: two starter reports (one per model, the
// same starter), each with a 3-run pass rate of 1.0 and no failure
// categories.
func TestRunSuite_TwoModelsEndToEnd(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory:  scriptedFactory(t, mgr, true, "Done!"),
		ShapeCopy:     shapeManifest("echo fixture-bench-ok"),
		Models: []ModelSpec{
			{Model: "alpha", Provider: "prov-a"},
			{Model: "beta", Provider: "prov-b"},
		},
	}

	runs, err := runner.RunSuite(context.Background(), []*Task{benchTask()})
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if len(runs) != 6 {
		t.Fatalf("runs = %d, want 6 (2 models × 1 task × 3 runs per pair)", len(runs))
	}
	for i, run := range runs {
		model, wantProvider := "alpha", "prov-a"
		if i >= 3 {
			model, wantProvider = "beta", "prov-b"
		}
		wantNumber := (i % 3) + 1
		if run.Model != model || run.RunNumber != wantNumber {
			t.Errorf("runs[%d] = model %q run %d, want model %q run %d (suite order: models outer, tasks inner, run number within the pair)",
				i, run.Model, run.RunNumber, model, wantNumber)
		}
		if run.Provider != wantProvider {
			t.Errorf("runs[%d].Provider = %q, want %q (the spec's provider, recorded on the run)", i, run.Provider, wantProvider)
		}
		if run.TaskID != "bench-test" || run.Starter != "fixture" {
			t.Errorf("runs[%d] = task %q / starter %q, want bench-test / fixture", i, run.TaskID, run.Starter)
		}
		if !run.Passed {
			t.Errorf("runs[%d].Passed = false, want true (the scripted build passes)", i)
		}
	}

	// The report: two starter reports — one per model,
	// the same starter — each with 3 runs and pass rate 1.0; no failure
	// categories (every run passed).
	rep := BuildReport(runs, runner.SuiteModels(), Meta{RunDate: "2026-10-04", Version: "v0.0.0-suite"})
	if len(rep.Starters) != 2 {
		t.Fatalf("Starters = %d, want 2 (one per model)", len(rep.Starters))
	}
	for i, sr := range rep.Starters {
		if sr.Starter != "fixture" {
			t.Errorf("Starters[%d].Starter = %q, want fixture (the same starter for both models)", i, sr.Starter)
		}
		if sr.RunsTotal != 3 || sr.Passed != 3 {
			t.Errorf("Starters[%d] = Passed %d / RunsTotal %d, want 3 / 3", i, sr.Passed, sr.RunsTotal)
		}
		if sr.PassRate != 1.0 {
			t.Errorf("Starters[%d].PassRate = %v, want 1.0", i, sr.PassRate)
		}
	}
	if len(rep.Tasks) != 2 {
		t.Fatalf("Tasks = %d, want 2 (one (task, model) pair per model)", len(rep.Tasks))
	}
	for i, tr := range rep.Tasks {
		if tr.PassRate != 1.0 || tr.Passed != 3 {
			t.Errorf("Tasks[%d] = Passed %d / PassRate %v, want 3 / 1.0", i, tr.Passed, tr.PassRate)
		}
	}
	if rep.FailureCategories != nil {
		t.Errorf("FailureCategories = %v, want nil (every run passed)", rep.FailureCategories)
	}
}

// TestRunSuite_PartialErrorMidSuite pins the partial-result contract: a
// RunTask suite-level error (the ctx cancelled between pairs) returns
// the runs accumulated so far alongside the wrapped context error — a
// partial report is still buildable (matching RunTask's own
// partial-result contract). The cancellation lands after the first
// model's last run is shaped, so the second model's RunTask sees the
// cancelled ctx before its first run.
func TestRunSuite_PartialErrorMidSuite(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithCancel(context.Background())
	shaped := 0
	shape := func(runDir string, task *Task) error {
		shaped++
		if shaped == 2 {
			cancel()
		}
		return shapeManifest("echo fixture-bench-ok")(runDir, task)
	}

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory:  scriptedFactory(t, mgr, true, "Done."),
		ShapeCopy:     shape,
		RunsPerTask:   2,
		Models: []ModelSpec{
			{Model: "alpha", Provider: "prov-a"},
			{Model: "beta", Provider: "prov-b"},
		},
	}

	runs, err := runner.RunSuite(ctx, []*Task{benchTask()})
	if err == nil {
		t.Fatal("RunSuite: err = nil, want the wrapped context error (the ctx cancelled between the two models)")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want the context cancellation wrapped", err)
	}
	if !strings.Contains(err.Error(), "cancelled before run 1") {
		t.Errorf("err = %v, want the second model's first run named (the suite-level error fires in its RunTask)", err)
	}
	// The first model's runs are returned alongside (the partial report
	// is still buildable); the second model's runs never ran.
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want 2 (the first model's 2 runs, returned with the error)", len(runs))
	}
	for i, run := range runs {
		if run.Model != "alpha" {
			t.Errorf("runs[%d].Model = %q, want alpha (only the first model ran)", i, run.Model)
		}
	}
}

// TestRunSuite_NilRunnerAndTaskList pins the suite's error paths: a nil
// runner and a nil task list are rejected up front (mirroring RunTask's
// invalid-input side), and a nil task inside the list propagates
// RunTask's invalid-task error mid-suite with the partial runs.
func TestRunSuite_NilRunnerAndTaskList(t *testing.T) {
	var nilRunner *Runner
	if _, err := nilRunner.RunSuite(context.Background(), []*Task{benchTask()}); err == nil {
		t.Error("RunSuite (nil runner): err = nil, want the nil-runner error")
	} else if !strings.Contains(err.Error(), "nil runner") {
		t.Errorf("RunSuite (nil runner): err = %v, want the nil-runner error", err)
	}
	if _, err := (&Runner{}).RunSuite(context.Background(), nil); err == nil {
		t.Error("RunSuite (nil task list): err = nil, want the task-list error")
	} else if !strings.Contains(err.Error(), "task list is required") {
		t.Errorf("RunSuite (nil task list): err = %v, want the task-list error", err)
	}
	// A nil task inside the list: RunTask's invalid-task error fires in
	// the first pair, with the runs accumulated so far (none here).
	runs, err := (&Runner{}).RunSuite(context.Background(), []*Task{nil})
	if err == nil {
		t.Error("RunSuite (nil task in list): err = nil, want the invalid-task error")
	} else if !strings.Contains(err.Error(), "task is required") {
		t.Errorf("RunSuite (nil task in list): err = %v, want RunTask's invalid-task error", err)
	} else if runs != nil {
		t.Errorf("RunSuite (nil task in list): runs = %v, want none (the pair never ran)", runs)
	}
}
