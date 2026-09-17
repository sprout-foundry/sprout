package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fbDoc is a §4d feedback document builder: target, status, resolution, and
// the annotations array, so the tests state only the field under test.
func fbDoc(target, status, annotations string) string {
	return `{"target":"` + target + `","status":"` + status + `","resolution":"","annotations":[` + annotations + `]}`
}

const fbAnnotation = `{"id":"a1","at":{"x":0.42,"y":0.18},"area":"hierarchy","note":"Primary CTA reads as secondary","resolved":false,"created":"2026-09-15T10:36:47Z"}`

func TestValidateFeedbackFile(t *testing.T) {
	stems := []string{"login", "home", "sign-up"}

	t.Run("valid", func(t *testing.T) {
		findings := validateFeedbackFile(t.TempDir(), "design/feedback/login.json",
			[]byte(fbDoc("design/wireframes/login.svg", "changes-requested", fbAnnotation)), stems)
		requireNoWireframeFindings(t, findings)
	})

	t.Run("valid-resolved-no-annotations", func(t *testing.T) {
		content := fbDoc("design/wireframes/home.svg", "resolved", "")
		findings := validateFeedbackFile(t.TempDir(), "design/feedback/home.json", []byte(content), stems)
		requireNoWireframeFindings(t, findings)
	})

	t.Run("invalid-json", func(t *testing.T) {
		findings := validateFeedbackFile(t.TempDir(), "design/feedback/x.json", []byte("{not json"), stems)
		assert.Equal(t, 1, findingRules(findings)[ruleFeedbackJSON])
		for _, f := range findings {
			if f.Rule == ruleFeedbackJSON {
				assert.Equal(t, SeverityError, f.Severity)
			}
		}
	})

	t.Run("top-level-array", func(t *testing.T) {
		findings := validateFeedbackFile(t.TempDir(), "design/feedback/x.json", []byte(`[1,2,3]`), stems)
		assert.Equal(t, 1, findingRules(findings)[ruleFeedbackJSON])
	})

	t.Run("top-level-null", func(t *testing.T) {
		// A bare `null` is valid JSON but not a document.
		findings := validateFeedbackFile(t.TempDir(), "design/feedback/x.json", []byte(`null`), stems)
		assert.Equal(t, 1, findingRules(findings)[ruleFeedbackJSON])
	})

	t.Run("empty-target", func(t *testing.T) {
		findings := validateFeedbackFile(t.TempDir(), "design/feedback/x.json", []byte(fbDoc("  ", "changes-requested", "")), stems)
		assert.Equal(t, 1, findingRules(findings)[ruleFeedbackTargetRef])
		for _, f := range findings {
			if f.Rule == ruleFeedbackTargetRef {
				assert.Equal(t, SeverityError, f.Severity, "an empty target is a hard violation")
			}
		}
	})

	t.Run("empty-annotation-id", func(t *testing.T) {
		ann := `{"id":"","at":{"x":0,"y":0},"area":"contrast","note":"x","resolved":false,"created":""}`
		findings := validateFeedbackFile(t.TempDir(), "design/feedback/x.json",
			[]byte(fbDoc("design/wireframes/login.svg", "changes-requested", ann)), stems)
		assert.Equal(t, 1, findingRules(findings)[ruleFeedbackAnnotationID])
	})

	t.Run("duplicate-annotation-id", func(t *testing.T) {
		ann := `{"id":"a1","at":{"x":0,"y":0},"area":"contrast","note":"x","resolved":false,"created":""},` +
			`{"id":"a1","at":{"x":0,"y":0},"area":"contrast","note":"y","resolved":false,"created":""}`
		findings := validateFeedbackFile(t.TempDir(), "design/feedback/x.json",
			[]byte(fbDoc("design/wireframes/login.svg", "changes-requested", ann)), stems)
		assert.Equal(t, 1, findingRules(findings)[ruleFeedbackAnnotationID])
	})

	t.Run("empty-annotation-note", func(t *testing.T) {
		ann := `{"id":"a1","at":{"x":0,"y":0},"area":"contrast","note":"  ","resolved":false,"created":""}`
		findings := validateFeedbackFile(t.TempDir(), "design/feedback/x.json",
			[]byte(fbDoc("design/wireframes/login.svg", "changes-requested", ann)), stems)
		assert.Equal(t, 1, findingRules(findings)[ruleFeedbackAnnotationNote])
		for _, f := range findings {
			if f.Rule == ruleFeedbackAnnotationNote {
				assert.Equal(t, SeverityInfo, f.Severity, "an empty note is advisory")
			}
		}
	})

	t.Run("dangling-target", func(t *testing.T) {
		findings := validateFeedbackFile(t.TempDir(), "design/feedback/x.json",
			[]byte(fbDoc("design/wireframes/nowhere.svg", "changes-requested", fbAnnotation)), stems)
		assert.Equal(t, 1, findingRules(findings)[ruleFeedbackTargetDangling])
		for _, f := range findings {
			if f.Rule == ruleFeedbackTargetDangling {
				assert.Equal(t, SeverityInfo, f.Severity)
			}
		}
	})

	t.Run("resolves-by-stem-across-kinds", func(t *testing.T) {
		// screens/*.html and flows/*.mmd resolve by the same stem vocabulary.
		for _, target := range []string{
			"design/wireframes/home.svg",
			"design/screens/login.html",
			"design/flows/sign-up.mmd",
		} {
			findings := validateFeedbackFile(t.TempDir(), "design/feedback/x.json",
				[]byte(fbDoc(target, "changes-requested", "")), stems)
			assert.Zero(t, findingRules(findings)[ruleFeedbackTargetDangling], "target %s should resolve", target)
		}
	})

	t.Run("no-stems-skips-crossref", func(t *testing.T) {
		// An empty stem list means there is nothing to resolve against; the
		// cross-reference must not fire a false dangling finding.
		findings := validateFeedbackFile(t.TempDir(), "design/feedback/x.json",
			[]byte(fbDoc("design/wireframes/anything.svg", "changes-requested", "")), nil)
		assert.Zero(t, findingRules(findings)[ruleFeedbackTargetDangling])
	})

	t.Run("uppercase-target-resolves-case-insensitively", func(t *testing.T) {
		// feedbackTargetStem lowercases, so an uppercase target still resolves
		// against the lowercase stem vocabulary.
		findings := validateFeedbackFile(t.TempDir(), "design/feedback/x.json",
			[]byte(fbDoc("design/wireframes/Login.SVG", "changes-requested", "")), stems)
		assert.Zero(t, findingRules(findings)[ruleFeedbackTargetDangling])
	})
}

func TestFeedbackTargetStem(t *testing.T) {
	cases := map[string]string{
		"design/wireframes/login.svg": "login",
		"wireframes/login.svg":        "login",
		"design/screens/Login.HTML":   "login",
		"design/flows/sign-up.mmd":    "sign-up",
		"design/wireframes/login":     "login",
		"login.svg":                   "login",
		"  design/wireframes/a/b.svg": "b",
		"":                            ".",
	}
	for in, want := range cases {
		assert.Equal(t, want, feedbackTargetStem(in), "feedbackTargetStem(%q)", in)
	}
}

func TestValidateFeedbackDir(t *testing.T) {
	t.Run("missing-dir", func(t *testing.T) {
		root := t.TempDir()
		findings, err := ValidateFeedbackDir(root)
		require.NoError(t, err)
		require.NotNil(t, findings)
		assert.Empty(t, findings)
	})

	t.Run("resolves-across-files", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "feedback"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "wireframes"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "flows"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "wireframes", "login.svg"), []byte("<svg viewBox=\"0 0 1 1\"></svg>"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "flows", "sign-up.mmd"), []byte("flowchart LR\n  a-->b\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "feedback", "login.json"),
			[]byte(fbDoc("design/wireframes/login.svg", "changes-requested", fbAnnotation)), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "feedback", "sign-up.json"),
			[]byte(fbDoc("design/flows/sign-up.mmd", "resolved", "")), 0o644))

		findings, err := ValidateFeedbackDir(root)
		require.NoError(t, err)
		requireNoWireframeFindings(t, findings)
	})

	t.Run("dangling-across-files", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "feedback"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "wireframes"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "wireframes", "login.svg"), []byte("<svg viewBox=\"0 0 1 1\"></svg>"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "feedback", "x.json"),
			[]byte(fbDoc("design/wireframes/nowhere.svg", "changes-requested", "")), 0o644))

		findings, err := ValidateFeedbackDir(root)
		require.NoError(t, err)
		assert.Equal(t, 1, findingRules(findings)[ruleFeedbackTargetDangling])
	})

	t.Run("unreadable-json-entry-is-an-error", func(t *testing.T) {
		// A dangling symlink named *.json is not a directory (so it is not
		// skipped) but cannot be read: ValidateFeedbackDir surfaces the I/O
		// failure as an error (the validator must not silently pass a tree it
		// could not read), whereas the reporting parse skips it.
		root := t.TempDir()
		dir := filepath.Join(root, "design", "feedback")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.Symlink(filepath.Join(dir, "missing-target"), filepath.Join(dir, "trap.json")))

		_, err := ValidateFeedbackDir(root)
		require.Error(t, err, "an unreadable feedback entry must be an I/O error, not a silent pass")

		states, scanErr := ScanFeedbackDir(root)
		require.NoError(t, scanErr, "the reporting path skips unreadable files rather than failing")
		assert.Empty(t, states)
	})
}

// TestParseFeedbackFile covers the reporting-oriented parse of the §4d schema
// (SP-140-4 item 4.7): target/status/resolution plus the annotation and
// unresolved-annotation counts the design_assets pending section reports.
func TestParseFeedbackFile(t *testing.T) {
	t.Run("pending-changes-requested", func(t *testing.T) {
		state := parseFeedbackFile("design/feedback/login.json",
			[]byte(fbDoc("design/wireframes/login.svg", "changes-requested", fbAnnotation)))
		require.True(t, state.Valid)
		assert.Equal(t, "design/feedback/login.json", state.Path)
		assert.Equal(t, "design/wireframes/login.svg", state.Target)
		assert.Equal(t, FeedbackStatusChangesRequested, state.Status)
		assert.Equal(t, 1, state.Annotations)
		assert.Equal(t, 0, state.Resolved)
		assert.Equal(t, 1, state.Pending)
		assert.True(t, state.IsPending())
	})

	t.Run("counts-mixed-annotations", func(t *testing.T) {
		anns := `{"id":"a1","at":{"x":0,"y":0},"area":"hierarchy","note":"x","resolved":true,"created":""},` +
			`{"id":"a2","at":{"x":1,"y":1},"area":"contrast","note":"y","resolved":false,"created":""},` +
			`{"id":"a3","at":{"x":0,"y":1},"area":"spacing","note":"z","resolved":false,"created":""}`
		state := parseFeedbackFile("design/feedback/login.json",
			[]byte(fbDoc("design/wireframes/login.svg", "resolved", anns)))
		require.True(t, state.Valid)
		assert.Equal(t, 3, state.Annotations)
		assert.Equal(t, 1, state.Resolved)
		assert.Equal(t, 2, state.Pending)
		// An unresolved annotation keeps the target pending even when the
		// status is not "changes-requested" (§4d predicate).
		assert.True(t, state.IsPending())
	})

	t.Run("fully-resolved-not-pending", func(t *testing.T) {
		ann := `{"id":"a1","at":{"x":0,"y":0},"area":"contrast","note":"x","resolved":true,"created":""}`
		state := parseFeedbackFile("design/feedback/login.json",
			[]byte(fbDoc("design/wireframes/login.svg", "resolved", ann)))
		require.True(t, state.Valid)
		assert.Equal(t, 1, state.Annotations)
		assert.Equal(t, 1, state.Resolved)
		assert.Zero(t, state.Pending)
		assert.False(t, state.IsPending())
	})

	t.Run("changes-requested-with-no-annotations-is-pending", func(t *testing.T) {
		state := parseFeedbackFile("design/feedback/login.json",
			[]byte(fbDoc("design/wireframes/login.svg", "changes-requested", "")))
		require.True(t, state.Valid)
		assert.Zero(t, state.Annotations)
		assert.True(t, state.IsPending(), "a changes-requested status is pending on its own")
	})

	t.Run("invalid-json-not-valid-not-pending", func(t *testing.T) {
		state := parseFeedbackFile("design/feedback/x.json", []byte("{not json"))
		assert.False(t, state.Valid)
		assert.False(t, state.IsPending())
		assert.Zero(t, state.Annotations)
	})

	t.Run("empty-content-not-valid", func(t *testing.T) {
		state := parseFeedbackFile("design/feedback/x.json", nil)
		assert.False(t, state.Valid)
		assert.False(t, state.IsPending())
	})

	t.Run("top-level-array-not-valid", func(t *testing.T) {
		state := parseFeedbackFile("design/feedback/x.json", []byte(`[1,2]`))
		assert.False(t, state.Valid)
	})

	t.Run("top-level-null-not-valid", func(t *testing.T) {
		state := parseFeedbackFile("design/feedback/x.json", []byte(`null`))
		assert.False(t, state.Valid)
	})
}

func TestScanFeedbackDir(t *testing.T) {
	t.Run("missing-dir", func(t *testing.T) {
		states, err := ScanFeedbackDir(t.TempDir())
		require.NoError(t, err)
		require.NotNil(t, states)
		assert.Empty(t, states)
	})

	t.Run("parses-and-sorts", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "design", "feedback")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		// Written out of order to prove the result is path-sorted.
		require.NoError(t, os.WriteFile(filepath.Join(dir, "login.json"),
			[]byte(fbDoc("design/wireframes/login.svg", "changes-requested", fbAnnotation)), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "home.json"),
			[]byte(fbDoc("design/wireframes/home.svg", "resolved", "")), 0o644))
		// A non-JSON file in the directory is ignored.
		require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), []byte("hi"), 0o644))

		states, err := ScanFeedbackDir(root)
		require.NoError(t, err)
		require.Len(t, states, 2)
		assert.Equal(t, "design/feedback/home.json", states[0].Path)
		assert.Equal(t, "design/feedback/login.json", states[1].Path)
		assert.Equal(t, 1, PendingFeedbackCount(states))
		assert.True(t, states[1].IsPending())
		assert.False(t, states[0].IsPending())
	})
}
