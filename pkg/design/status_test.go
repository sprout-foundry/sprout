package design

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// statusWriteTree seeds the canonical valid fixture tree (writeValidDesignTree
// from tree_test.go) so the status aggregation runs over a shape that
// validates clean.
func statusWriteTree(t *testing.T, root string) {
	t.Helper()
	writeValidDesignTree(t, root)
}

// TestBuildDesignStatus_NoTreeIsExistsFalse pins the no-design contract: a
// workspace without design/ returns Exists=false with zeroed sections and an
// explicit synced drift flag — the health strip renders "no design tree",
// never an error.
func TestBuildDesignStatus_NoTreeIsExistsFalse(t *testing.T) {
	root := t.TempDir()
	s := BuildDesignStatus(root, nil)
	require.False(t, s.Exists)
	require.Equal(t, 0, s.Validation.Errors)
	require.Equal(t, 0, s.Validation.Warnings)
	require.Equal(t, 0, len(s.Validation.Findings))
	require.Equal(t, 0, s.Feedback.PendingCount)
	require.True(t, s.Drift.Synced, "drift synced must be explicit true on the no-tree path")
	require.NotEmpty(t, s.Summary)
}

// TestBuildDesignStatus_CleanTreeHasZeroTallies pins the aggregation contract
// on the canonical valid fixture: zero errors, both drift rows present, no
// pending feedback, code-ahead honestly not-assessable without a touched set.
func TestBuildDesignStatus_CleanTreeHasZeroTallies(t *testing.T) {
	root := t.TempDir()
	statusWriteTree(t, root)
	s := BuildDesignStatus(root, nil)
	require.True(t, s.Exists)
	require.Equal(t, 0, s.Validation.Errors, "seeded tree must be valid: %+v", s.Validation.Findings)

	require.True(t, s.Drift.DesignAhead.Synced || s.Drift.DesignAhead.Ahead)
	require.True(t, s.Drift.CodeAhead.Synced, "no touched set: code-ahead not assessable")
	require.Empty(t, s.Drift.CodeAhead.Remedy)
	require.True(t, s.Drift.Synced)

	require.Equal(t, 0, s.Feedback.PendingCount)
	require.NotEmpty(t, s.Summary)
}

// TestBuildDesignStatus_SeesErrorsAndDriftAndFeedback pins that all three
// sections move when the tree carries real signal: a dangling data-nav (hard
// error), tokens moved after an export (design-ahead), and a pending feedback
// file.
func TestBuildDesignStatus_SeesErrorsAndDriftAndFeedback(t *testing.T) {
	root := t.TempDir()
	statusWriteTree(t, root)

	// (1) Validation: a seeded wireframe with a dangling data-nav target is a
	// hard error (§4b).
	driftWrite(t, root, "design/wireframes/broken.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><rect data-nav="nowhere"/></svg>`)

	// (2) Drift: a genuine export, then move the tokens (§5a hash comparison)
	// so design-ahead becomes real.
	driftExport(t, root)
	driftWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {
    "brand": {
      "primary": { "$type": "color", "$value": "#ff0000" }
    }
  }
}`)

	// (3) Feedback: a pending document on the login screen.
	driftWrite(t, root, "design/feedback/login.json", `{
  "target": "design/screens/login.html",
  "status": "changes-requested",
  "resolution": "",
  "annotations": [
    {"id": "a1", "at": {"x": 0.5, "y": 0.5}, "area": "hierarchy",
     "note": "CTA reads as secondary", "resolved": false,
     "created": "2026-09-19T09:00:00Z"}
  ]
}`)

	s := BuildDesignStatus(root, nil)
	require.True(t, s.Exists)

	require.GreaterOrEqual(t, s.Validation.Errors, 1)
	require.NotEmpty(t, s.Validation.Findings)
	for _, f := range s.Validation.Findings {
		require.NotEmpty(t, f.File)
		require.NotEmpty(t, f.Rule)
	}

	require.True(t, s.Drift.DesignAhead.Ahead, "tokens moved after export; expected design-ahead")
	require.Greater(t, s.Drift.DesignAhead.Count, 0)
	require.NotEmpty(t, s.Drift.DesignAhead.Remedy)
	require.Equal(t, "design_export_tokens", s.Drift.DesignAhead.NextStep)
	require.False(t, s.Drift.Synced)

	require.Equal(t, 1, s.Feedback.PendingCount)
	require.Equal(t, "design/screens/login.html", s.Feedback.Pending[0].Target)
	require.Equal(t, 1, s.Feedback.Pending[0].Unresolved)

	require.Contains(t, s.Summary, "design-ahead")
}

// TestBuildDesignStatus_FindingsCappedAndSorted pins the cap: more findings
// than StatusFindingsCap still yield authoritative tallies with a capped,
// deterministically ordered list.
func TestBuildDesignStatus_FindingsCappedAndSorted(t *testing.T) {
	root := t.TempDir()
	statusWriteTree(t, root)
	// Seed more broken files than the cap; each dangling data-nav is one hard
	// error finding. Names stay slug-sorted and unique.
	for i := 0; i < StatusFindingsCap+10; i++ {
		name := "broken-" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		driftWrite(t, root, "design/wireframes/"+name+".svg",
			`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><rect data-nav="nowhere"/></svg>`)
	}
	s := BuildDesignStatus(root, nil)
	require.Len(t, s.Validation.Findings, StatusFindingsCap)
	require.Greater(t, s.Validation.Errors, StatusFindingsCap, "tallies must stay authoritative beyond the cap")
	for i := 1; i < len(s.Validation.Findings); i++ {
		a, b := s.Validation.Findings[i-1], s.Validation.Findings[i]
		require.LessOrEqual(t, a.File, b.File)
		if a.File == b.File {
			require.LessOrEqual(t, a.Line, b.Line)
		}
	}
}

// TestBuildDesignStatus_InvalidFeedbackNeverPending pins the rule that a
// broken feedback document cannot fabricate pending work.
func TestBuildDesignStatus_InvalidFeedbackNeverPending(t *testing.T) {
	root := t.TempDir()
	statusWriteTree(t, root)
	driftWrite(t, root, "design/feedback/login.json", `{not json`)
	s := BuildDesignStatus(root, nil)
	require.Equal(t, 0, s.Feedback.PendingCount)
}

// TestBuildDesignStatus_TokenRefsCountLexicalAliases pins the alias-reference
// counts (the co-editing alias-warning input): references inside $value
// aliases count per dotted path; unreferenced tokens are absent.
func TestBuildDesignStatus_TokenRefsCountLexicalAliases(t *testing.T) {
	root := t.TempDir()
	driftWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {
    "brand": {
      "primary": { "$type": "color", "$value": "#0055ff" },
      "focus": { "$type": "color", "$value": "{color.brand.primary}" }
    }
  },
  "button": {
    "border": { "$type": "color", "$value": "{color.brand.primary}" }
  }
}`)
	s := BuildDesignStatus(root, nil)
	require.True(t, s.Exists)
	require.Equal(t, 2, s.TokenRefs["color.brand.primary"])
	_, referenced := s.TokenRefs["color.brand.focus"]
	require.False(t, referenced, "unreferenced tokens are absent from the map")
}

// TestBuildDesignStatus_TalliesMatchValidatorDirect pins the one-truth
// contract: the tallies equal a direct ValidateTree run on the same tree, so
// the webui strip can never disagree with what design_validate reports.
func TestBuildDesignStatus_TalliesMatchValidatorDirect(t *testing.T) {
	root := t.TempDir()
	statusWriteTree(t, root)
	driftWrite(t, root, "design/wireframes/broken.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><rect data-nav="nowhere"/></svg>`)

	findings, err := ValidateTree(root)
	require.NoError(t, err)
	var errors, warnings, infos int
	for _, f := range findings {
		switch f.Severity {
		case SeverityError:
			errors++
		case SeverityWarn, SeverityFix:
			warnings++
		default:
			infos++
		}
	}
	s := BuildDesignStatus(root, nil)
	require.Equal(t, errors, s.Validation.Errors)
	require.Equal(t, warnings, s.Validation.Warnings)
	require.Equal(t, infos, s.Validation.Infos)
}

// TestBuildDesignStatus_Deterministic pins that repeated runs over the same
// tree produce identical payloads (the strip must not flicker).
func TestBuildDesignStatus_Deterministic(t *testing.T) {
	root := t.TempDir()
	statusWriteTree(t, root)
	a := BuildDesignStatus(root, nil)
	b := BuildDesignStatus(root, nil)
	require.Equal(t, a.Validation, b.Validation)
	require.Equal(t, a.Feedback, b.Feedback)
	require.Equal(t, a.Drift, b.Drift)
	require.Equal(t, a.TokenRefs, b.TokenRefs)
}
