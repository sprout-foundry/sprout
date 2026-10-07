// report.go — the benchmark report: one structured source
// (Report) rendering as both JSON and Markdown, comparing the suite's
// models and starters, with the pass rate per starter per model
// computed over the 3 runs and the failure categories that
// guide skill and prompt fixes.
//
// The report is a pure, deterministic function of its inputs — the run
// records, the suite's model list, and the run metadata (Meta): no
// clock, no network, and no map iteration in the renderers (the failure
// categories iterate in sorted key order). That is what makes the
// golden-file test possible (report_test.go pins the bytes) and the
// on-demand path safe: RunSuite (runner.go) feeds real runs in when a
// benchmark is run on demand — real models cost network and money, so
// the suite and the report are never part of `go test ./...`.
//
// Publication (where the published results live) is the spec's open
// question and is deliberately not wired here.
package benchmark

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

// Meta is the report's run metadata: the run date the
// results are published under and the sprout version the runs used
// (production passes buildinfo.Version; tests pass fixed values, so
// the report is a pure function of its inputs).
type Meta struct {
	RunDate string
	Version string
}

// Report is the benchmark report: one structured source
// rendering as both JSON and Markdown — comparing models and starters,
// with the pass rate per starter per model over the 3 runs and
// the failure categories that guide skill and prompt fixes.
type Report struct {
	Meta Meta `json:"meta"`
	// Models is the suite's model list (SuiteModels order) — recorded
	// as passed in, never re-derived from the runs.
	Models []ModelSpec `json:"models"`
	// Tasks are the (task, model, provider) pair reports in first-seen
	// order (the suite's model × task order when the runs come from
	// RunSuite).
	Tasks []TaskReport `json:"tasks"`
	// Starters pool the (task × run) matrix per (starter, model,
	// provider) in first-seen order — the "pass rate per starter
	// per model".
	Starters []StarterReport `json:"starters"`
	// FailureCategories counts the failed runs per failure mode (see
	// failureCategories); nil — omitted from the JSON — when nothing
	// failed.
	FailureCategories map[string]int `json:"failure_categories,omitempty"`
}

// TaskReport is one (task, model) pair's slice of the report: the
// pair's runs in run order (the full per-run metrics), the
// pair's pass count, and the pair's pass rate (Passed over len(Runs);
// 0 when the pair has no runs).
type TaskReport struct {
	TaskID string
	// Starter is the task's starter id.
	Starter  string
	Model    string
	Provider string
	// Runs are the pair's runs in run number order.
	Runs []Run
	// Passed is how many of the runs passed.
	Passed int
	// PassRate is Passed/len(Runs) (0 when there are no runs).
	PassRate float64
}

// StarterReport pools one (starter, model) pair's task × run matrix:
// the "pass rate per starter per model" over the 3 runs
// — the pooled rate a product's readiness bar would read.
type StarterReport struct {
	Starter  string
	Model    string
	Provider string
	// Tasks is how many distinct tasks of this starter the report
	// covers.
	Tasks int
	// RunsTotal is the pooled task × run matrix size.
	RunsTotal int
	Passed    int
	// PassRate is the pooled Passed/RunsTotal (0 when RunsTotal is 0).
	PassRate float64
}

// BuildReport assembles the report from the suite's runs.
// It is pure and deterministic: the same inputs always yield
// byte-identical JSON and Markdown (no clock, no network, no reordering
// beyond the sorted failure-category keys). Tasks group the runs by
// (TaskID, Model, Provider) in first-seen order — the suite's model ×
// task order when the runs come from RunSuite — and each pair's Runs
// keep run order. Starters pool the (task × run) matrix per (Starter,
// Model, Provider) in first-seen order: the pass rate per starter
// per model (pooled — the number a product's readiness bar reads).
// models is the suite's model list (SuiteModels order) — it is recorded
// as-is, never re-derived from the runs.
func BuildReport(runs []Run, models []ModelSpec, meta Meta) Report {
	rep := Report{
		Meta:     meta,
		Models:   models,
		Tasks:    make([]TaskReport, 0, len(runs)),
		Starters: make([]StarterReport, 0, len(runs)),
	}

	type pairKey struct{ task, model, provider string }
	type starterKey struct{ starter, model, provider string }
	taskIndex := make(map[pairKey]int, len(runs))
	starterAggregates := make(map[starterKey]*starterAggregate, len(runs))

	for _, run := range runs {
		pk := pairKey{run.TaskID, run.Model, run.Provider}
		ti, ok := taskIndex[pk]
		if !ok {
			ti = len(rep.Tasks)
			taskIndex[pk] = ti
			rep.Tasks = append(rep.Tasks, TaskReport{
				TaskID:   run.TaskID,
				Starter:  run.Starter,
				Model:    run.Model,
				Provider: run.Provider,
				Runs:     make([]Run, 0, 3),
			})
		}
		tr := &rep.Tasks[ti]
		tr.Runs = append(tr.Runs, run)
		if run.Passed {
			tr.Passed++
		}

		sk := starterKey{run.Starter, run.Model, run.Provider}
		ag, ok := starterAggregates[sk]
		if !ok {
			ag = &starterAggregate{tasks: make(map[string]struct{})}
			starterAggregates[sk] = ag
			rep.Starters = append(rep.Starters, StarterReport{
				Starter:  run.Starter,
				Model:    run.Model,
				Provider: run.Provider,
			})
		}
		ag.tasks[run.TaskID] = struct{}{}
		ag.runs++
		if run.Passed {
			ag.passed++
		}
	}

	for i := range rep.Tasks {
		tr := &rep.Tasks[i]
		if len(tr.Runs) > 0 {
			tr.PassRate = float64(tr.Passed) / float64(len(tr.Runs))
		}
	}
	for i := range rep.Starters {
		sr := &rep.Starters[i]
		ag := starterAggregates[starterKey{sr.Starter, sr.Model, sr.Provider}]
		sr.Tasks = len(ag.tasks)
		sr.RunsTotal = ag.runs
		sr.Passed = ag.passed
		if ag.runs > 0 {
			sr.PassRate = float64(ag.passed) / float64(ag.runs)
		}
	}

	if cats := failureCategories(runs); len(cats) > 0 {
		rep.FailureCategories = cats
	}
	return rep
}

// starterAggregate accumulates the pooled (task × run) matrix for one
// (starter, model, provider) pair while BuildReport walks the runs.
type starterAggregate struct {
	tasks  map[string]struct{}
	runs   int
	passed int
}

// failureCategories counts the failed runs' failure categories
// (the categories that guide skill and prompt fixes). One failed
// run may contribute to several categories; a passed run contributes
// nothing. It returns nil when no category was counted — the report
// omits the field then (omitempty).
//
// Rules, per failed run:
//   - Each executed failed check (not skipped, not passed) adds its
//     plancontract kind — check.Kind.String(): "build", "test", "page",
//     "interaction"; a future kind extends the set automatically.
//     Manual checks are never counted: they are reported by a human,
//     never machine-checked, so a failed manual check says nothing
//     about the agent.
//   - "stopped_by_rule" when the verification result failed and the
//     turn-end hook exhausted its repair budget (RepairLimit > 0 and
//     RepairRounds >= RepairLimit): the agent got its full repair
//     chance and the stopping rule still ended the turn.
//   - "error" for runs with Run.Err set (a setup or turn failure — such
//     a run is never Passed) or with run-level errors on the result
//     (Failed via Errors, no failed check — e.g. an unreadable plan or
//     manifest): the benchmark's plumbing, not the agent's work, is
//     what needs fixing.
func failureCategories(runs []Run) map[string]int {
	cats := make(map[string]int)
	for _, run := range runs {
		if run.Passed {
			continue
		}
		if run.Result != nil {
			for _, c := range run.Result.Checks {
				if c.Skipped || c.Passed || c.Kind == plancontract.KindManual {
					continue
				}
				cats[c.Kind.String()]++
			}
			if run.Result.Failed() && run.RepairLimit > 0 && run.RepairRounds >= run.RepairLimit {
				cats["stopped_by_rule"]++
			}
			if len(run.Result.Errors) > 0 {
				cats["error"]++
			}
		}
		if run.Err != nil {
			cats["error"]++
		}
	}
	if len(cats) == 0 {
		return nil
	}
	return cats
}

// JSON renders the report as deterministic, indented JSON: Go marshals
// the struct in field order, sorts the failure-categories map keys, and
// renders the runs' time.Time fields as RFC3339 — the bytes are a
// function of the report's contents alone (the golden fixture pins the
// exact timestamps). A run-level error (Run.Err) marshals as {} —
// standard library error types carry the message in unexported fields;
// the message itself is on Run.Err for programmatic use and in the
// Markdown's result cell ("error: <one-line err>"). A trailing newline
// follows.
func (rep Report) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("benchmark: marshal report as JSON: %w", err)
	}
	return append(data, '\n'), nil
}

// Markdown renders the report as deterministic Markdown:
// no clock and no map iteration — the models line and the task
// detail follow the report's slice order, and the failure categories
// iterate in sorted key order. The starter table carries the pooled
// pass rate per starter per model, and each task table carries the
// cost and turns columns the acceptance criteria pin.
func (rep Report) Markdown() string {
	var b strings.Builder
	b.WriteString("# Agent benchmark — " + rep.Meta.RunDate + " (sprout " + rep.Meta.Version + ")\n\n")

	labels := make([]string, 0, len(rep.Models))
	for _, m := range rep.Models {
		labels = append(labels, modelLabel(m.Provider, m.Model))
	}
	b.WriteString("Models: " + strings.Join(labels, ", ") +
		fmt.Sprintf(" (%d runs per task)", maxRunsPerTask(rep.Tasks)) + "\n\n")

	b.WriteString("## Pass rate per starter per model\n\n")
	b.WriteString("| Starter | Model | Tasks | Runs | Passed | Pass rate |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, sr := range rep.Starters {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %s |\n",
			sr.Starter, modelLabel(sr.Provider, sr.Model), sr.Tasks, sr.RunsTotal, sr.Passed,
			passRateCell(sr.Passed, sr.RunsTotal))
	}

	b.WriteString("\n## Failure categories\n\n")
	if len(rep.FailureCategories) == 0 {
		b.WriteString("No failures.\n")
	} else {
		b.WriteString("| Category | Failed runs |\n")
		b.WriteString("|---|---|\n")
		keys := make([]string, 0, len(rep.FailureCategories))
		for k := range rep.FailureCategories {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "| %s | %d |\n", k, rep.FailureCategories[k])
		}
	}

	b.WriteString("\n## Task detail\n\n")
	for _, tr := range rep.Tasks {
		fmt.Fprintf(&b, "### %s — %s — %s\n\n", tr.TaskID, tr.Starter, modelLabel(tr.Provider, tr.Model))
		b.WriteString("| Run | Result | Turns | Tokens | Cost | Repair rounds | Wall |\n")
		b.WriteString("|---|---|---|---|---|---|---|\n")
		for _, run := range tr.Runs {
			fmt.Fprintf(&b, "| %d | %s | %d | %d | $%.4f | %d | %s |\n",
				run.RunNumber, resultCell(run), run.Turns, run.Tokens, run.Cost,
				run.RepairRounds, run.FinishedAt.Sub(run.StartedAt))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// modelLabel renders one model as "provider/model", the report's
// convention (an empty field renders as-is: "/model" when the run's
// configuration default applies to the provider).
func modelLabel(provider, model string) string { return provider + "/" + model }

// maxRunsPerTask is the largest pair's run count (the "n runs per task"
// the models line reports; 0 when the report has no tasks).
func maxRunsPerTask(tasks []TaskReport) int {
	max := 0
	for _, tr := range tasks {
		if len(tr.Runs) > max {
			max = len(tr.Runs)
		}
	}
	return max
}

// passRateCell renders one pass-rate cell: "n/m (pct%)" with pct the
// rounded whole percentage (4 of 6 → "4/6 (67%)"); a zero matrix
// renders as "0/0 (—)".
func passRateCell(passed, total int) string {
	if total == 0 {
		return "0/0 (—)"
	}
	pct := int(math.Round(100 * float64(passed) / float64(total)))
	return fmt.Sprintf("%d/%d (%d%%)", passed, total, pct)
}

// resultCell renders one run's outcome: "pass", "fail", or
// "error: <one-line err>" — a run-level error outranks the verification
// outcome (the run did not complete cleanly, whatever the check says).
func resultCell(run Run) string {
	if run.Err != nil {
		return "error: " + oneLine(run.Err.Error())
	}
	if run.Passed {
		return "pass"
	}
	return "fail"
}

// oneLine folds an error message onto a single line for a table cell:
// every whitespace run becomes one space.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
