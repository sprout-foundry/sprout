package health

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twoIdenticalFunctions is a fixture body long enough (30+ tokens) and
// duplicated verbatim between two files, so the near-duplicate scan must find
// it without any embedding model or network.
const duplicateFunctionBody = `package x

import "strings"

func NormalizeLabel(raw string) string {
	trimmed := strings.TrimSpace(raw)
	lowered := strings.ToLower(trimmed)
	replaced := strings.ReplaceAll(lowered, "  ", " ")
	collapsed := strings.Trim(replaced, "-")
	if collapsed == "" {
		return "unnamed"
	}
	if len(collapsed) > 64 {
		collapsed = collapsed[:64]
	}
	return collapsed
}
`

func TestTextDuplicateFinderFindsNearIdenticalFunctions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "alpha.go", duplicateFunctionBody)
	// The same body in another file under a different name.
	writeFile(t, root, "beta.go", strings.Replace(duplicateFunctionBody, "NormalizeLabel", "CleanLabel", 1))

	matches, err := TextDuplicateFinder{}.FindDuplicates(context.Background(), root, DefaultDuplicateThreshold)
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected a near-duplicate match; got none")
	}
	got := matches[0]
	if got.Similarity < DefaultDuplicateThreshold {
		t.Fatalf("similarity = %v, want >= %v", got.Similarity, DefaultDuplicateThreshold)
	}
	files := map[string]bool{got.A.File: true, got.B.File: true}
	if !files["alpha.go"] || !files["beta.go"] {
		t.Fatalf("match should span alpha.go and beta.go, got %+v", got)
	}
}

func TestTextDuplicateFinderIgnoresDistinctFunctions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "alpha.go", duplicateFunctionBody)
	writeFile(t, root, "other.go", `package x

import "sort"

func SortIntsDescending(values []int) []int {
	sorted := append([]int(nil), values...)
	sort.Sort(sort.Reverse(sort.IntSlice(sorted)))
	if len(sorted) > 100 {
		sorted = sorted[:100]
	}
	return sorted
}
`)

	matches, err := TextDuplicateFinder{}.FindDuplicates(context.Background(), root, DefaultDuplicateThreshold)
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	for _, m := range matches {
		if m.A.File == "alpha.go" && m.B.File == "other.go" {
			t.Fatalf("unrelated functions matched: %+v", m)
		}
	}
}

func TestTextDuplicateFinderSkipsTinyFunctions(t *testing.T) {
	root := t.TempDir()
	tiny := "package x\n\nfunc Get() int { return 1 }\n"
	writeFile(t, root, "a.go", tiny)
	writeFile(t, root, "b.go", tiny)

	matches, err := TextDuplicateFinder{}.FindDuplicates(context.Background(), root, 0.5)
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("tiny functions must not be reported, got %+v", matches)
	}
}

func TestTextDuplicateFinderRequiresRoot(t *testing.T) {
	if _, err := (TextDuplicateFinder{}).FindDuplicates(context.Background(), "", 0); err == nil {
		t.Fatal("expected an error for an empty root")
	}
}

func TestDuplicateFindingsProposeSharedHelper(t *testing.T) {
	findings := duplicateFindings([]DuplicateMatch{{
		A:          CodeLocation{File: "a.go", Name: "Alpha", Line: 3},
		B:          CodeLocation{File: "b.go", Name: "Beta", Line: 5},
		Similarity: 0.93,
	}})
	if len(findings) != 1 {
		t.Fatalf("want one finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Kind != KindDuplication {
		t.Errorf("kind = %q, want %q", f.Kind, KindDuplication)
	}
	if f.Fix == nil {
		t.Fatalf("finding must carry a proposed fix")
	}
	if !strings.Contains(f.Fix.Summary, "a.go:Alpha") || !strings.Contains(f.Fix.Summary, "b.go:Beta") {
		t.Errorf("fix summary should name both sites: %q", f.Fix.Summary)
	}
	if !strings.Contains(f.Message, "93%") {
		t.Errorf("message should state the similarity: %q", f.Message)
	}
}

func TestBuildReportsDuplicateFixtureFinding(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "alpha.go", duplicateFunctionBody)
	writeFile(t, root, "beta.go", strings.Replace(duplicateFunctionBody, "NormalizeLabel", "CleanLabel", 1))

	report, err := Build(context.Background(), root, Config{
		Duplicates:         TextDuplicateFinder{},
		DuplicateThreshold: DefaultDuplicateThreshold,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var found bool
	for _, f := range report.Findings {
		if f.Kind == KindDuplication {
			found = true
			if f.Fix == nil {
				t.Errorf("duplication finding has no proposed fix: %+v", f)
			}
		}
	}
	if !found {
		t.Fatalf("expected a duplication finding; got %+v", report.Findings)
	}
}

func TestBuildNoDuplicatesSkipsFinder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "alpha.go", duplicateFunctionBody)

	report, err := Build(context.Background(), root, Config{
		Duplicates:   TextDuplicateFinder{},
		NoDuplicates: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, f := range report.Findings {
		if f.Kind == KindDuplication {
			t.Fatalf("duplication finding present despite NoDuplicates: %+v", f)
		}
	}
}

func TestBuildRecordsDuplicateFinderError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package x\n")

	report, err := Build(context.Background(), root, Config{Duplicates: errDuplicateFinder{}})
	if err != nil {
		t.Fatalf("Build must not fail on a finder error: %v", err)
	}
	if len(report.Errors) == 0 || !strings.Contains(report.Errors[0], "duplicates") {
		t.Fatalf("expected the finder error recorded, got %+v", report.Errors)
	}
}

type errDuplicateFinder struct{}

func (errDuplicateFinder) FindDuplicates(context.Context, string, float64) ([]DuplicateMatch, error) {
	return nil, os.ErrPermission
}

func TestParseGoModRequires(t *testing.T) {
	content := `module example.com/app

go 1.21

require (
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.11.1 // indirect
)

require github.com/foo/bar v0.4.0

replace github.com/foo/bar => ../bar
`
	reqs := parseGoModRequires(content)
	got := map[string]Requirement{}
	for _, r := range reqs {
		got[r.Module] = r
	}
	if len(reqs) != 3 {
		t.Fatalf("got %d requirements, want 3: %+v", len(reqs), reqs)
	}
	if got["github.com/spf13/cobra"].Version != "v1.10.2" {
		t.Errorf("cobra version = %q", got["github.com/spf13/cobra"].Version)
	}
	if !got["github.com/stretchr/testify"].Indirect {
		t.Errorf("testify should be marked indirect")
	}
	if got["github.com/foo/bar"].Version != "v0.4.0" {
		t.Errorf("single-line require not parsed: %+v", got["github.com/foo/bar"])
	}
	if !got["github.com/foo/bar"].Replaced {
		t.Errorf("github.com/foo/bar is pinned by a replace directive and must be marked replaced")
	}
	if got["github.com/spf13/cobra"].Replaced {
		t.Errorf("cobra is not replaced")
	}
}

func TestFindOutdatedSkipsReplacedModules(t *testing.T) {
	reqs := []Requirement{
		{Module: "github.com/spf13/cobra", Version: "v1.10.2"},
		{Module: "github.com/foo/bar", Version: "v0.4.0", Replaced: true},
	}
	checker := &fakeDependencyChecker{latest: map[string]string{
		"github.com/spf13/cobra": "v1.11.0",
		"github.com/foo/bar":     "v0.9.0",
	}}

	outdated, err := FindOutdated(context.Background(), reqs, checker, "/tmp/x")
	if err != nil {
		t.Fatalf("FindOutdated: %v", err)
	}
	if len(outdated) != 1 || outdated[0].Module != "github.com/spf13/cobra" {
		t.Fatalf("a replaced module must not be reported outdated, got %+v", outdated)
	}
}

func TestParseGoModReplacesBlockForm(t *testing.T) {
	content := `module x

go 1.21

replace (
	github.com/a/b => ../b
	github.com/c/d v1.0.0 => github.com/c/d v1.1.0
)
`
	replaced := parseGoModReplaces(content)
	if !replaced["github.com/a/b"] || !replaced["github.com/c/d"] {
		t.Fatalf("block replace directives not parsed: %+v", replaced)
	}
}

func TestValidateDuplicateThreshold(t *testing.T) {
	if err := ValidateDuplicateThreshold(0); err != nil {
		t.Errorf("0 means default at the Config level and must be accepted: %v", err)
	}
	if err := ValidateDuplicateThreshold(0.5); err != nil {
		t.Errorf("0.5 must be accepted: %v", err)
	}
	if err := ValidateDuplicateThreshold(1); err != nil {
		t.Errorf("1 must be accepted: %v", err)
	}
	if err := ValidateDuplicateThreshold(1.5); err == nil {
		t.Errorf(">1 must be rejected")
	}
	if err := RejectZeroDuplicateThreshold(0); err == nil {
		t.Errorf("an explicit 0 from the command layer must be rejected")
	}
	if err := RejectZeroDuplicateThreshold(0.85); err != nil {
		t.Errorf("0.85 must be accepted: %v", err)
	}
}

func TestBuildRejectsInvalidThreshold(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package x\n")
	if _, err := Build(context.Background(), root, Config{DuplicateThreshold: 2}); err == nil {
		t.Fatal("Build must reject an out-of-range duplicate threshold")
	}
}

func TestGoModRequiresMissingFile(t *testing.T) {
	reqs, err := goModRequires(t.TempDir())
	if err != nil {
		t.Fatalf("missing go.mod must not error: %v", err)
	}
	if len(reqs) != 0 {
		t.Fatalf("want no requirements, got %+v", reqs)
	}
}

func TestOutdatedThresholdExceeded(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v1.2.0", "v1.3.0", true},
		{"v1.2.0", "v1.2.0", false},
		{"v1.3.0", "v1.2.0", false},
		{"not-a-version", "v1.0.0", false},
		{"v1.0.0", "garbage", false},
	}
	for _, c := range cases {
		if got := OutdatedThresholdExceeded(c.current, c.latest); got != c.want {
			t.Errorf("OutdatedThresholdExceeded(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

// fakeDependencyChecker returns scripted latest versions with no network.
type fakeDependencyChecker struct {
	latest map[string]string
	err    error
	calls  int
}

func (f *fakeDependencyChecker) LatestVersions(context.Context, string) (map[string]string, error) {
	f.calls++
	return f.latest, f.err
}

func TestFindOutdatedFromFixture(t *testing.T) {
	reqs := []Requirement{
		{Module: "github.com/spf13/cobra", Version: "v1.10.2"},
		{Module: "github.com/stretchr/testify", Version: "v1.11.1"},
		{Module: "github.com/foo/bar", Version: "v0.4.0"},
	}
	checker := &fakeDependencyChecker{latest: map[string]string{
		"github.com/spf13/cobra":      "v1.11.0",
		"github.com/stretchr/testify": "v1.11.1",
	}}

	outdated, err := FindOutdated(context.Background(), reqs, checker, "/tmp/x")
	if err != nil {
		t.Fatalf("FindOutdated: %v", err)
	}
	if len(outdated) != 1 {
		t.Fatalf("want one outdated dep, got %d: %+v", len(outdated), outdated)
	}
	if outdated[0].Module != "github.com/spf13/cobra" || outdated[0].Latest != "v1.11.0" {
		t.Fatalf("wrong outdated entry: %+v", outdated[0])
	}
}

func TestBuildReportsOutdatedDependencyFixture(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", `module example.com/app

go 1.21

require (
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.11.1
)
`)
	checker := &fakeDependencyChecker{latest: map[string]string{
		"github.com/spf13/cobra": "v1.11.0",
	}}

	report, err := Build(context.Background(), root, Config{Deps: checker})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if checker.calls != 1 {
		t.Fatalf("checker called %d times, want 1", checker.calls)
	}
	var dep *Finding
	for i := range report.Findings {
		if report.Findings[i].Kind == KindOutdatedDep {
			dep = &report.Findings[i]
		}
	}
	if dep == nil {
		t.Fatalf("expected an outdated-dependency finding; got %+v", report.Findings)
	}
	if dep.Target != "github.com/spf13/cobra" {
		t.Errorf("target = %q, want the module path", dep.Target)
	}
	if dep.Fix == nil || !strings.Contains(dep.Fix.Summary, "bump github.com/spf13/cobra") {
		t.Errorf("fix should propose a bump: %+v", dep.Fix)
	}
	if !strings.Contains(dep.Fix.Summary, "v1.10.2") || !strings.Contains(dep.Fix.Summary, "v1.11.0") {
		t.Errorf("fix should name both versions: %q", dep.Fix.Summary)
	}
}

func TestBuildNoDepsSkipsChecker(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module x\n\ngo 1.21\n\nrequire github.com/a/b v1.0.0\n")
	checker := &fakeDependencyChecker{latest: map[string]string{"github.com/a/b": "v2.0.0"}}

	report, err := Build(context.Background(), root, Config{Deps: checker, NoDeps: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if checker.calls != 0 {
		t.Errorf("checker called %d times, want 0 with NoDeps", checker.calls)
	}
	for _, f := range report.Findings {
		if f.Kind == KindOutdatedDep {
			t.Fatalf("outdated finding present despite NoDeps: %+v", f)
		}
	}
}

func TestBuildRecordsDependencyCheckerError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module x\n\ngo 1.21\n\nrequire github.com/a/b v1.0.0\n")
	checker := &fakeDependencyChecker{err: os.ErrDeadlineExceeded}

	report, err := Build(context.Background(), root, Config{Deps: checker})
	if err != nil {
		t.Fatalf("Build must not fail on a checker error: %v", err)
	}
	if len(report.Errors) == 0 || !strings.Contains(report.Errors[0], "dependencies") {
		t.Fatalf("expected the checker error recorded, got %+v", report.Errors)
	}
}

func TestGoModRequiresReadsFixtureFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "go.mod")
	if err := os.WriteFile(path, []byte("module x\n\ngo 1.21\n\nrequire github.com/spf13/cobra v1.10.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reqs, err := goModRequires(root)
	if err != nil {
		t.Fatalf("goModRequires: %v", err)
	}
	if len(reqs) != 1 || reqs[0].Version != "v1.10.2" {
		t.Fatalf("unexpected requirements: %+v", reqs)
	}
}
