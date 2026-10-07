package health

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// fakeChecks is a CheckRunner that returns a scripted verification result (or
// an error), so the check portion of a health report is exercised without a
// shell.
type fakeChecks struct {
	result *verify.Result
	err    error
	calls  int
}

func (f *fakeChecks) Run(context.Context, string) (*verify.Result, error) {
	f.calls++
	return f.result, f.err
}

func failingResult() *verify.Result {
	return &verify.Result{
		Baseline: true,
		Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Command: "go build ./...", Passed: true},
			{
				Kind:     plancontract.KindTest,
				Command:  "go test ./...",
				Passed:   false,
				Excerpt:  "FAIL github.com/x/y 0.3s\n--- FAIL: TestThing (0.00s)",
				Duration: 0,
			},
		},
	}
}

func TestBuildReportsFixtureFindings(t *testing.T) {
	root := t.TempDir()
	// A file over the line threshold.
	writeFile(t, root, "big.go", repeatedLines(40))
	// A function over the complexity threshold.
	writeFile(t, root, "hot.go", `package x

func Hot(xs []int) int {
	n := 0
	for _, x := range xs {
		if x > 0 {
			if x%2 == 0 {
				n++
			}
		}
	}
	return n
}
`)
	writeFile(t, root, "small.go", "package x\n\nfunc Small() int { return 1 }\n")

	checks := &fakeChecks{result: failingResult()}
	report, err := Build(context.Background(), root, Config{
		MaxFileLines:  10,
		MaxComplexity: 3,
		Checks:        checks,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if checks.calls != 1 {
		t.Fatalf("check runner called %d times, want 1", checks.calls)
	}

	kinds := map[FindingKind]int{}
	for _, f := range report.Findings {
		kinds[f.Kind]++
		if f.Fix == nil {
			t.Errorf("finding %+v has no proposed fix", f)
		}
	}
	if kinds[KindFileSize] == 0 {
		t.Errorf("expected a file-size finding; got %+v", report.Findings)
	}
	if kinds[KindComplexity] == 0 {
		t.Errorf("expected a complexity finding; got %+v", report.Findings)
	}
	if kinds[KindCheck] != 1 {
		t.Errorf("expected exactly one failing-check finding, got %d", kinds[KindCheck])
	}
	if !strings.Contains(report.Checks, "test: failed") {
		t.Errorf("checks summary = %q, want the failing test named", report.Checks)
	}
}

func TestBuildFileSizeFindingProposesSplit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "big.go", repeatedLines(30))

	report, err := Build(context.Background(), root, Config{MaxFileLines: 10})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var got *Finding
	for i := range report.Findings {
		if report.Findings[i].Kind == KindFileSize {
			got = &report.Findings[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("no file-size finding: %+v", report.Findings)
	}
	if got.Target != "big.go" {
		t.Errorf("target = %q, want big.go", got.Target)
	}
	if got.Fix == nil || !strings.Contains(got.Fix.Summary, "split big.go") {
		t.Errorf("proposed fix = %+v, want a split of big.go", got.Fix)
	}
	if !strings.Contains(got.Fix.Summary, "30 lines") {
		t.Errorf("proposed fix should state the line count: %q", got.Fix.Summary)
	}
}

func TestBuildComplexityFindingGroupsByFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "hot.go", `package x

func A(x int) int {
	if x > 0 {
		if x > 1 {
			if x > 2 {
				return 1
			}
		}
	}
	return 0
}

func B(x int) int {
	if x > 0 {
		if x > 1 {
			if x > 2 {
				return 1
			}
		}
	}
	return 0
}
`)
	report, err := Build(context.Background(), root, Config{MaxComplexity: 2})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var complexity []Finding
	for _, f := range report.Findings {
		if f.Kind == KindComplexity {
			complexity = append(complexity, f)
		}
	}
	if len(complexity) != 1 {
		t.Fatalf("expected one grouped complexity finding, got %d: %+v", len(complexity), complexity)
	}
	f := complexity[0]
	if f.Target != "hot.go" {
		t.Errorf("target = %q, want hot.go", f.Target)
	}
	if !strings.Contains(f.Message, "A") || !strings.Contains(f.Message, "B") {
		t.Errorf("grouped finding should name both functions: %q", f.Message)
	}
}

func TestBuildNoChecksSkipsRunner(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "small.go", "package x\n")
	checks := &fakeChecks{result: failingResult()}

	report, err := Build(context.Background(), root, Config{Checks: checks, NoChecks: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if checks.calls != 0 {
		t.Errorf("check runner called %d times, want 0 with NoChecks", checks.calls)
	}
	if report.Checks != "" {
		t.Errorf("checks summary should be empty with NoChecks, got %q", report.Checks)
	}
}

func TestBuildRecordsCheckRunnerError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "small.go", "package x\n")
	checks := &fakeChecks{err: errors.New("no executor")}

	report, err := Build(context.Background(), root, Config{Checks: checks})
	if err != nil {
		t.Fatalf("Build must not fail on a check-runner error: %v", err)
	}
	if len(report.Errors) == 0 || !strings.Contains(report.Errors[0], "no executor") {
		t.Errorf("expected the runner error recorded, got %+v", report.Errors)
	}
}

func TestBuildSkippedCheckIsNotAFinding(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "small.go", "package x\n")
	checks := &fakeChecks{result: &verify.Result{
		Baseline: true,
		Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Skipped: true, Reason: "no build command"},
			{Kind: plancontract.KindTest, Skipped: true, Reason: "no test command"},
		},
	}}
	report, err := Build(context.Background(), root, Config{Checks: checks})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, f := range report.Findings {
		if f.Kind == KindCheck {
			t.Errorf("a skipped check must not be a finding: %+v", f)
		}
	}
}

func TestBuildOrdersFixBeforeWarn(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "big.go", repeatedLines(30))
	writeFile(t, root, "hot.go", `package x

func Hot(x int) int {
	if x > 0 {
		if x > 1 {
			return 1
		}
	}
	return 0
}
`)
	report, err := Build(context.Background(), root, Config{MaxFileLines: 10, MaxComplexity: 2})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	seenWarn := false
	for _, f := range report.Findings {
		if f.Severity == SeverityWarn {
			seenWarn = true
		}
		if f.Severity == SeverityFix && seenWarn {
			t.Fatalf("fix finding appeared after a warn finding: %+v", report.Findings)
		}
	}
}

func TestBuildRequiresRoot(t *testing.T) {
	if _, err := Build(context.Background(), "", Config{}); err == nil {
		t.Fatal("expected an error for an empty root")
	}
}

func TestReportCounts(t *testing.T) {
	r := &Report{Findings: []Finding{
		{Severity: SeverityFix},
		{Severity: SeverityFix},
		{Severity: SeverityWarn},
	}}
	fix, warn := r.Counts()
	if fix != 2 || warn != 1 {
		t.Fatalf("Counts = (%d, %d), want (2, 1)", fix, warn)
	}
}
