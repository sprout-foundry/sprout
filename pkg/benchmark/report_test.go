//go:build !js

// report_test.go — the report tests.
//
// TestReportGolden pins the report's JSON and Markdown byte-for-byte
// against testdata/report.json and testdata/report.md, built from a
// deterministic 12-run fixture (2 models × 2 tasks × 3 runs — no
// clock, no network). The scenario deliberately covers: a (task, model)
// pair that passes all 3; a pair that fails all 3 on the build check;
// a mixed pair (2 of 3); a stopped_by_rule run (the hook exhausted its
// repair budget); a setup-error run (Err set, no result); failed
// test/page/interaction checks; and a language-guard mismatch. The same
// test asserts the structural invariants the bytes imply — the pooled
// starter pass rates, the hand-computed category counts, and
// byte-identical rebuilds (the report is a pure function of its
// inputs).
//
// Golden-file convention — established here (the repo had no prior
// golden convention): the golden file lives in testdata/ next to the
// test. Set UPDATE_GOLDEN=1 to rewrite the golden files from the
// current output, then re-run the test normally to confirm a clean
// comparison:
//
//	UPDATE_GOLDEN=1 go test -count=1 -run TestReportGolden ./pkg/benchmark/
//	go test -count=1 -run TestReportGolden ./pkg/benchmark/

package benchmark

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// goldenModels is the golden fixture's suite model list (SuiteModels
// order: alpha first, beta second).
var goldenModels = []ModelSpec{
	{Model: "alpha", Provider: "prov-a"},
	{Model: "beta", Provider: "prov-b"},
}

// goldenMeta is the golden fixture's run metadata: fixed values so the
// report is a pure function of the fixture (production passes
// buildinfo.Version and the real run date).
var goldenMeta = Meta{RunDate: "2026-10-04", Version: "v0.0.0-golden"}

// goldenTime is a fixed UTC instant of the golden run date: no clock
// anywhere in the fixture.
func goldenTime(min, sec, ms int) time.Time {
	return time.Date(2026, 10, 4, 8, min, sec, ms*1_000_000, time.UTC)
}

// goldenBuildPass / goldenBuildFail are the fixture's verification
// results (the build-only shape keeps the golden slim; the failed
// checks carry a one-line excerpt — the verification evidence).
func goldenBuildPass() *verify.Result {
	return &verify.Result{PlanRevision: 1, Checks: []verify.Check{
		{Kind: plancontract.KindBuild, Command: "npm run build", Passed: true},
	}}
}

func goldenBuildFail() *verify.Result {
	return &verify.Result{PlanRevision: 1, Checks: []verify.Check{
		{Kind: plancontract.KindBuild, Command: "npm run build",
			Excerpt: "tsc: error TS2304: Cannot find name 'bench'."},
	}}
}

// goldenRuns builds the fixture's 14 runs in suite order (models outer —
// alpha, then beta — tasks inner — add-badge, then dark-mode — run
// number within the pair):
//
//	add-badge × alpha: 3 passes (r2 carries a language-guard mismatch)
//	dark-mode × alpha: r1 fails on the test check, r2 fails on the page
//	                   check, r3 is a setup-error run (Err set, no
//	                   result, metrics zero)
//	add-badge × beta:  2 passes, r3 fails on the build check after the
//	                   hook exhausted its repair budget (stopped_by_rule)
//	dark-mode × beta:  r1, r2 and r4 fail on the build check (r2's run
//	                   also fails the interaction check); r3 is a
//	                   silent-fail run — no verification result and no Err
//	                   — carrying a not-verified reason, so the report's
//	                   failure categories are non-empty and the Result
//	                   cell names the reason
//
// Hand-computed expectations the test asserts (not just the golden
// bytes): 5 of 14 runs pass; starter fixture/alpha pooled 3/6 (50%),
// starter fixture/beta pooled 2/7 (29%); failure categories build 4,
// test 1, page 1, interaction 1, stopped_by_rule 1, error 1, not_verified 1.
func goldenRuns() []Run {
	base := func(task, model, provider string, runNumber int, start, end time.Time, tokens int, cost float64) Run {
		return Run{
			TaskID: task, Starter: "fixture", Model: model, Provider: provider,
			RunNumber: runNumber, StartedAt: start, FinishedAt: end,
			Turns: 1, Tokens: tokens, Cost: cost,
		}
	}
	testFail := func() *verify.Result {
		return &verify.Result{PlanRevision: 1, Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Command: "npm run build", Passed: true},
			{Kind: plancontract.KindTest, Command: "npm test",
				Excerpt: "2 of 14 tests failed."},
		}}
	}
	pageFail := func() *verify.Result {
		return &verify.Result{PlanRevision: 1, Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Command: "npm run build", Passed: true},
			{Kind: plancontract.KindPage, Command: "npm run dev", Routes: []string{"/"},
				Excerpt: "route /: HTTP 500"},
		}}
	}
	buildInteractionFail := func() *verify.Result {
		return &verify.Result{PlanRevision: 1, Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Command: "npm run build",
				Excerpt: "tsc: error TS2304: Cannot find name 'bench'."},
			{Kind: plancontract.KindInteraction, Command: "browser: 2 steps",
				Excerpt: "step 2: expected element missing"},
		}}
	}

	runs := make([]Run, 0, 14)
	add := func(r Run) { runs = append(runs, r) }

	// add-badge × prov-a/alpha: all three runs pass (r2 carries the
	// language-guard mismatch — a mismatch can coexist with a pass).
	r := base("add-badge", "alpha", "prov-a", 1, goldenTime(0, 0, 0), goldenTime(0, 1, 234), 250, 0.0011)
	r.Passed = true
	r.Result = goldenBuildPass()
	// The run's per-role usage: a single coder-role
	// entry whose split sums to the run's totals (250 tokens, 0.0011
	// cost, 2 model calls). Every other golden run leaves RoleUsage
	// empty (the serialized "RoleUsage": null) — this one pins the
	// populated shape.
	r.RoleUsage = []agent.RoleUsage{
		{Role: "coder", PromptTokens: 200, CompletionTokens: 50, Tokens: 250,
			ChargedCost: 0.0011, TokenCost: 0.0011, Calls: 2},
	}
	add(r)
	r = base("add-badge", "alpha", "prov-a", 2, goldenTime(0, 5, 0), goldenTime(0, 7, 500), 310, 0.0013)
	r.Passed = true
	r.Result = goldenBuildPass()
	r.LangChecks = 1
	r.LangMismatches = 1
	add(r)
	r = base("add-badge", "alpha", "prov-a", 3, goldenTime(0, 10, 0), goldenTime(0, 12, 15), 290, 0.0012)
	r.Passed = true
	r.Result = goldenBuildPass()
	add(r)

	// dark-mode × prov-a/alpha: test failure, page failure, setup error.
	r = base("dark-mode", "alpha", "prov-a", 1, goldenTime(2, 0, 0), goldenTime(2, 5, 600), 500, 0.0031)
	r.Result = testFail()
	add(r)
	r = base("dark-mode", "alpha", "prov-a", 2, goldenTime(2, 10, 0), goldenTime(2, 16, 100), 520, 0.0033)
	r.Result = pageFail()
	add(r)
	r = base("dark-mode", "alpha", "prov-a", 3, goldenTime(2, 20, 0), goldenTime(2, 20, 500), 0, 0)
	r.Turns = 0
	r.Err = errors.New(`benchmark: instantiate starter "fixture" for task dark-mode run 3: harness: simulate setup failure`)
	add(r)

	// add-badge × prov-b/beta: two passes, then the stopped_by_rule run
	// (the repair budget was exhausted — one round against the N=1
	// limit — and the build check still failed).
	r = base("add-badge", "beta", "prov-b", 1, goldenTime(1, 0, 0), goldenTime(1, 2, 50), 340, 0.0021)
	r.Passed = true
	r.Result = goldenBuildPass()
	add(r)
	r = base("add-badge", "beta", "prov-b", 2, goldenTime(1, 5, 0), goldenTime(1, 8, 75), 355, 0.0022)
	r.Passed = true
	r.Result = goldenBuildPass()
	add(r)
	r = base("add-badge", "beta", "prov-b", 3, goldenTime(1, 10, 0), goldenTime(1, 14, 400), 420, 0.0027)
	r.Result = goldenBuildFail()
	r.RepairRounds = 1
	r.RepairLimit = 1
	r.RepairAttempts = map[string]int{"build": 1}
	add(r)

	// dark-mode × prov-b/beta: r1, r2 and r4 fail on the build check (r2's
	// run also fails the interaction check); r3 is a silent-fail run — no
	// verification result and no Err (the hook never ran) — carrying the
	// reason the report must surface instead of printing "No failures."
	// next to a failed run.
	r = base("dark-mode", "beta", "prov-b", 1, goldenTime(3, 0, 0), goldenTime(3, 3, 300), 480, 0.0029)
	r.Result = goldenBuildFail()
	add(r)
	r = base("dark-mode", "beta", "prov-b", 2, goldenTime(3, 5, 0), goldenTime(3, 9, 900), 495, 0.0030)
	r.Result = buildInteractionFail()
	add(r)
	r = base("dark-mode", "beta", "prov-b", 3, goldenTime(3, 10, 0), goldenTime(3, 15, 200), 510, 0.0032)
	r.NotVerifiedReason = "no code changes this turn"
	add(r)
	r = base("dark-mode", "beta", "prov-b", 4, goldenTime(3, 20, 0), goldenTime(3, 25, 400), 505, 0.0031)
	r.Result = goldenBuildFail()
	add(r)

	return runs
}

// checkGolden compares want against the golden file testdata/name — or
// rewrites it when UPDATE_GOLDEN is set (see the file header for the
// convention this establishes).
func checkGolden(t *testing.T, name string, want []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden dir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		t.Logf("wrote golden %s (%d bytes)", path, len(want))
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden:\n--- got ---\n%s\n--- golden ---\n%s", name, want, got)
	}
}

// TestReportGolden pins the report's bytes (the golden-file test): the
// fixture's JSON and Markdown must match testdata/report.json and
// testdata/report.md byte-for-byte. It also asserts the structural
// invariants the bytes imply — the pooled starter pass rates, the
// failure-category counts, and byte-identical rebuilds (determinism).
func TestReportGolden(t *testing.T) {
	rep := BuildReport(goldenRuns(), goldenModels, goldenMeta)
	rep2 := BuildReport(goldenRuns(), goldenModels, goldenMeta)

	// Structural invariants (not just the bytes).
	if !reflect.DeepEqual(rep.Meta, goldenMeta) {
		t.Errorf("Meta = %+v, want %+v", rep.Meta, goldenMeta)
	}
	if len(rep.Models) != 2 || rep.Models[0] != goldenModels[0] || rep.Models[1] != goldenModels[1] {
		t.Errorf("Models = %+v, want the suite list in order %+v", rep.Models, goldenModels)
	}

	type wantPair struct {
		task, model, provider string
		runs                  int
		passed                int
		passRate              float64
	}
	wantPairs := []wantPair{
		{"add-badge", "alpha", "prov-a", 3, 3, 1.0},
		{"dark-mode", "alpha", "prov-a", 3, 0, 0.0},
		{"add-badge", "beta", "prov-b", 3, 2, 2.0 / 3.0},
		{"dark-mode", "beta", "prov-b", 4, 0, 0.0}, // 4: the extra silent-fail run
	}
	if len(rep.Tasks) != len(wantPairs) {
		t.Fatalf("Tasks = %d, want %d (2 models × 2 tasks, first-seen order)", len(rep.Tasks), len(wantPairs))
	}
	for i, w := range wantPairs {
		tr := rep.Tasks[i]
		if tr.TaskID != w.task || tr.Model != w.model || tr.Provider != w.provider {
			t.Errorf("Tasks[%d] = %s/%s/%s, want %s/%s/%s (suite order: models outer, tasks inner)",
				i, tr.TaskID, tr.Model, tr.Provider, w.task, w.model, w.provider)
		}
		if len(tr.Runs) != w.runs {
			t.Errorf("Tasks[%d].Runs = %d, want %d", i, len(tr.Runs), w.runs)
		}
		if tr.Passed != w.passed {
			t.Errorf("Tasks[%d].Passed = %d, want %d", i, tr.Passed, w.passed)
		}
		if tr.PassRate != w.passRate {
			t.Errorf("Tasks[%d].PassRate = %v, want %v", i, tr.PassRate, w.passRate)
		}
	}

	type wantStarter struct {
		model, provider string
		passed          int
		runsTotal       int
		passRate        float64
	}
	wantStarters := []wantStarter{
		{"alpha", "prov-a", 3, 6, 3.0 / 6.0}, // pooled 3/6
		{"beta", "prov-b", 2, 7, 2.0 / 7.0},  // pooled 2/7
	}
	if len(rep.Starters) != len(wantStarters) {
		t.Fatalf("Starters = %d, want %d (one per model, same starter)", len(rep.Starters), len(wantStarters))
	}
	for i, w := range wantStarters {
		sr := rep.Starters[i]
		if sr.Starter != "fixture" {
			t.Errorf("Starters[%d].Starter = %q, want fixture (the same starter for both models)", i, sr.Starter)
		}
		if sr.Model != w.model || sr.Provider != w.provider {
			t.Errorf("Starters[%d] = %s/%s, want %s/%s", i, sr.Model, sr.Provider, w.model, w.provider)
		}
		if sr.Tasks != 2 || sr.RunsTotal != w.runsTotal {
			t.Errorf("Starters[%d] = Tasks %d / RunsTotal %d, want 2 / %d (the pooled task × run matrix)", i, sr.Tasks, sr.RunsTotal, w.runsTotal)
		}
		if sr.Passed != w.passed {
			t.Errorf("Starters[%d].Passed = %d, want %d (pooled)", i, sr.Passed, w.passed)
		}
		if sr.PassRate != w.passRate {
			t.Errorf("Starters[%d].PassRate = %v, want %v (the pooled starter rate the readiness bar reads)", i, sr.PassRate, w.passRate)
		}
	}

	wantCats := map[string]int{
		"build": 4, // add-badge×beta r3 + dark-mode×beta r1, r2 and r4
		"error": 1, // dark-mode×alpha r3 (Err set)
		// dark-mode×beta r2 (one run, two categories)
		"interaction":     1,
		"not_verified":    1, // dark-mode×beta r3 (no result, no err)
		"page":            1, // dark-mode×alpha r2
		"stopped_by_rule": 1, // add-badge×beta r3 (the exhausted repair budget)
		"test":            1, // dark-mode×alpha r1
	}
	if !reflect.DeepEqual(rep.FailureCategories, wantCats) {
		t.Errorf("FailureCategories = %v, want %v", rep.FailureCategories, wantCats)
	}

	// Determinism: two consecutive builds are byte-identical.
	json1, err := rep.JSON()
	if err != nil {
		t.Fatalf("JSON(): %v", err)
	}
	json2, err := rep2.JSON()
	if err != nil {
		t.Fatalf("JSON() (second build): %v", err)
	}
	if !bytes.Equal(json1, json2) {
		t.Errorf("two consecutive builds produced different JSON:\n--- 1 ---\n%s\n--- 2 ---\n%s", json1, json2)
	}
	if rep.Markdown() != rep2.Markdown() {
		t.Error("two consecutive builds produced different Markdown")
	}

	// The golden files (UPDATE_GOLDEN=1 rewrites them).
	checkGolden(t, "report.json", json1)
	checkGolden(t, "report.md", []byte(rep.Markdown()))
}

// TestReportFailureCategoriesRules pins the category rules' edges (beyond
// the golden's hand-computed counts): a run-level error with no failed
// check, one run contributing to several categories, a failed manual
// check (never counted), a never-ran verification failure (no rule
// matches), a repair budget that was not exhausted, and the all-pass →
// nil map.
func TestReportFailureCategoriesRules(t *testing.T) {
	mk := func(mutate func(*Run)) Run {
		r := Run{TaskID: "t", Starter: "fixture", Model: "m", Provider: "p", RunNumber: 1}
		mutate(&r)
		return r
	}

	// Run-level errors on the result (Failed via Errors, no failed
	// check): "error" only.
	r := mk(func(r *Run) {
		r.Result = &verify.Result{
			PlanRevision: 1,
			Checks:       []verify.Check{{Kind: plancontract.KindBuild, Command: "b", Passed: true}},
			Errors:       []string{"plan unreadable"},
		}
	})
	if cats := failureCategories([]Run{r}); !reflect.DeepEqual(cats, map[string]int{"error": 1}) {
		t.Errorf("run-level errors: cats = %v, want {error:1}", cats)
	}

	// Run.Err plus a failed check: both categories (one run may
	// contribute to several).
	r = mk(func(r *Run) {
		r.Result = goldenBuildFail()
		r.Err = errors.New("harness: the turn exploded")
	})
	if cats := failureCategories([]Run{r}); !reflect.DeepEqual(cats, map[string]int{"build": 1, "error": 1}) {
		t.Errorf("Err + failed check: cats = %v, want {build:1 error:1}", cats)
	}

	// A failed manual check is never counted: no categories at all.
	r = mk(func(r *Run) {
		r.Result = &verify.Result{PlanRevision: 1, Checks: []verify.Check{
			{Kind: plancontract.KindManual},
		}}
	})
	if cats := failureCategories([]Run{r}); len(cats) != 0 {
		t.Errorf("manual-only failure: cats = %v, want none (manual checks are reported by a human)", cats)
	}

	// A no-result, no-err run (the silent fail): failed, and now counted
	// as not_verified so the report can never print "No failures." next to
	// it. The specific reason rides on the run, not the category.
	r = mk(func(r *Run) { r.NotVerifiedReason = "no code changes this turn" })
	if cats := failureCategories([]Run{r}); !reflect.DeepEqual(cats, map[string]int{"not_verified": 1}) {
		t.Errorf("no-result failure: cats = %v, want {not_verified:1}", cats)
	}

	// A repair budget that was not exhausted: no stopped_by_rule.
	r = mk(func(r *Run) {
		r.Result = goldenBuildFail()
		r.RepairLimit = 2
		r.RepairRounds = 1
	})
	if cats := failureCategories([]Run{r}); !reflect.DeepEqual(cats, map[string]int{"build": 1}) {
		t.Errorf("unexhausted budget: cats = %v, want {build:1} (no stopped_by_rule)", cats)
	}

	// All passed: nil (the report omits the field).
	passed := Run{TaskID: "t", Starter: "fixture", Model: "m", Provider: "p", RunNumber: 1,
		Passed: true, Result: goldenBuildPass()}
	if cats := failureCategories([]Run{passed}); cats != nil {
		t.Errorf("all-pass: cats = %v, want nil", cats)
	}
}

// TestReportPassRateCell pins the pass-rate cell formatting: the rounded
// whole percentage and the zero-matrix cell.
func TestReportPassRateCell(t *testing.T) {
	cases := []struct {
		passed, total int
		want          string
	}{
		{4, 6, "4/6 (67%)"},
		{3, 6, "3/6 (50%)"},
		{2, 6, "2/6 (33%)"},
		{3, 3, "3/3 (100%)"},
		{0, 3, "0/3 (0%)"},
		{1, 3, "1/3 (33%)"},
		{0, 0, "0/0 (—)"},
	}
	for _, c := range cases {
		if got := passRateCell(c.passed, c.total); got != c.want {
			t.Errorf("passRateCell(%d, %d) = %q, want %q", c.passed, c.total, got, c.want)
		}
	}
}

// TestReportMarkdownNoFailures pins the no-failures rendering: the
// "No failures." line (no categories table), the models line, and the
// per-task cost/turns columns.
func TestReportMarkdownNoFailures(t *testing.T) {
	runs := []Run{{
		TaskID: "t", Starter: "fixture", Model: "m", Provider: "p", RunNumber: 1,
		Passed: true, Result: goldenBuildPass(),
		StartedAt: goldenTime(0, 0, 0), FinishedAt: goldenTime(0, 1, 0),
		Turns: 1, Tokens: 100, Cost: 0.001,
	}}
	rep := BuildReport(runs, []ModelSpec{{Model: "m", Provider: "p"}}, goldenMeta)
	if rep.FailureCategories != nil {
		t.Fatalf("FailureCategories = %v, want nil (every run passed)", rep.FailureCategories)
	}
	md := rep.Markdown()
	for _, want := range []string{
		"# Agent benchmark — 2026-10-04 (sprout v0.0.0-golden)",
		"Models: p/m (1 runs per task)",
		"No failures.",
		"| 1 | pass | 1 | 100 | $0.0010 | 0 | 1s |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown missing %q:\n%s", want, md)
		}
	}
}

// TestReportNoResultFailureNotSilent is the item's acceptance test: a
// failed run with no verification result and no Err (the exact
// "Result: null, Err: null" shape) must produce a non-empty
// FailureCategories and a Markdown report that does NOT print
// "No failures." — and its Result cell must name the not-verified reason.
func TestReportNoResultFailureNotSilent(t *testing.T) {
	run := Run{
		TaskID: "t", Starter: "fixture", Model: "m", Provider: "p", RunNumber: 1,
		NotVerifiedReason: "no code changes this turn",
		StartedAt:         goldenTime(0, 0, 0), FinishedAt: goldenTime(0, 1, 0),
		Turns: 1, Tokens: 100, Cost: 0.001,
	}
	rep := BuildReport([]Run{run}, []ModelSpec{{Model: "m", Provider: "p"}}, goldenMeta)

	if len(rep.FailureCategories) == 0 {
		t.Fatalf("FailureCategories = %v, want non-empty (a run with no verification result and no Err is a failure)", rep.FailureCategories)
	}
	if got := rep.FailureCategories["not_verified"]; got != 1 {
		t.Errorf("FailureCategories[not_verified] = %d, want 1 (got %v)", got, rep.FailureCategories)
	}

	md := rep.Markdown()
	if strings.Contains(md, "No failures.") {
		t.Errorf("Markdown contains %q next to a failed run:\n%s", "No failures.", md)
	}
	if !strings.Contains(md, "| not_verified | 1 |") {
		t.Errorf("Markdown missing the not_verified category row:\n%s", md)
	}
	if !strings.Contains(md, "fail: no verification result (no code changes this turn)") {
		t.Errorf("Markdown missing the reason in the Result cell:\n%s", md)
	}
}

// TestReportResultCellNotVerified pins resultCell's no-result branches: a
// reason is named, and an absent reason (verification disabled — which a
// benchmark run cannot normally reach) still renders a crisp cell rather
// than a bare "fail".
func TestReportResultCellNotVerified(t *testing.T) {
	withReason := Run{NotVerifiedReason: "verify timed out"}
	if got, want := resultCell(withReason), "fail: no verification result (verify timed out)"; got != want {
		t.Errorf("resultCell(with reason) = %q, want %q", got, want)
	}
	noReason := Run{}
	if got, want := resultCell(noReason), "fail: no verification result (reason not recorded)"; got != want {
		t.Errorf("resultCell(no reason) = %q, want %q", got, want)
	}
	if got := resultCell(Run{Err: errors.New("boom")}); got != "error: boom" {
		t.Errorf("resultCell(err) = %q, want %q (the error outranks the result)", got, "error: boom")
	}
	if got := resultCell(Run{Passed: true, Result: goldenBuildPass()}); got != "pass" {
		t.Errorf("resultCell(passed) = %q, want pass", got)
	}
	if got := resultCell(Run{Result: goldenBuildFail()}); got != "fail" {
		t.Errorf("resultCell(failed result) = %q, want fail", got)
	}
}

// TestReasonForMissingResult pins the runner's reason precedence: a
// timed-out turn is the runner's own knowledge and outranks the agent's
// reason; otherwise the agent's reason stands.
func TestReasonForMissingResult(t *testing.T) {
	if got := reasonForMissingResult(fmt.Errorf("wrap: %w", ErrRunTimeout), "no code changes this turn"); got != "verify timed out" {
		t.Errorf("timeout reason = %q, want %q", got, "verify timed out")
	}
	if got := reasonForMissingResult(nil, "verification setup error"); got != "verification setup error" {
		t.Errorf("agent reason = %q, want it passed through", got)
	}
	if got := reasonForMissingResult(errors.New("some other turn error"), "verification did not run this turn"); got != "verification did not run this turn" {
		t.Errorf("non-timeout turn error reason = %q, want the agent's reason", got)
	}
}
