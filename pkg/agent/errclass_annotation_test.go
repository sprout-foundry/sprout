// errclass_annotation_test.go — pure unit tests for the classifier bridge
// that the turn-end reports consume: the annotation names the failure kind
// for a classifiable output and renders nothing for an unknown one, and the
// verification/quality report builders attach it alongside — never in place
// of — the raw excerpt. Pure (no agent, no fixture, no shell), so it runs
// in every build.

package agent

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// TestErrclassAnnotation_ClassifiesKnownOutput pins that a recognized build
// failure yields a non-empty annotation naming the category, and that the
// annotation embeds the explanation template rather than the raw output.
func TestErrclassAnnotation_ClassifiesKnownOutput(t *testing.T) {
	raw := "./main.go:3:8: cannot find package \"example.com/dep\" in any of:\n\t/usr/local/go/src (from $GOROOT)\n"
	got := errclassAnnotation(raw)
	if got == "" {
		t.Fatal("annotation for a missing-dependency output = \"\", want a classification line")
	}
	if !strings.Contains(got, "missing-dependency") {
		t.Errorf("annotation = %q, want it to name the missing-dependency category", got)
	}
	if !strings.Contains(got, "dependency") || !strings.Contains(got, "import") {
		t.Errorf("annotation = %q, want the explanation template text", got)
	}
	if strings.Contains(got, "example.com/dep") {
		t.Errorf("annotation = %q, must not embed the raw output (the report keeps the excerpt)", got)
	}
}

// TestErrclassAnnotation_UnknownRendersNothing pins the conservative path:
// an output that matches no known shape (including the empty string and a
// whitespace-only excerpt) renders no annotation, so an unclassified failure
// keeps the existing report shape.
func TestErrclassAnnotation_UnknownRendersNothing(t *testing.T) {
	for _, raw := range []string{"", "   \n\t", "make: nothing to be done\nall good here\n", "just some prose with no error tokens\n"} {
		if got := errclassAnnotation(raw); got != "" {
			t.Errorf("errclassAnnotation(%q) = %q, want \"\" (unknown: existing behavior)", raw, got)
		}
	}
}

// TestBuildVerificationReport_ClassifiedFailureAccompaniesRaw pins the
// classifier integration in the verification report: a classified failing
// check's bullet gains a "Classification:" line, and the raw excerpt is
// still present below it (the explanation accompanies, does not replace).
func TestBuildVerificationReport_ClassifiedFailureAccompaniesRaw(t *testing.T) {
	res := &verify.Result{
		Baseline: true,
		Checks: []verify.Check{
			{
				Kind:    plancontract.KindBuild,
				Command: "go build ./...",
				Excerpt: "./main.go:3:8: cannot find package \"example.com/dep\" in any of:\n",
			},
		},
	}
	got := buildVerificationReport(res, map[string]int{"build": 1}, 3)

	if !strings.Contains(got, "Classification: missing-dependency:") {
		t.Errorf("report missing the classification line:\n%s", got)
	}
	// The raw excerpt is preserved verbatim under the bullet.
	if !strings.Contains(got, "  ./main.go:3:8: cannot find package \"example.com/dep\" in any of:\n") {
		t.Errorf("report must still carry the raw excerpt:\n%s", got)
	}
	// Order: the bullet, then the classification, then the excerpt.
	bullet := strings.Index(got, "- build 1/3")
	class := strings.Index(got, "Classification: missing-dependency:")
	excerpt := strings.Index(got, "cannot find package")
	if bullet < 0 || class < 0 || excerpt < 0 || bullet >= class || class >= excerpt {
		t.Errorf("report order wrong (bullet<classification<excerpt): bullet=%d class=%d excerpt=%d\n%s", bullet, class, excerpt, got)
	}
}

// TestBuildVerificationReport_UnknownFailureKeepsExistingShape pins that an
// unclassified failing check renders exactly the pre-classifier report —
// no classification line (rule 3: unknown failures follow existing
// behavior).
func TestBuildVerificationReport_UnknownFailureKeepsExistingShape(t *testing.T) {
	res := &verify.Result{
		Baseline: true,
		Checks: []verify.Check{
			{
				Kind:    plancontract.KindBuild,
				Command: "make build",
				Excerpt: "make: nothing to be done for 'all'\n",
			},
		},
	}
	got := buildVerificationReport(res, map[string]int{"build": 1}, 3)
	if strings.Contains(got, "Classification:") {
		t.Errorf("unknown failure must not gain a classification line:\n%s", got)
	}
	// The excerpt is still carried even with no classification.
	if !strings.Contains(got, "  make: nothing to be done for 'all'\n") {
		t.Errorf("report must still carry the raw excerpt:\n%s", got)
	}
}

// TestBuildQualityReport_ClassifiedFailureAccompaniesRaw pins the same
// integration for the quality report: a classified failing linter gain a
// classification line next to the preserved raw excerpt.
func TestBuildQualityReport_ClassifiedFailureAccompaniesRaw(t *testing.T) {
	res := &verify.QualityResult{
		Checks: []verify.QualityCheck{
			{Kind: verify.QualityFormat, Command: "gofmt -w .", Passed: true},
			{
				Kind:    verify.QualityLint,
				Command: "golangci-lint run",
				Excerpt: "app.go:3:2: undefined: helper\n",
			},
		},
	}
	got := buildQualityReport(res, map[string]int{"lint": 1}, 3)
	if !strings.Contains(got, "Classification: type-error:") {
		t.Errorf("quality report missing the classification line:\n%s", got)
	}
	if !strings.Contains(got, "  app.go:3:2: undefined: helper\n") {
		t.Errorf("quality report must still carry the raw excerpt:\n%s", got)
	}
}

// TestBuildQualityReport_UnknownFindingKeepsExistingShape pins the
// unclassified path for the quality report.
func TestBuildQualityReport_UnknownFindingKeepsExistingShape(t *testing.T) {
	res := &verify.QualityResult{
		Checks: []verify.QualityCheck{
			{Kind: verify.QualityFormat, Command: "gofmt -w .", Passed: true},
			{Kind: verify.QualityLint, Command: "lint", Excerpt: "lint: nothing to report oddly\n"},
		},
	}
	got := buildQualityReport(res, map[string]int{"lint": 1}, 3)
	if strings.Contains(got, "Classification:") {
		t.Errorf("unknown finding must not gain a classification line:\n%s", got)
	}
}
