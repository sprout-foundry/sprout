package design

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// SP-140-5 §5b — design_sync analyze core (TODO item 5.3)
//
// The analyze half is pure analysis (plus a read of the design/ tree): it takes
// the touched UI code files and returns the structured sync report. These tests
// use the §5b fixtures and assert the report shape: the delta fields (basis,
// kind, confidence, design files), the literal→DTCG mapping, the structural
// wireframe/flow proposals, the inferred proposals, and the determinism the
// report guarantees.
// -----------------------------------------------------------------------------

// syncWrite writes rel (slash-separated) under root with parent directories.
func syncWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// syncFixtureTree is a minimal, valid design/ tree the analyze fixtures
// cross-reference: one colour token, two wireframes, one flow, one screen.
const syncFixtureTokens = `{
  "color": {
    "brand": {
      "primary": { "$type": "color", "$value": "#0055ff" }
    }
  },
  "dimension": {
    "space": {
      "medium": { "$type": "dimension", "$value": "8px" }
    }
  }
}`

const syncFixtureWireframe = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <rect id="go" data-nav="home" />
</svg>`

func syncWriteFixtureTree(t *testing.T, root string) {
	t.Helper()
	syncWrite(t, root, "design/tokens/color.tokens.json", syncFixtureTokens)
	syncWrite(t, root, "design/wireframes/login.svg", syncFixtureWireframe)
	syncWrite(t, root, "design/wireframes/home.svg", syncFixtureWireframe)
	syncWrite(t, root, "design/flows/sign-up.mmd", "flowchart TD\n  login --> home\n")
	syncWrite(t, root, "design/screens/login.html", `<style>.root{width:390px;}</style>`)
}

// syncDeltaFor returns the first delta matching a predicate, or fails.
func syncDeltaFor(t *testing.T, rep *SyncReport, pred func(SyncDelta) bool) SyncDelta {
	t.Helper()
	for _, d := range rep.Deltas {
		if pred(d) {
			return d
		}
	}
	require.FailNow(t, "no matching delta", "report has %d deltas: %s", len(rep.Deltas), syncDeltaDigest(rep))
	return SyncDelta{}
}

// syncDeltaDigest renders every delta for a failure message.
func syncDeltaDigest(rep *SyncReport) string {
	if rep == nil {
		return "<nil>"
	}
	var parts []string
	for _, d := range rep.Deltas {
		parts = append(parts, string(d.Basis)+"/"+d.Kind+" "+d.Delta)
	}
	return strings.Join(parts, " | ")
}

// -----------------------------------------------------------------------------
// Report shape
// -----------------------------------------------------------------------------

func TestAnalyzeTouchedFiles_EmptyInputIsAnEmptyReport(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root})
	require.NoError(t, err)
	require.NotNil(t, rep)
	assert.Equal(t, 0, rep.TouchedCount)
	assert.Equal(t, 0, rep.DeltaCount)
	assert.Empty(t, rep.Deltas)
	assert.True(t, rep.WritesNothing, "analyze mode writes nothing (§5e)")
	assert.Equal(t, "design/tokens", rep.TokensPath)
	assert.Equal(t, "design/wireframes", rep.WireframeDir)
	assert.Equal(t, "design/flows", rep.FlowsDir)
	// Counts maps are always present so the JSON shape is stable.
	require.NotNil(t, rep.ByBasis)
	require.NotNil(t, rep.ByKind)
}

func TestAnalyzeTouchedFiles_NoDesignTreeIsGuidanceNotError(t *testing.T) {
	root := t.TempDir() // no design/

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/theme.css", Content: []byte(".btn{background:#ff00ff;}\n")},
	}})
	require.NoError(t, err, "a missing design/ tree is a reportable state, not a failure")
	require.NotEmpty(t, rep.Notes)
	assert.Contains(t, strings.Join(rep.Notes, " "), "No design/ tree")
	// The raw hex is still detected — as an inferred proposal, since there is
	// no token to switch to.
	d := syncDeltaFor(t, rep, func(d SyncDelta) bool { return d.Basis == DeltaBasisInferred })
	assert.Equal(t, ConfidenceLow, d.Confidence)
	assert.False(t, d.SafeToApply)
	assert.True(t, d.Proposal)
}

// -----------------------------------------------------------------------------
// §5b fixture 1 — token rename/revalue in code → literal delta naming the DTCG
// file and entry
// -----------------------------------------------------------------------------

func TestAnalyzeTouchedFiles_LiteralTokenRevalueNamesDTCGEntry(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/theme.css", Content: []byte(":root{ --color-brand-primary: #ff0000; }\n")},
	}})
	require.NoError(t, err)

	d := syncDeltaFor(t, rep, func(d SyncDelta) bool { return d.Basis == DeltaBasisLiteral })
	assert.Equal(t, DeltaKindToken, d.Kind)
	assert.Equal(t, ConfidenceHigh, d.Confidence)
	assert.True(t, d.SafeToApply, "literal deltas are safe to apply mechanically")
	assert.False(t, d.Proposal)
	// It names the DTCG file and entry, 1:1.
	assert.Equal(t, "color.brand.primary", d.Token)
	assert.Equal(t, "color.brand.primary", d.TokenEntry)
	assert.Equal(t, "design/tokens/color.tokens.json", d.TokenFile)
	assert.Contains(t, d.Delta, "color.brand.primary")
	assert.Contains(t, d.Delta, "revalued")
	// The design files it would touch include the token source and the §5a
	// generated artifacts (§5f: a token change means regenerate the theme).
	assert.Contains(t, d.DesignFiles, "design/tokens/color.tokens.json")
	assert.Contains(t, d.DesignFiles, "design/generated/tokens.css")
	assert.Equal(t, []string{"src/theme.css"}, d.CodeFiles)
}

// TestAnalyzeTouchedFiles_LiteralTokenRename is the rename half of the §5b
// fixture: the code uses the *renamed* var, and the report maps it to the DTCG
// entry by the generated CSS-var name the export machinery owns.
func TestAnalyzeTouchedFiles_LiteralTokenRename(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	// Before: code declared --color-brand-primary. After the dev turn it
	// declares the same value under a var that still maps to the same DTCG
	// entry (the rename is in the DTCG tier's spelling, not the value).
	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/theme.css", Content: []byte(":root{ --color-brand-primary: #0055ff; }\n")},
	}})
	require.NoError(t, err)

	d := syncDeltaFor(t, rep, func(d SyncDelta) bool { return d.Basis == DeltaBasisLiteral })
	assert.Equal(t, "color.brand.primary", d.TokenEntry, "the entry is named")
	assert.Equal(t, "design/tokens/color.tokens.json", d.TokenFile)
	assert.True(t, d.SafeToApply)
	// Values match, so this is a declaration (not a revalue) — the report
	// still surfaces it, because the code now carries the var.
	assert.NotEmpty(t, d.Delta)
}

// TestAnalyzeTouchedFiles_UnknownVarRefIsAnInferredProposal covers the other
// literal-pass direction: code references a CSS var with no DTCG counterpart.
func TestAnalyzeTouchedFiles_UnknownVarRefIsAnInferredProposal(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/card.tsx", Content: []byte("export const s = { color: 'var(--color-card-border)' };\n")},
	}})
	require.NoError(t, err)

	d := syncDeltaFor(t, rep, func(d SyncDelta) bool { return d.Basis == DeltaBasisInferred })
	assert.Equal(t, DeltaKindToken, d.Kind)
	assert.Equal(t, "new-token", d.ProposalKind)
	assert.Equal(t, ConfidenceLow, d.Confidence)
	assert.False(t, d.SafeToApply)
	assert.Contains(t, d.Delta, "--color-card-border")
}

// -----------------------------------------------------------------------------
// §5b fixture 2 — new route + screen component → structural delta proposing a
// wireframe stem and flow edge
// -----------------------------------------------------------------------------

func TestAnalyzeTouchedFiles_NewRouteProposesWireframeAndFlow(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	router := `export const routes = [
  { path: "/login", element: <Login /> },
  { path: "/check-deposit", element: <CheckDeposit /> },
];
`
	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/routes.tsx", Content: []byte(router)},
	}})
	require.NoError(t, err)

	wf := syncDeltaFor(t, rep, func(d SyncDelta) bool { return d.Kind == DeltaKindWireframe })
	assert.Equal(t, DeltaBasisStructural, wf.Basis)
	assert.Equal(t, ConfidenceMedium, wf.Confidence)
	assert.True(t, wf.SafeToApply, "structural deltas are the safe subset")
	assert.False(t, wf.Proposal)
	assert.Equal(t, "check-deposit", wf.WireframeStem)
	assert.True(t, SlugMatches(wf.WireframeStem), "the proposed stem is a legal wireframe name")
	assert.Equal(t, FlowDraftStatus, wf.Status)
	assert.Contains(t, wf.DesignFiles, "design/wireframes/check-deposit.svg")
	assert.Contains(t, wf.Delta, "/check-deposit")

	flow := syncDeltaFor(t, rep, func(d SyncDelta) bool { return d.Kind == DeltaKindFlow })
	assert.Equal(t, DeltaBasisStructural, flow.Basis)
	assert.True(t, flow.SafeToApply)
	assert.NotEmpty(t, flow.FlowEdge, "the edge is proposed")
	assert.Contains(t, flow.FlowEdge, "check-deposit")
	assert.Contains(t, flow.DesignFiles, flow.FlowFile)
	assert.Equal(t, FlowDraftStatus, flow.Status)

	// /login already has a wireframe: not re-proposed.
	for _, d := range rep.Deltas {
		assert.NotEqual(t, "login", d.WireframeStem, "an existing wireframe must not be re-proposed")
	}
}

// TestAnalyzeTouchedFiles_NavTargetWithoutWireframe covers the "nav target
// moved" structural case: a link to a screen with no wireframe.
func TestAnalyzeTouchedFiles_NavTargetWithoutWireframe(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/Nav.tsx", Content: []byte(`<nav><Link to="/settings">Settings</Link></nav>` + "\n")},
	}})
	require.NoError(t, err)

	d := syncDeltaFor(t, rep, func(d SyncDelta) bool { return d.WireframeStem == "settings" })
	assert.Equal(t, DeltaKindWireframe, d.Kind)
	assert.Equal(t, DeltaBasisStructural, d.Basis)
	assert.Contains(t, d.DesignFiles, "design/wireframes/settings.svg")
}

// TestAnalyzeTouchedFiles_ExistingNavTargetIsNotReproposed is the negative
// control: a nav target whose wireframe already exists produces nothing.
func TestAnalyzeTouchedFiles_ExistingNavTargetIsNotReproposed(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/Nav.tsx", Content: []byte(`<nav><Link to="/home">Home</Link></nav>` + "\n")},
	}})
	require.NoError(t, err)
	assert.Empty(t, rep.Deltas)
}

// TestAnalyzeTouchedFiles_DeliveredScreenIsNamedInTheWireframeDelta covers the
// design-ahead half of a structural delta: a screen exists for the stem, so the
// delta names it as adoption context.
func TestAnalyzeTouchedFiles_DeliveredScreenIsNamedInTheWireframeDelta(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)
	syncWrite(t, root, "design/screens/settings.html", `<style>.root{width:390px;}</style>`)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/routes.tsx", Content: []byte(`{ path: "/settings", element: <Settings /> }` + "\n")},
	}})
	require.NoError(t, err)

	d := syncDeltaFor(t, rep, func(d SyncDelta) bool {
		return d.Kind == DeltaKindWireframe && d.WireframeStem == "settings"
	})
	assert.Contains(t, d.DesignFiles, "design/wireframes/settings.svg")
	assert.Contains(t, d.DesignFiles, "design/screens/settings.html")
	assert.Contains(t, d.Delta, "delivered screen")
}

// -----------------------------------------------------------------------------
// §5b fixture 3 — raw-hex styling → inferred proposal (new token or
// switch-to-existing) with lower confidence
// -----------------------------------------------------------------------------

func TestAnalyzeTouchedFiles_RawHexSwitchToExistingProposal(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	// The hex equals color.brand.primary's value, so the proposal is to
	// switch to the existing token rather than invent one.
	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/Badge.tsx", Content: []byte(`export const Badge = () => <div style={{color:'#0055ff'}} />;` + "\n")},
	}})
	require.NoError(t, err)

	d := syncDeltaFor(t, rep, func(d SyncDelta) bool { return d.Basis == DeltaBasisInferred })
	assert.Equal(t, DeltaKindToken, d.Kind)
	assert.Equal(t, ConfidenceLow, d.Confidence, "inferred deltas carry a lower confidence")
	assert.False(t, d.SafeToApply, "inferred deltas are never auto-applied")
	assert.True(t, d.Proposal)
	assert.Equal(t, "switch-to-existing", d.ProposalKind)
	assert.Equal(t, "color.brand.primary", d.Candidate)
	assert.Contains(t, d.Delta, "switch to the existing token color.brand.primary")
	assert.Contains(t, d.DesignFiles, "design/tokens/color.tokens.json")
}

func TestAnalyzeTouchedFiles_RawHexNewTokenProposal(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/Badge.tsx", Content: []byte(`export const Badge = () => <div style={{color:'#ff00ff'}} />;` + "\n")},
	}})
	require.NoError(t, err)

	d := syncDeltaFor(t, rep, func(d SyncDelta) bool { return d.Basis == DeltaBasisInferred })
	assert.Equal(t, "new-token", d.ProposalKind)
	assert.Equal(t, ConfidenceLow, d.Confidence)
	assert.False(t, d.SafeToApply)
	assert.True(t, d.Proposal)
	assert.NotEmpty(t, d.Proposed)
	assert.Empty(t, d.Candidate)
	assert.Contains(t, d.Delta, "#ff00ff")
}

func TestAnalyzeTouchedFiles_MagicSpacingProposal(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/card.css", Content: []byte(".card{ padding: 13px; }\n")},
	}})
	require.NoError(t, err)

	d := syncDeltaFor(t, rep, func(d SyncDelta) bool { return d.Basis == DeltaBasisInferred })
	assert.Equal(t, DeltaKindToken, d.Kind)
	assert.Equal(t, "new-token", d.ProposalKind)
	assert.Contains(t, d.Delta, "13px")
	assert.True(t, strings.HasPrefix(d.Proposed, "dimension."), "a spacing proposal lands in a dimension group")
	assert.Empty(t, d.Candidate)
}

// TestAnalyzeTouchedFiles_ValueBackedByVarDeclIsNotDoubleReported proves the
// inferred pass does not re-report a value the literal pass already owns: a hex
// that is the declared value of a var mapping to a DTCG token yields exactly
// one delta.
func TestAnalyzeTouchedFiles_ValueBackedByVarDeclIsNotDoubleReported(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/theme.css", Content: []byte(":root{--color-brand-primary:#ff0000;}\n")},
	}})
	require.NoError(t, err)

	require.Len(t, rep.Deltas, 1, "one declared var → one literal delta: %s", syncDeltaDigest(rep))
	assert.Equal(t, DeltaBasisLiteral, rep.Deltas[0].Basis)
}

// -----------------------------------------------------------------------------
// Determinism
// -----------------------------------------------------------------------------

func TestAnalyzeTouchedFiles_Deterministic(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	inputs := []SyncFileInput{
		{Path: "src/theme.css", Content: []byte(":root{--color-brand-primary:#ff0000;--x:1px;}\n.btn{padding:13px;background:#ff00ff;}\n")},
		{Path: "src/routes.tsx", Content: []byte(`{ path: "/check-deposit", element: <CheckDeposit /> }` + "\n")},
		{Path: "src/Nav.tsx", Content: []byte(`<Link to="/settings">S</Link>` + "\n")},
	}

	var reference string
	for run := 0; run < 10; run++ {
		// Shuffle the touched order each run: the report must not depend on it.
		shuffled := make([]SyncFileInput, len(inputs))
		copy(shuffled, inputs)
		if run%2 == 1 {
			shuffled[0], shuffled[2] = shuffled[2], shuffled[0]
		}
		rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: shuffled})
		require.NoError(t, err)
		data, err := json.Marshal(rep)
		require.NoError(t, err)
		got := string(data)
		if run == 0 {
			reference = got
			continue
		}
		assert.Equal(t, reference, got, "run %d drifted from the reference report", run+1)
	}
}

func TestAnalyzeTouchedFiles_DedupsRepeatedValues(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/a.css", Content: []byte(".a{padding:13px;padding:13px;}\n")},
	}})
	require.NoError(t, err)
	// The same magic value repeated in one file yields one delta.
	count := 0
	for _, d := range rep.Deltas {
		if strings.Contains(d.Delta, "13px") {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestAnalyzeTouchedFiles_DuplicateTouchedPathKeepsFirstContent(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/theme.css", Content: []byte(":root{--color-brand-primary:#ff0000;}\n")},
		{Path: "src/theme.css", Content: []byte(":root{--color-brand-primary:#00ff00;}\n")},
	}})
	require.NoError(t, err)
	assert.Equal(t, 1, rep.TouchedCount)
	d := syncDeltaFor(t, rep, func(d SyncDelta) bool { return d.Basis == DeltaBasisLiteral })
	assert.Contains(t, d.Evidence, "#ff0000", "the first content wins")
}

func TestAnalyzeTouchedFiles_SkippedPathsReported(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/missing.css", Content: nil},
		{Path: "", Content: []byte("x")},
	}})
	require.NoError(t, err)
	assert.Equal(t, 0, rep.TouchedCount)
	assert.Equal(t, []string{"src/missing.css"}, rep.Skipped)
}

func TestAnalyzeTouchedFiles_AbsolutePathsAreMadeRelative(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: filepath.Join(root, "src", "theme.css"), Content: []byte(":root{--color-brand-primary:#ff0000;}\n")},
	}})
	require.NoError(t, err)
	d := syncDeltaFor(t, rep, func(d SyncDelta) bool { return d.Basis == DeltaBasisLiteral })
	assert.Equal(t, []string{"src/theme.css"}, d.CodeFiles, "no absolute path leaks into the report")
}

// -----------------------------------------------------------------------------
// Ordering and the apply contract
// -----------------------------------------------------------------------------

func TestAnalyzeTouchedFiles_DeltasAreCanonicallyOrdered(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/theme.css", Content: []byte(":root{--color-brand-primary:#ff0000;}\n.btn{background:#ff00ff;padding:13px;}\n")},
		{Path: "src/routes.tsx", Content: []byte(`{ path: "/check-deposit", element: <CheckDeposit /> }` + "\n")},
	}})
	require.NoError(t, err)

	// Literal, then structural, then inferred.
	var bases []string
	for _, d := range rep.Deltas {
		bases = append(bases, string(d.Basis))
	}
	want := []string{"literal", "structural", "structural", "inferred", "inferred"}
	assert.Equal(t, want, bases, "bases follow the canonical order")

	// The report is a sorted, stable work list.
	sorted := append([]SyncDelta(nil), rep.Deltas...)
	sort.SliceStable(sorted, func(i, j int) bool { return CompareDeltas(sorted[i], sorted[j]) })
	assert.Equal(t, sorted, rep.Deltas)
}

// TestSyncReport_IsJSONShapeStableForApply pins the report contract apply mode
// (item 5.4) consumes: every delta carries the §5b fields and the design files
// it would touch, and only literal/structural deltas are marked safe.
func TestSyncReport_IsJSONShapeStableForApply(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/theme.css", Content: []byte(":root{--color-brand-primary:#ff0000;--unknown-x:1;}\n.btn{background:#ff00ff;}\n")},
		{Path: "src/routes.tsx", Content: []byte(`{ path: "/check-deposit", element: <CheckDeposit /> }` + "\n")},
	}})
	require.NoError(t, err)

	data, err := json.Marshal(rep)
	require.NoError(t, err)
	var generic map[string]any
	require.NoError(t, json.Unmarshal(data, &generic))
	for _, key := range []string{"tokensPath", "wireframesPath", "flowsPath",
		"touchedCount", "deltaCount", "byBasis", "byKind", "deltas", "writesNothing", "nextStep"} {
		assert.Contains(t, generic, key, "report must carry %q", key)
	}

	deltas, ok := generic["deltas"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, deltas)
	for _, raw := range deltas {
		d, ok := raw.(map[string]any)
		require.True(t, ok)
		for _, key := range []string{"delta", "kind", "basis", "confidence", "designFiles", "safeToApply", "codeFiles"} {
			assert.Contains(t, d, key, "delta must carry %q", key)
		}
		basis, _ := d["basis"].(string)
		safe, _ := d["safeToApply"].(bool)
		if basis == "inferred" {
			assert.False(t, safe, "an inferred delta is never safe to apply")
			assert.Equal(t, true, d["proposal"], "an inferred delta is a proposal")
		} else {
			assert.True(t, safe, "literal/structural deltas are safe: %v", d["delta"])
		}
		// Design files is always an array (possibly empty), so apply can range
		// it without a nil check.
		assert.IsType(t, []any{}, d["designFiles"], "designFiles is always an array")
	}
}

func TestRenderSyncSummary(t *testing.T) {
	root := t.TempDir()
	syncWriteFixtureTree(t, root)

	rep, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: []SyncFileInput{
		{Path: "src/theme.css", Content: []byte(":root{--color-brand-primary:#ff0000;}\n.btn{background:#ff00ff;}\n")},
	}})
	require.NoError(t, err)

	summary := RenderSyncSummary(rep)
	assert.Contains(t, summary, "design_sync (analyze)")
	assert.Contains(t, summary, "1 literal")
	assert.Contains(t, summary, "1 inferred")
	assert.Contains(t, summary, "safe to apply")

	assert.Equal(t, "design_sync: no report.", RenderSyncSummary(nil))
}

// -----------------------------------------------------------------------------
// Convention helpers
// -----------------------------------------------------------------------------

func TestScreenStemFromRoute(t *testing.T) {
	cases := map[string]string{
		"/login":            "login",
		"/check-deposit":    "check-deposit",
		"/settings/profile": "settings-profile",
		"/":                 "",
		"/:id":              "",
		"/users/:userId":    "",
	}
	for route, want := range cases {
		assert.Equal(t, want, screenStemFromRoute(route), "route %q", route)
	}
}

func TestSlugMatches(t *testing.T) {
	assert.True(t, SlugMatches("check-deposit"))
	assert.True(t, SlugMatches("login2"))
	assert.False(t, SlugMatches("Check-Deposit"))
	assert.False(t, SlugMatches("check_deposit"))
	assert.False(t, SlugMatches(""))
	assert.False(t, SlugMatches("-leading"))
}

func TestProposeFlowEdgePrefersParentSegment(t *testing.T) {
	tree := syncTree{
		wireframeStems: map[string]bool{"login": true, "home": true, "settings": true},
		flowEdges:      map[string]bool{"login --> home": true},
		flowFiles:      []string{"design/flows/sign-up.mmd"},
	}
	edge, file := proposeFlowEdge("settings-profile", tree)
	assert.Equal(t, "settings --> settings-profile", edge)
	assert.Equal(t, "design/flows/sign-up.mmd", file)

	// No parent segment → a deterministic fallback source.
	edge, file = proposeFlowEdge("check-deposit", tree)
	assert.Equal(t, "home --> check-deposit", edge)
	assert.Equal(t, "design/flows/sign-up.mmd", file)
}

func TestProposeFlowEdgeNoWireframesYieldsNoEdge(t *testing.T) {
	tree := syncTree{flowEdges: map[string]bool{}}
	edge, _ := proposeFlowEdge("check-deposit", tree)
	assert.Empty(t, edge, "no source screen means no proposed edge")
}

func TestTokenPathFromCSSVar(t *testing.T) {
	assert.Equal(t, "color.brand.primary", tokenPathFromCSSVar("color-brand-primary"))
	assert.Equal(t, "space.md", tokenPathFromCSSVar("space_md"))
}

func TestValueSlug(t *testing.T) {
	assert.Equal(t, "ff00ff", valueSlug("#ff00ff"))
	assert.Equal(t, "v13", valueSlug("13px"))
	assert.Equal(t, "value", valueSlug("---"))
}
