package agent

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/spec"
)

func TestFormatSelfReviewResult_BasicResult(t *testing.T) {
	result := &spec.ChangeReviewResult{
		RevisionID:   "rev-123",
		FilesChanged: 3,
		TotalChanges: 5,
		Summary:      "Reviewed 3 files with 5 changes",
	}

	output := formatSelfReviewResult(result)

	if !strings.Contains(output, "## Self-Review Results") {
		t.Error("should contain header")
	}
	if !strings.Contains(output, "**Revision ID**: rev-123") {
		t.Error("should contain revision ID")
	}
	if !strings.Contains(output, "**Files Changed**: 3") {
		t.Error("should contain files changed")
	}
	if !strings.Contains(output, "**Total Changes**: 5") {
		t.Error("should contain total changes")
	}
	if !strings.Contains(output, "Reviewed 3 files with 5 changes") {
		t.Error("should contain summary")
	}
	if !strings.Contains(output, "### Summary") {
		t.Error("should contain summary section")
	}
	// In-scope recommendation (no scope result means no violations)
	if !strings.Contains(output, "### [OK] Recommendation") {
		t.Error("should contain OK recommendation when no scope violations")
	}
}

func TestFormatSelfReviewResult_WithSpecResult(t *testing.T) {
	result := &spec.ChangeReviewResult{
		RevisionID:   "rev-456",
		FilesChanged: 2,
		TotalChanges: 4,
		Summary:      "Summary text",
		SpecResult: &spec.SpecExtractionResult{
			Spec: &spec.CanonicalSpec{
				Objective:  "Implement feature X",
				InScope:    []string{"task A", "task B"},
				OutOfScope: []string{"task C"},
			},
			Confidence: 0.85,
		},
	}

	output := formatSelfReviewResult(result)

	if !strings.Contains(output, "### Specification") {
		t.Error("should contain specification section")
	}
	if !strings.Contains(output, "**Objective**: Implement feature X") {
		t.Error("should contain objective")
	}
	if !strings.Contains(output, "**Confidence**: 85%") {
		t.Error("should contain formatted confidence")
	}
	if !strings.Contains(output, "**In Scope**:") {
		t.Error("should contain in-scope items")
	}
	if !strings.Contains(output, "- task A") {
		t.Error("should list in-scope items")
	}
	if !strings.Contains(output, "- task B") {
		t.Error("should list all in-scope items")
	}
	if !strings.Contains(output, "**Out of Scope**:") {
		t.Error("should contain out-of-scope section")
	}
	if !strings.Contains(output, "- task C") {
		t.Error("should list out-of-scope items")
	}
}

func TestFormatSelfReviewResult_WithInScopeValidation(t *testing.T) {
	result := &spec.ChangeReviewResult{
		RevisionID:   "rev-789",
		FilesChanged: 1,
		TotalChanges: 2,
		Summary:      "All good",
		ScopeResult: &spec.ScopeReviewResult{
			InScope: true,
		},
	}

	output := formatSelfReviewResult(result)

	if !strings.Contains(output, "### Scope Validation") {
		t.Error("should contain scope validation section")
	}
	if !strings.Contains(output, "[OK] **Status**: IN_SCOPE") {
		t.Error("should show IN_SCOPE status")
	}
	if !strings.Contains(output, "All changes align with the specification") {
		t.Error("should contain alignment message")
	}
}

func TestFormatSelfReviewResult_WithOutOfScopeValidation(t *testing.T) {
	result := &spec.ChangeReviewResult{
		RevisionID:   "rev-out",
		FilesChanged: 2,
		TotalChanges: 3,
		Summary:      "Issues found",
		ScopeResult: &spec.ScopeReviewResult{
			InScope: false,
			Summary: "Changes include unrelated modifications",
			Violations: []spec.ScopeViolation{
				{
					File:        "foo.go",
					Line:        42,
					Severity:    "high",
					Description: "Added unnecessary logging",
					Why:         "Not in spec",
				},
			},
			Suggestions: []string{
				"Remove the logging",
				"Simplify the function",
			},
		},
	}

	output := formatSelfReviewResult(result)

	if !strings.Contains(output, "[WARN] **Status**: OUT_OF_SCOPE") {
		t.Error("should show OUT_OF_SCOPE status")
	}
	if !strings.Contains(output, "Changes include unrelated modifications") {
		t.Error("should contain scope summary")
	}
	if !strings.Contains(output, "**Violations**") {
		t.Error("should contain violations section")
	}
	if !strings.Contains(output, "**[high]** [foo.go:42]") {
		t.Error("should format violation with severity, file, and line")
	}
	if !strings.Contains(output, "Added unnecessary logging") {
		t.Error("should include violation description")
	}
	if !strings.Contains(output, "Not in spec") {
		t.Error("should include violation why")
	}
	if !strings.Contains(output, "**Suggestions**") {
		t.Error("should contain suggestions section")
	}
	if !strings.Contains(output, "- Remove the logging") {
		t.Error("should list suggestions")
	}
	if !strings.Contains(output, "- Simplify the function") {
		t.Error("should list all suggestions")
	}
	// Should have warning recommendation
	if !strings.Contains(output, "### [WARN] Recommendation") {
		t.Error("should have warning recommendation for out-of-scope")
	}
	if !strings.Contains(output, "Scope violations were detected") {
		t.Error("should explain what to do about violations")
	}
}

func TestFormatSelfReviewResult_LowConfidenceRecommendation(t *testing.T) {
	result := &spec.ChangeReviewResult{
		RevisionID:   "rev-low",
		FilesChanged: 1,
		TotalChanges: 1,
		Summary:      "Low confidence",
		ScopeResult: &spec.ScopeReviewResult{
			InScope: true, // In scope, but...
		},
		SpecResult: &spec.SpecExtractionResult{
			Spec: &spec.CanonicalSpec{
				Objective: "Do something",
			},
			Confidence: 0.5, // Low confidence
		},
	}

	output := formatSelfReviewResult(result)

	// When in scope but low confidence (< 0.7), the code checks SpecResult.Confidence < 0.7
	// after the InScope check passes, so it shows WARN recommendation
	if !strings.Contains(output, "### [WARN] Recommendation") {
		t.Error("should show WARN recommendation when in scope but low spec confidence")
	}
	if !strings.Contains(output, "Spec confidence is low") {
		t.Error("should mention low spec confidence")
	}
}

func TestFormatSelfReviewResult_LowConfidenceNoScopeResult(t *testing.T) {
	result := &spec.ChangeReviewResult{
		RevisionID:   "rev-low-ns",
		FilesChanged: 1,
		TotalChanges: 1,
		Summary:      "Low confidence no scope",
		ScopeResult:  nil, // No scope result
		SpecResult: &spec.SpecExtractionResult{
			Spec: &spec.CanonicalSpec{
				Objective: "Do something",
			},
			Confidence: 0.5, // Low confidence
		},
	}

	output := formatSelfReviewResult(result)

	// No scope result + low confidence -> warning recommendation
	if !strings.Contains(output, "### [WARN] Recommendation") {
		t.Error("should have warning recommendation for low confidence without scope violations")
	}
	if !strings.Contains(output, "Spec confidence is low") {
		t.Error("should mention low spec confidence")
	}
}

func TestFormatSelfReviewResult_EmptySpecLists(t *testing.T) {
	result := &spec.ChangeReviewResult{
		RevisionID:   "rev-empty",
		FilesChanged: 0,
		TotalChanges: 0,
		Summary:      "No changes",
		SpecResult: &spec.SpecExtractionResult{
			Spec: &spec.CanonicalSpec{
				Objective:  "Test",
				InScope:    []string{},
				OutOfScope: []string{},
			},
			Confidence: 1.0,
		},
	}

	output := formatSelfReviewResult(result)

	// Should not have In Scope or Out of Scope sections when empty
	if strings.Contains(output, "**In Scope**:") {
		t.Error("should not show In Scope section when empty")
	}
	if strings.Contains(output, "**Out of Scope**:") {
		t.Error("should not show Out of Scope section when empty")
	}
}

func TestFormatSelfReviewResult_NilSpecResult(t *testing.T) {
	result := &spec.ChangeReviewResult{
		RevisionID:   "rev-nil-spec",
		FilesChanged: 1,
		TotalChanges: 1,
		Summary:      "No spec",
		ScopeResult: &spec.ScopeReviewResult{
			InScope: true,
		},
		SpecResult: nil,
	}

	output := formatSelfReviewResult(result)

	// Should not have Specification section
	if strings.Contains(output, "### Specification") {
		t.Error("should not show Specification section when spec is nil")
	}
	// Should have scope validation
	if !strings.Contains(output, "### Scope Validation") {
		t.Error("should show Scope Validation")
	}
}

func TestFormatSelfReviewResult_NilScopeResult(t *testing.T) {
	result := &spec.ChangeReviewResult{
		RevisionID:   "rev-nil-scope",
		FilesChanged: 1,
		TotalChanges: 1,
		Summary:      "No scope",
		SpecResult: &spec.SpecExtractionResult{
			Spec: &spec.CanonicalSpec{
				Objective: "Do something",
			},
			Confidence: 1.0,
		},
		ScopeResult: nil,
	}

	output := formatSelfReviewResult(result)

	// Should not have Scope Validation section
	if strings.Contains(output, "### Scope Validation") {
		t.Error("should not show Scope Validation when scope result is nil")
	}
	// Should have OK recommendation (no scope violations, high confidence)
	if !strings.Contains(output, "### [OK] Recommendation") {
		t.Error("should show OK recommendation")
	}
}

func TestFormatSelfReviewResult_ConfidenceFormatting(t *testing.T) {
	tests := []struct {
		confidence float64
		expected   string
	}{
		{1.0, "100%"},
		{0.0, "0%"},
		{0.75, "75%"},
		{0.33, "33%"},
	}

	for _, tt := range tests {
		result := &spec.ChangeReviewResult{
			RevisionID: "rev",
			Summary:    "test",
			SpecResult: &spec.SpecExtractionResult{
				Spec:       &spec.CanonicalSpec{Objective: "obj"},
				Confidence: tt.confidence,
			},
		}
		output := formatSelfReviewResult(result)
		if !strings.Contains(output, "**Confidence**: "+tt.expected) {
			t.Errorf("confidence %.2f should format as %s, got output without it", tt.confidence, tt.expected)
		}
	}
}

func TestFormatSelfReviewResult_MultipleViolations(t *testing.T) {
	result := &spec.ChangeReviewResult{
		RevisionID:   "rev-multi",
		FilesChanged: 2,
		TotalChanges: 4,
		Summary:      "Multiple issues",
		ScopeResult: &spec.ScopeReviewResult{
			InScope: false,
			Summary: "Multiple violations found",
			Violations: []spec.ScopeViolation{
				{File: "a.go", Line: 10, Severity: "critical", Description: "Critical issue", Why: "Very bad"},
				{File: "b.go", Line: 20, Severity: "low", Description: "Minor issue", Why: "Not ideal"},
			},
		},
	}

	output := formatSelfReviewResult(result)

	if !strings.Contains(output, "**[critical]** [a.go:10]") {
		t.Error("should format first violation")
	}
	if !strings.Contains(output, "**[low]** [b.go:20]") {
		t.Error("should format second violation")
	}
}
