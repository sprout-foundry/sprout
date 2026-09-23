package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// SP-140-5 §5c — drift direction core
//
// Drift is signal, not defect: design-ahead (design/ changed, generated/code
// behind) and code-ahead (implementation changed, semantic layer behind) are
// two distinct states with two distinct remedies, always reported as two rows.
// These tests pin the semantics: the two-row shape, the distinct remedies, the
// provenance-hash basis for design-ahead, the design_sync-reuse basis for
// code-ahead, the synced state, and determinism.
// -----------------------------------------------------------------------------

// driftWrite writes rel (slash-separated) under root with parent directories.
func driftWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

const driftTokens = `{
  "color": {
    "brand": {
      "primary": { "$type": "color", "$value": "#0055ff" }
    }
  }
}`

// driftWriteTree writes a minimal design/ tree: one token file, two wireframes,
// one flow. It has no design/generated/ unless the export helper is called.
func driftWriteTree(t *testing.T, root string) {
	t.Helper()
	driftWrite(t, root, "design/tokens/color.tokens.json", driftTokens)
	driftWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><rect data-nav="home"/></svg>`)
	driftWrite(t, root, "design/wireframes/home.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Home</text></svg>`)
	driftWrite(t, root, "design/flows/sign-up.mmd", "flowchart TD\n  login --> home\n")
}

// driftExport writes a real design/generated/ export (genuine §5a provenance
// headers) from the current token sources, so design-ahead's hash comparison
// runs against a genuine banner.
func driftExport(t *testing.T, root string) {
	t.Helper()
	tokens, err := ResolveExportTokens(root)
	require.NoError(t, err)
	artifacts, err := RenderArtifacts(tokens, exportTargetList(ExportTargets))
	require.NoError(t, err)
	require.NoError(t, WriteExportedArtifacts(root, artifacts))
}

func driftRow(t *testing.T, rep *DriftReport, direction string) DriftDirectionRow {
	t.Helper()
	require.NotNil(t, rep)
	for _, r := range rep.Rows {
		if r.Direction == direction {
			return r
		}
	}
	require.FailNow(t, "missing drift row", "direction=%s rows=%v", direction, rep.Rows)
	return DriftDirectionRow{}
}

// -----------------------------------------------------------------------------
// Two-row shape and distinct remedies
// -----------------------------------------------------------------------------

// TestAnalyzeDrift_AlwaysReportsBothDirections pins the §5c two-row contract:
// every report carries design-ahead and code-ahead, in that order, even when
// one (or both) is synced.
func TestAnalyzeDrift_AlwaysReportsBothDirections(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	driftWriteTree(t, root)

	rep := AnalyzeDrift(root, nil)
	require.Len(t, rep.Rows, 2)
	assert.Equal(t, DriftRowDesignAhead, rep.Rows[0].Direction)
	assert.Equal(t, DriftRowCodeAhead, rep.Rows[1].Direction)
}

// TestAnalyzeDrift_DesignAheadAndCodeAheadDistinct pins the AC: both directions
// hold at once, and each carries its own remedy (regenerate theme vs run
// design_sync) and its own next-step tool pointer.
func TestAnalyzeDrift_DesignAheadAndCodeAheadDistinct(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	driftWriteTree(t, root)

	// An export from the current tokens, then move the tokens so the export is
	// stale (design-ahead), plus touched code with an importable semantic delta
	// (code-ahead: a literal token revalue).
	driftExport(t, root)
	driftWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {"brand": {"primary": { "$type": "color", "$value": "#101010" }}}
}`)
	touched := []SyncFileInput{{
		Path:    "webui/src/theme.css",
		Content: []byte(":root { --color-brand-primary: #0055ff; }\n"),
	}}

	rep := AnalyzeDrift(root, touched)

	designAhead := driftRow(t, rep, DriftRowDesignAhead)
	codeAhead := driftRow(t, rep, DriftRowCodeAhead)

	require.True(t, designAhead.Ahead, "design-ahead must hold (tokens moved after export)")
	require.True(t, codeAhead.Ahead, "code-ahead must hold (code has an importable delta)")
	assert.NotEqual(t, designAhead.Remedy, codeAhead.Remedy, "the two directions must have distinct remedies")
	assert.Equal(t, "design_export_tokens", designAhead.NextStep)
	assert.Equal(t, "design_sync", codeAhead.NextStep)
	assert.Contains(t, designAhead.Remedy, "design_export_tokens")
	assert.Contains(t, codeAhead.Remedy, "design_sync")

	// Both are advisory warns — drift is never an error (§5c).
	assert.Equal(t, DriftSeverityWarn, designAhead.Advisory)
	assert.Equal(t, DriftSeverityWarn, codeAhead.Advisory)

	// Concrete magnitudes.
	assert.Equal(t, 3, designAhead.Count, "2 wireframes + 1 flow ahead of the build")
	assert.Equal(t, 1, codeAhead.Count, "one importable semantic delta")
	assert.False(t, rep.Synced)
}

// TestAnalyzeDrift_RemedyStringsAreStable pins the exact remedy/next-step
// vocabulary both handlers reuse, so the two surfaces cannot drift apart.
func TestAnalyzeDrift_RemedyStringsAreStable(t *testing.T) {
	t.Parallel()
	assert.Contains(t, DriftDesignAheadNextStep, "design_export_tokens")
	assert.Contains(t, DriftCodeAheadNextStep, "design_sync")
}

// -----------------------------------------------------------------------------
// Design-ahead
// -----------------------------------------------------------------------------

// TestAnalyzeDrift_DesignAheadRequiresAnExport pins that a tree which has never
// been exported is NOT design-ahead: there is nothing to be behind, and firing
// there would report drift on every fresh tree.
func TestAnalyzeDrift_DesignAheadRequiresAnExport(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	driftWriteTree(t, root)

	rep := AnalyzeDrift(root, nil)
	row := driftRow(t, rep, DriftRowDesignAhead)
	assert.False(t, row.Ahead)
	assert.True(t, row.Synced)
	assert.Empty(t, row.Remedy)
	assert.Equal(t, DriftSeverityInfo, row.Advisory)
	require.NotEmpty(t, rep.Evidence)
	assert.Contains(t, rep.Evidence[0], "No design/generated/ export found")
}

// TestAnalyzeDrift_DesignAheadSyncedWhenExportCurrent pins the provenance
// comparison: an export matching the current tokens is in sync.
func TestAnalyzeDrift_DesignAheadSyncedWhenExportCurrent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	driftWriteTree(t, root)
	driftExport(t, root)

	row := driftRow(t, AnalyzeDrift(root, nil), DriftRowDesignAhead)
	assert.False(t, row.Ahead)
	assert.True(t, row.Synced)
	assert.Empty(t, row.Remedy)
}

// TestAnalyzeDrift_DesignAheadWhenTokensMovedAfterExport pins the exact §5a
// provenance basis: revalue a token after the export and the direction flips
// to ahead with the regeneration remedy.
func TestAnalyzeDrift_DesignAheadWhenTokensMovedAfterExport(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	driftWriteTree(t, root)
	driftExport(t, root)

	// Revalue a token (same shape, different bytes -> different input hash).
	driftWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {"brand": {"primary": { "$type": "color", "$value": "#ff0000" }}}
}`)

	rep := AnalyzeDrift(root, nil)
	row := driftRow(t, rep, DriftRowDesignAhead)
	require.True(t, row.Ahead)
	assert.Equal(t, DriftSeverityWarn, row.Advisory)
	assert.Equal(t, 3, row.Count)
	assert.Contains(t, row.Summary, "3 screen(s)/flow(s) ahead of the build")
	assert.Equal(t, DriftDesignAheadNextStep, row.Remedy)
	assert.Equal(t, "design_export_tokens", row.NextStep)
	assert.True(t, rep.DesignAheadCount == 3)
}

// TestAnalyzeDrift_DesignAheadReExportClearsIt pins that regenerating the theme
// resolves design-ahead: the whole loop is hash-driven and self-clearing.
func TestAnalyzeDrift_DesignAheadReExportClearsIt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	driftWriteTree(t, root)
	driftExport(t, root)
	driftWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {"brand": {"primary": { "$type": "color", "$value": "#ff0000" }}}
}`)
	require.True(t, driftRow(t, AnalyzeDrift(root, nil), DriftRowDesignAhead).Ahead)

	driftExport(t, root)

	row := driftRow(t, AnalyzeDrift(root, nil), DriftRowDesignAhead)
	assert.False(t, row.Ahead)
	assert.True(t, row.Synced)
}

// TestAnalyzeDrift_NoHeaderIsNotStale pins a defensible degradation: a
// generated/ directory whose artifact carries no provenance banner is treated
// as current (the banner convention is the signal; its absence is not itself
// drift).
func TestAnalyzeDrift_NoHeaderIsNotStale(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	driftWriteTree(t, root)
	driftWrite(t, root, "design/generated/tokens.css", ":root { --color-brand-primary: #0055ff; }\n")

	row := driftRow(t, AnalyzeDrift(root, nil), DriftRowDesignAhead)
	assert.False(t, row.Ahead)
}

// -----------------------------------------------------------------------------
// Code-ahead
// -----------------------------------------------------------------------------

// TestAnalyzeDrift_CodeAheadNotAssessableWithoutTouched pins the honest
// degradation: with no touched set the tree alone cannot say the implementation
// is ahead, so the row is synced with evidence saying so.
func TestAnalyzeDrift_CodeAheadNotAssessableWithoutTouched(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	driftWriteTree(t, root)

	rep := AnalyzeDrift(root, nil)
	row := driftRow(t, rep, DriftRowCodeAhead)
	assert.False(t, row.Ahead)
	assert.True(t, row.Synced)
	assert.Empty(t, row.Remedy)
	assert.Equal(t, DriftSeverityInfo, row.Advisory)
	require.NotEmpty(t, rep.Evidence)
	assert.Contains(t, rep.Evidence[len(rep.Evidence)-1], "code-ahead is not assessable")
}

// TestAnalyzeDrift_CodeAheadCountsImportableDeltas pins the reuse of the §5b
// analysis: the count is the safe-to-apply (literal/structural) subset — the
// deltas design_sync can actually import — with inferred proposals called out
// separately in the summary rather than inflating the count.
func TestAnalyzeDrift_CodeAheadCountsImportableDeltas(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	driftWriteTree(t, root)

	touched := []SyncFileInput{{
		Path: "webui/src/theme.css",
		// A literal revalue (importable) plus a raw hex with no token
		// counterpart (inferred proposal; not counted as "to import").
		Content: []byte(":root { --color-brand-primary: #0055ff; }\n.accent { color: #7f00ff; }\n"),
	}}

	rep := AnalyzeDrift(root, touched)
	row := driftRow(t, rep, DriftRowCodeAhead)
	require.True(t, row.Ahead)
	assert.Equal(t, DriftSeverityWarn, row.Advisory)
	assert.Equal(t, 1, row.Count, "only the importable delta counts")
	assert.Contains(t, row.Summary, "inferred proposal(s) to review")
	assert.Equal(t, DriftCodeAheadNextStep, row.Remedy)
	assert.Equal(t, "design_sync", row.NextStep)
	assert.Equal(t, 1, rep.CodeAheadCount)
}

// TestAnalyzeDrift_CodeAheadSyncedWhenNoDeltas pins that a touched set with no
// semantic deltas reports the direction synced (no guilt).
func TestAnalyzeDrift_CodeAheadSyncedWhenNoDeltas(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	driftWriteTree(t, root)

	touched := []SyncFileInput{{
		Path:    "webui/src/util.ts",
		Content: []byte("export const add = (a: number, b: number) => a + b;\n"),
	}}

	rep := AnalyzeDrift(root, touched)
	row := driftRow(t, rep, DriftRowCodeAhead)
	assert.False(t, row.Ahead)
	assert.True(t, row.Synced)
	assert.Empty(t, row.Remedy)
	assert.True(t, rep.Synced)
}

// TestAnalyzeDrift_InferredOnlyIsNotImportable pins that proposals alone do not
// make the implementation "ahead" in the importable sense: an inferred-only
// touched set reports synced, with the proposal count named in evidence.
func TestAnalyzeDrift_InferredOnlyIsNotImportable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	driftWriteTree(t, root)

	touched := []SyncFileInput{{
		Path:    "webui/src/accent.css",
		Content: []byte(".accent { color: #7f00ff; }\n"),
	}}

	rep := AnalyzeDrift(root, touched)
	row := driftRow(t, rep, DriftRowCodeAhead)
	assert.False(t, row.Ahead)
	assert.True(t, row.Synced)
	require.NotEmpty(t, rep.Evidence)
	assert.Contains(t, rep.Evidence[len(rep.Evidence)-1], "inferred proposal(s)")
}

// -----------------------------------------------------------------------------
// Determinism and findings mapping
// -----------------------------------------------------------------------------

// TestAnalyzeDrift_Deterministic pins that two runs over the same workspace and
// input produce identical reports.
func TestAnalyzeDrift_Deterministic(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	driftWriteTree(t, root)
	driftExport(t, root)
	driftWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {"brand": {"primary": { "$type": "color", "$value": "#ff0000" }}}
}`)
	touched := []SyncFileInput{{
		Path:    "webui/src/theme.css",
		Content: []byte(":root { --color-brand-primary: #0055ff; }\n"),
	}}

	first := AnalyzeDrift(root, touched)
	second := AnalyzeDrift(root, touched)
	assert.Equal(t, first, second)
}

// TestDriftDirectionFindings_AheadRowsOnly pins the design_validate mapping:
// only ahead rows become findings (advisory warns, never errors); synced rows
// stay in the structured section without a "nothing to do" finding.
func TestDriftDirectionFindings_AheadRowsOnly(t *testing.T) {
	t.Parallel()
	findings := DriftDirectionFindings(&DriftReport{Rows: []DriftDirectionRow{
		{Direction: DriftRowDesignAhead, Ahead: true, Count: 2, Summary: "2 screens ahead", Remedy: DriftDesignAheadNextStep},
		{Direction: DriftRowCodeAhead, Synced: true},
	}})
	require.Len(t, findings, 1)
	assert.Equal(t, "drift_design_ahead", findings[0].Rule)
	assert.Equal(t, SeverityWarn, findings[0].Severity)
	assert.Equal(t, DirName, findings[0].File)
	assert.Contains(t, findings[0].Message, "2 screens ahead")
	assert.Contains(t, findings[0].Message, "design_export_tokens")
}

// TestDriftDirectionFindings_BothAheadHasDistinctRules pins that the two
// directions produce two distinct rule ids when both hold — the finding-level
// statement of §5c's "reported separately".
func TestDriftDirectionFindings_BothAheadHasDistinctRules(t *testing.T) {
	t.Parallel()
	findings := DriftDirectionFindings(&DriftReport{Rows: []DriftDirectionRow{
		{Direction: DriftRowDesignAhead, Ahead: true, Count: 2, Remedy: DriftDesignAheadNextStep},
		{Direction: DriftRowCodeAhead, Ahead: true, Count: 3, Remedy: DriftCodeAheadNextStep},
	}})
	require.Len(t, findings, 2)
	assert.Equal(t, "drift_design_ahead", findings[0].Rule)
	assert.Equal(t, "drift_code_ahead", findings[1].Rule)
	assert.NotEqual(t, findings[0].Rule, findings[1].Rule)
	for _, f := range findings {
		assert.NotEqual(t, SeverityError, f.Severity, "drift is advisory, never an error")
	}
}

// TestDriftDirectionFindings_NilIsEmpty pins the nil-safe empty result.
func TestDriftDirectionFindings_NilIsEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, DriftDirectionFindings(nil))
}

// TestDriftRuleID pins the stable rule-id mapping.
func TestDriftRuleID(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "drift_design_ahead", DriftRuleID(DriftRowDesignAhead))
	assert.Equal(t, "drift_code_ahead", DriftRuleID(DriftRowCodeAhead))
}

// TestDriftSummaryLine pins the text form of the two-row report: both
// directions named, synced stated explicitly, remedies named when ahead.
func TestDriftSummaryLine(t *testing.T) {
	t.Parallel()

	synced := DriftSummaryLine(&DriftReport{
		Rows: []DriftDirectionRow{
			{Direction: DriftRowDesignAhead, Synced: true},
			{Direction: DriftRowCodeAhead, Synced: true},
		},
		Synced: true,
	})
	assert.Contains(t, synced, "in sync")
	assert.Contains(t, synced, "design-ahead: none")
	assert.Contains(t, synced, "code-ahead: none")

	mixed := DriftSummaryLine(&DriftReport{
		Rows: []DriftDirectionRow{
			{Direction: DriftRowDesignAhead, Ahead: true, Count: 2, NextStep: "design_export_tokens"},
			{Direction: DriftRowCodeAhead, Synced: true},
		},
	})
	assert.Contains(t, mixed, "design-ahead 2 (remedy: design_export_tokens)")
	assert.Contains(t, mixed, "code-ahead none")
}

// TestDriftSummaryLine_NilIsEmpty pins the nil-safe empty string.
func TestDriftSummaryLine_NilIsEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, DriftSummaryLine(nil))
}

// TestDriftDirections_ReturnsCopy pins that the canonical order accessor cannot
// be mutated by a caller.
func TestDriftDirections_ReturnsCopy(t *testing.T) {
	t.Parallel()
	got := DriftDirections()
	require.Equal(t, []string{DriftRowDesignAhead, DriftRowCodeAhead}, got)
	got[0] = "mutated"
	assert.Equal(t, DriftRowDesignAhead, DriftDirections()[0])
}

// TestProvenanceSourceHash pins the banner parser: it reads the §5a source-hash
// line out of both the block-comment (CSS) and line-comment (TS) header styles,
// and returns "" for an artifact without one.
func TestProvenanceSourceHash(t *testing.T) {
	t.Parallel()
	css := provenanceHeader(ExportTargetCSS, "fnv1a64:deadbeefdeadbeef")
	assert.Equal(t, "fnv1a64:deadbeefdeadbeef", provenanceSourceHash(css))

	ts := provenanceHeader(ExportTargetTS, "fnv1a64:0011223344556677")
	assert.Equal(t, "fnv1a64:0011223344556677", provenanceSourceHash(ts))

	assert.Empty(t, provenanceSourceHash(":root { --x: 1; }\n"))
}

// TestCurrentTokenInputHash_MatchesExport pins that the drift recomputation
// uses exactly the §5a hash the export writes.
func TestCurrentTokenInputHash_MatchesExport(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	driftWriteTree(t, root)

	sources, err := tokenExportSources(root)
	require.NoError(t, err)
	require.NotEmpty(t, sources)

	current, err := currentTokenInputHash(root)
	require.NoError(t, err)
	assert.Equal(t, exportTokenInputHash(sources), current)

	// A read of the export's own banner must record the same hash.
	driftExport(t, root)
	data, err := os.ReadFile(filepath.Join(root, DirName, GeneratedSubdir, ExportFilenames[ExportTargetCSS]))
	require.NoError(t, err)
	assert.Equal(t, current, provenanceSourceHash(string(data)))
}

// TestCurrentTokenInputHash_NoTokens pins the empty-tree degradation.
func TestCurrentTokenInputHash_NoTokens(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	hash, err := currentTokenInputHash(root)
	require.NoError(t, err)
	assert.Empty(t, hash)
}
