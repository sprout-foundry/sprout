// quality_report_unit_test.go — pure unit tests for the quality hook's report
// builder and its reuse of the shared repair-loop stopping rule. They are
// pure — no agent, no fixture, no shell — so they run in every build including
// js/wasm.

package agent

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/verify"
)

// qhFailedLintResult is a deterministic failed quality run: the formatter
// passed, the linter failed with violation evidence.
func qhFailedLintResult() *verify.QualityResult {
	return &verify.QualityResult{
		Checks: []verify.QualityCheck{
			{Kind: verify.QualityFormat, Command: "gofmt -w .", Passed: true},
			{Kind: verify.QualityLint, Command: "golangci-lint run", Excerpt: "app.go:3: unused variable x\n"},
		},
	}
}

// TestBuildQualityReport_Repairable pins the exact report for the repairable
// case: the envelope, the framing line, the summary, and one bullet per
// repairable failing step (key, attempts used n/limit, reason, indented
// excerpt).
func TestBuildQualityReport_Repairable(t *testing.T) {
	res := qhFailedLintResult()
	got := buildQualityReport(res, map[string]int{"lint": 1}, 3)
	want := `<quality-report>
Turn-end quality checks found findings. Fix the repairable ones, then finish the turn.
Summary: ` + res.Summary() + `

Findings:
- lint 1/3 — command failed
  app.go:3: unused variable x

</quality-report>
`
	if got != want {
		t.Errorf("report mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestBuildQualityReport_Exhausted pins the stop-rule report: an exhausted
// step moves from the findings bullets to the explicit DONE section.
func TestBuildQualityReport_Exhausted(t *testing.T) {
	res := qhFailedLintResult()
	got := buildQualityReport(res, map[string]int{"lint": 3}, 3)
	want := `<quality-report>
Turn-end quality checks found findings. Fix the repairable ones, then finish the turn.
Summary: ` + res.Summary() + `

Steps that are DONE after the repair limit — stop trying to fix them and state the remaining findings plainly in your final reply:
- lint 3/3

</quality-report>
`
	if got != want {
		t.Errorf("report mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestQualityChecksAsVerifyChecks pins the adapter that lets the quality loop
// reuse the verification repair mechanism: the quality kinds become the
// counter keys, and the fields the stop rule reads are carried over.
func TestQualityChecksAsVerifyChecks(t *testing.T) {
	res := qhFailedLintResult()
	checks := qualityChecksAsVerifyChecks(res)
	if len(checks) != 2 {
		t.Fatalf("adapter returned %d checks, want 2", len(checks))
	}
	if got := checkAttemptKey(checks[0]); got != "format" {
		t.Errorf("format counter key = %q, want %q", got, "format")
	}
	if got := checkAttemptKey(checks[1]); got != "lint" {
		t.Errorf("lint counter key = %q, want %q", got, "lint")
	}
	if !checks[0].Passed {
		t.Error("the passing formatter check must carry Passed")
	}
	if checks[1].Passed || checks[1].Skipped {
		t.Error("the failing linter check must carry neither Passed nor Skipped")
	}
	if !strings.Contains(checks[1].Excerpt, "unused variable") {
		t.Errorf("the excerpt must be carried over, got %q", checks[1].Excerpt)
	}
}

// TestQualityLoopReusesRepairStopRule pins that the quality loop's stopping
// rule is the shared verification mechanism: the per-check rule fires once a
// failing step has used its attempts, and the total-rounds cap fires first
// when reached.
func TestQualityLoopReusesRepairStopRule(t *testing.T) {
	checks := qualityChecksAsVerifyChecks(qhFailedLintResult())

	// A failing step under its limit does not stop the loop.
	if repairLoopShouldStop(checks, map[string]int{"lint": 1}, 3, 0, 10) {
		t.Error("a failing step with attempts left must not stop the loop")
	}
	// The per-check rule fires once the failing step has used its limit.
	if !repairLoopShouldStop(checks, map[string]int{"lint": 3}, 3, 0, 10) {
		t.Error("an exhausted failing step must stop the loop (per-check rule)")
	}
	// The total-rounds cap fires first, even with attempts left.
	if !repairLoopShouldStop(checks, map[string]int{"lint": 0}, 3, 5, 5) {
		t.Error("reaching the total-rounds cap must stop the loop")
	}
}
