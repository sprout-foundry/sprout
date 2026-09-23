//go:build !js

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// ---------------------------------------------------------------------------
// design_sync handler tests (SP-140-5 §5b — ANALYZE mode)
//
// The pure analysis is unit tested in pkg/design/sync_test.go. These tests
// cover the ToolHandler seam: argument resolution, the touched-file default via
// the sanctioned ListChanges seam and the explicit `files` override, Gate-1
// prechecks (including off-workspace denial), the structured report, determinism
// through the real filesystem, and the apply-mode happy paths, proposals, §5e
// design/-confinement, and Gate-1 on write paths (item 5.4).
// ---------------------------------------------------------------------------

// dsWrite writes rel (slash-separated) under root with parent directories.
func dsWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// dsFixtureTree populates root with the §5b fixture design/ tree.
func dsFixtureTree(t *testing.T, root string) {
	t.Helper()
	dsWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": { "brand": { "primary": { "$type": "color", "$value": "#0055ff" } } },
  "dimension": { "space": { "medium": { "$type": "dimension", "$value": "8px" } } }
}`)
	dsWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><rect id="go" data-nav="home"/></svg>`)
	dsWrite(t, root, "design/wireframes/home.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"/>`)
	dsWrite(t, root, "design/flows/sign-up.mmd", "flowchart TD\n  login --> home\n")
	dsWrite(t, root, "design/screens/login.html", `<style>.root{width:390px;}</style>`)
}

// dsListChangesOutput renders a list_changes-shaped JSON envelope for the given
// paths, so the default-touched-set path can be exercised without an agent.
func dsListChangesOutput(paths ...string) string {
	type fileEntry struct {
		Path string `json:"path"`
		Op   string `json:"op"`
		Tool string `json:"tool"`
	}
	type env struct {
		RevisionID string      `json:"revision_id"`
		Enabled    bool        `json:"enabled"`
		Count      int         `json:"count"`
		Files      []fileEntry `json:"files"`
	}
	e := env{RevisionID: "rev-1", Enabled: true, Count: len(paths)}
	for _, p := range paths {
		e.Files = append(e.Files, fileEntry{Path: p, Op: "modified", Tool: "edit_file"})
	}
	b, _ := json.Marshal(e)
	return string(b)
}

// dsEnvWithChanges builds a ToolEnv whose ListChanges seam returns the given
// list_changes JSON payload.
func dsEnvWithChanges(t *testing.T, root, payload string) ToolEnv {
	t.Helper()
	env := newTestEnv(t, root)
	env.ToolFuncs = &ToolFuncSet{
		ListChanges: func(context.Context, map[string]any) (string, error) {
			return payload, nil
		},
	}
	return env
}

// dsOutput extracts the structured result.
func dsOutput(t *testing.T, res ToolResult) designSyncOutput {
	t.Helper()
	out, ok := res.StructuredOut.(designSyncOutput)
	require.True(t, ok, "StructuredOut must be a designSyncOutput, got %T", res.StructuredOut)
	require.NotNil(t, out.Report)
	return out
}

// dsDeltaFor returns the first delta matching a predicate.
func dsDeltaFor(t *testing.T, out designSyncOutput, pred func(design.SyncDelta) bool) design.SyncDelta {
	t.Helper()
	for _, d := range out.Report.Deltas {
		if pred(d) {
			return d
		}
	}
	require.FailNow(t, "no matching delta", "deltas: %+v", out.Report.Deltas)
	return design.SyncDelta{}
}

// ---------------------------------------------------------------------------
// Definition / Validate
// ---------------------------------------------------------------------------

func TestDesignSyncHandler_NameAndDefinition(t *testing.T) {
	t.Parallel()
	h := &designSyncHandler{}

	require.Equal(t, "design_sync", h.Name())

	def := h.Definition()
	require.Equal(t, "design_sync", def.Name)
	require.NotEmpty(t, def.Description)
	for _, want := range []string{"analyze", "apply", "literal", "structural", "inferred", "files"} {
		require.Contains(t, def.Description, want, "the description must name %q", want)
	}
	require.Empty(t, def.Required, "files and mode are both optional")

	params := map[string]bool{}
	for _, p := range def.Parameters {
		params[p.Name] = true
	}
	assert.True(t, params["files"])
	assert.True(t, params["mode"])
}

func TestDesignSyncHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &designSyncHandler{}

	require.NoError(t, h.Validate(nil))
	require.NoError(t, h.Validate(map[string]any{}))
	require.NoError(t, h.Validate(map[string]any{"files": "src/a.css,src/b.ts"}))
	require.NoError(t, h.Validate(map[string]any{"files": []any{"src/a.css", "src/b.ts"}}))
	require.NoError(t, h.Validate(map[string]any{"mode": "analyze"}))
	assert.Error(t, h.Validate(map[string]any{"mode": 5}))
	assert.Error(t, h.Validate(map[string]any{"files": 5}))
}

// ---------------------------------------------------------------------------
// §5b fixture 1 — token rename/revalue → literal delta naming the DTCG entry
// ---------------------------------------------------------------------------

func TestDesignSyncHandler_LiteralTokenRevalue(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{ --color-brand-primary: #ff0000; }\n")
	h := &designSyncHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "src/theme.css"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := dsOutput(t, res)
	assert.Equal(t, "argument", out.TouchedSource)
	assert.Equal(t, []string{"src/theme.css"}, out.TouchedFiles)
	assert.True(t, out.Report.WritesNothing, "analyze mode writes nothing")

	d := dsDeltaFor(t, out, func(d design.SyncDelta) bool { return d.Basis == design.DeltaBasisLiteral })
	assert.Equal(t, design.DeltaKindToken, d.Kind)
	assert.Equal(t, design.ConfidenceHigh, d.Confidence)
	assert.True(t, d.SafeToApply)
	assert.Equal(t, "color.brand.primary", d.Token)
	assert.Equal(t, "color.brand.primary", d.TokenEntry)
	assert.Equal(t, "design/tokens/color.tokens.json", d.TokenFile)
	assert.Contains(t, d.Delta, "color.brand.primary")

	// §5e: every design file a delta names is inside design/.
	for _, df := range d.DesignFiles {
		assert.True(t, designSyncDesignFile(df), "design file %q must be inside design/", df)
	}
	assert.Contains(t, res.Output, "design_sync (analyze)")
}

// ---------------------------------------------------------------------------
// §5b fixture 2 — new route + screen component → structural delta
// ---------------------------------------------------------------------------

func TestDesignSyncHandler_NewRouteProposesWireframeAndFlow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/routes.tsx", `export const routes = [
  { path: "/login", element: <Login /> },
  { path: "/check-deposit", element: <CheckDeposit /> },
];
`)
	h := &designSyncHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "src/routes.tsx"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	out := dsOutput(t, res)

	wf := dsDeltaFor(t, out, func(d design.SyncDelta) bool {
		return d.Kind == design.DeltaKindWireframe && d.WireframeStem == "check-deposit"
	})
	assert.Equal(t, design.DeltaBasisStructural, wf.Basis)
	assert.Equal(t, design.ConfidenceMedium, wf.Confidence)
	assert.True(t, wf.SafeToApply)
	assert.False(t, wf.Proposal)
	assert.Equal(t, design.FlowDraftStatus, wf.Status, "a §5b-created wireframe is a draft")
	assert.Contains(t, wf.DesignFiles, "design/wireframes/check-deposit.svg")

	flow := dsDeltaFor(t, out, func(d design.SyncDelta) bool { return d.Kind == design.DeltaKindFlow })
	assert.Equal(t, design.DeltaBasisStructural, flow.Basis)
	assert.True(t, flow.SafeToApply)
	assert.NotEmpty(t, flow.FlowEdge)
	assert.Contains(t, flow.FlowEdge, "check-deposit")
	assert.True(t, designSyncDesignFile(flow.FlowFile))
}

// ---------------------------------------------------------------------------
// §5b fixture 3 — raw-hex styling → inferred proposal, lower confidence
// ---------------------------------------------------------------------------

func TestDesignSyncHandler_RawHexIsInferredProposal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/Badge.tsx", "export const Badge = () => <div style={{color:'#ff00ff'}} />;\n")
	h := &designSyncHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "src/Badge.tsx"})
	require.NoError(t, err)
	out := dsOutput(t, res)

	d := dsDeltaFor(t, out, func(d design.SyncDelta) bool { return d.Basis == design.DeltaBasisInferred })
	assert.Equal(t, design.ConfidenceLow, d.Confidence)
	assert.False(t, d.SafeToApply, "an inferred delta is never auto-applied")
	assert.True(t, d.Proposal)
	assert.NotEmpty(t, d.ProposalKind)
	assert.Contains(t, d.Delta, "#ff00ff")
	// Apply must not auto-create tokens for inferred deltas; the report says so
	// via SafeToApply=false and the summary counts the proposals.
	assert.Contains(t, res.Output, "proposal")
}

func TestDesignSyncHandler_RawHexSwitchToExisting(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/Badge.tsx", "export const Badge = () => <div style={{color:'#0055ff'}} />;\n")
	h := &designSyncHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "src/Badge.tsx"})
	require.NoError(t, err)
	out := dsOutput(t, res)

	d := dsDeltaFor(t, out, func(d design.SyncDelta) bool { return d.Basis == design.DeltaBasisInferred })
	assert.Equal(t, "switch-to-existing", d.ProposalKind)
	assert.Equal(t, "color.brand.primary", d.Candidate)
	assert.Contains(t, d.Delta, "switch to the existing token")
}

// ---------------------------------------------------------------------------
// Touched-file set: default (ListChanges) and explicit override
// ---------------------------------------------------------------------------

func TestDesignSyncHandler_DefaultsToChangeTrackerSet(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{ --color-brand-primary: #ff0000; }\n")
	h := &designSyncHandler{}

	env := dsEnvWithChanges(t, root, dsListChangesOutput("src/theme.css"))
	res, err := h.Execute(newTestCtx(root), env, map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := dsOutput(t, res)
	assert.Equal(t, "changes", out.TouchedSource, "the default path uses env.ResolveToolFuncs().ListChanges")
	assert.Equal(t, []string{"src/theme.css"}, out.TouchedFiles)
	d := dsDeltaFor(t, out, func(d design.SyncDelta) bool { return d.Basis == design.DeltaBasisLiteral })
	assert.Equal(t, "color.brand.primary", d.Token)
}

func TestDesignSyncHandler_ExplicitFilesOverridesDefault(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{ --color-brand-primary: #ff0000; }\n")
	dsWrite(t, root, "src/other.css", ":root{ --color-brand-primary: #00ff00; }\n")
	h := &designSyncHandler{}

	// The change tracker reports src/theme.css; the explicit argument must win.
	env := dsEnvWithChanges(t, root, dsListChangesOutput("src/theme.css"))
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"files": "src/other.css"})
	require.NoError(t, err)
	out := dsOutput(t, res)

	assert.Equal(t, "argument", out.TouchedSource)
	assert.Equal(t, []string{"src/other.css"}, out.TouchedFiles)
	d := dsDeltaFor(t, out, func(d design.SyncDelta) bool { return d.Basis == design.DeltaBasisLiteral })
	assert.Contains(t, d.Evidence, "#00ff00", "the explicit file's content is analysed, not the tracker's")
}

func TestDesignSyncHandler_ExplicitFilesListForm(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/a.css", ".a{padding:13px;}\n")
	dsWrite(t, root, "src/b.tsx", "export const x = '#ff00ff';\n")
	h := &designSyncHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": []any{"src/b.tsx", "src/a.css"}})
	require.NoError(t, err)
	out := dsOutput(t, res)
	assert.Equal(t, []string{"src/a.css", "src/b.tsx"}, out.TouchedFiles, "sorted and deduped")
}

func TestDesignSyncHandler_ListChangesBulkItems(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{ --color-brand-primary: #ff0000; }\n")
	h := &designSyncHandler{}

	payload := `{"revision_id":"r","enabled":true,"count":1,"files":[
  {"path":"","op":"bulk","tool":"shell_command","bulk_items":[{"path":"src/theme.css","op":"modified"}]}
]}`
	env := dsEnvWithChanges(t, root, payload)
	res, err := h.Execute(newTestCtx(root), env, map[string]any{})
	require.NoError(t, err)
	out := dsOutput(t, res)
	assert.Equal(t, []string{"src/theme.css"}, out.TouchedFiles, "bulk items are the real paths")
}

func TestDesignSyncHandler_NoChangesIsAnEmptyReport(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	h := &designSyncHandler{}

	env := dsEnvWithChanges(t, root, dsListChangesOutput())
	res, err := h.Execute(newTestCtx(root), env, map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)
	out := dsOutput(t, res)
	assert.Empty(t, out.TouchedFiles)
	assert.Equal(t, 0, out.Report.DeltaCount)
	assert.NotEmpty(t, out.Notes)
}

func TestDesignSyncHandler_NoListChangesSeamIsEmptyNotError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	h := &designSyncHandler{}

	// A ToolEnv with no ListChanges seam (standalone tools) yields an empty
	// touched set, not a failure.
	res, err := h.Execute(newTestCtx(root), ToolEnv{WorkspaceRoot: root}, map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)
	out := dsOutput(t, res)
	assert.Empty(t, out.TouchedFiles)
}

func TestDesignSyncHandler_MalformedListChangesPayloadIsEmpty(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	h := &designSyncHandler{}

	env := dsEnvWithChanges(t, root, "not json at all")
	res, err := h.Execute(newTestCtx(root), env, map[string]any{})
	require.NoError(t, err, "a tolerant parse degrades to an empty set")
	out := dsOutput(t, res)
	assert.Empty(t, out.TouchedFiles)
}

// ---------------------------------------------------------------------------
// Gate-1
// ---------------------------------------------------------------------------

func TestDesignSyncHandler_Gate1Deny(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{--color-brand-primary:#ff0000;}\n")
	h := &designSyncHandler{}

	env := newTestEnv(t, root)
	env.FileAccessClassifier = denyClassifier{}

	res, err := h.Execute(newTestCtx(root), env, map[string]any{"files": "src/theme.css"})
	require.Error(t, err)
	require.True(t, res.IsError, "a Gate-1 deny is a tool failure")
	assert.Contains(t, res.Output, "design_sync blocked")
}

// TestDesignSyncHandler_Gate1DenyOnTouchedPath proves a deny scoped to the
// touched code path (not design/ wholesale) still blocks the run: every touched
// path is prechecked, mirroring the §5b "Gate-1 on all touched paths".
func TestDesignSyncHandler_Gate1DenyOnTouchedPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{--color-brand-primary:#ff0000;}\n")
	h := &designSyncHandler{}

	env := newTestEnv(t, root)
	env.FileAccessClassifier = dsDenyPathClassifier{substr: "/src/"}

	res, err := h.Execute(newTestCtx(root), env, map[string]any{"files": "src/theme.css"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "design_sync blocked")
}

// dsDenyPathClassifier denies any resolved path containing substr.
type dsDenyPathClassifier struct{ substr string }

func (c dsDenyPathClassifier) ClassifyFileAccess(_ context.Context, filePath, resolvedPath, _ string) string {
	target := resolvedPath
	if target == "" {
		target = filePath
	}
	if strings.Contains(filepath.ToSlash(target), c.substr) {
		return "deny"
	}
	return "allow"
}

func (dsDenyPathClassifier) IsFolderSessionAllowed(_ string) bool { return false }

// ---------------------------------------------------------------------------
// Off-workspace / unreadable paths
// ---------------------------------------------------------------------------

func TestDesignSyncHandler_PathOutsideWorkspaceIsSkipped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	h := &designSyncHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "/etc/passwd"})
	require.NoError(t, err, "an unreadable/outside path is skipped, not a crash")
	out := dsOutput(t, res)
	assert.Empty(t, out.TouchedFiles)
	assert.Contains(t, strings.Join(out.Notes, " "), "Skipped")
}

func TestDesignSyncHandler_MissingTouchedFileIsSkipped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	h := &designSyncHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "src/does-not-exist.css"})
	require.NoError(t, err)
	out := dsOutput(t, res)
	assert.Empty(t, out.TouchedFiles)
	assert.Contains(t, strings.Join(out.Notes, " "), "Skipped")
}

// ---------------------------------------------------------------------------
// Mode handling — analyze (item 5.3) and apply (item 5.4)
// ---------------------------------------------------------------------------

func TestDesignSyncHandler_ModeDefaultsToAnalyze(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{--color-brand-primary:#ff0000;}\n")
	h := &designSyncHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"files": "src/theme.css"})
	require.NoError(t, err)
	out := dsOutput(t, res)
	assert.Equal(t, SyncModeAnalyze, out.Report.Mode)
	assert.Nil(t, out.Apply, "analyze mode carries no apply result")
}

// TestDesignSyncHandler_ApplyModeLiteralRewritesTokenFile is the §5b fixture 1
// apply half: a token revalue in code → apply rewrites the DTCG entry, and a
// second run is a no-op.
func TestDesignSyncHandler_ApplyModeLiteralRewritesTokenFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{ --color-brand-primary: #ff0000; }\n")
	before := dsTreeSnapshot(t, root)
	h := &designSyncHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "src/theme.css", "mode": "apply"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := dsOutput(t, res)
	require.NotNil(t, out.Apply, "apply mode carries the apply result")
	assert.True(t, out.Apply.ImplementationUntouched, "§5e: apply never rewrites the implementation")
	assert.Equal(t, []string{"design/tokens/color.tokens.json"}, out.Apply.Applied)
	assert.Equal(t, 0, out.Apply.ProposalCount)

	// The DTCG entry now carries the code's value.
	tokenFile := dsReadFile(t, root, "design/tokens/color.tokens.json")
	assert.Contains(t, tokenFile, "#ff0000")
	assert.Contains(t, tokenFile, `"primary"`)
	assert.Contains(t, res.Output, "design_sync (apply)")
	assert.Contains(t, res.Output, "design/")

	// The AC hard assertion: the workspace diff is confined to design/.
	dsAssertDiffConfinedToDesign(t, before, dsTreeSnapshot(t, root))

	// SECOND RUN: analyze reports the same declared var, but apply is a no-op —
	// the file is unchanged and nothing more is written.
	afterFirst := dsReadFile(t, root, "design/tokens/color.tokens.json")
	res2, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "src/theme.css", "mode": "apply"})
	require.NoError(t, err)
	out2 := dsOutput(t, res2)
	require.NotNil(t, out2.Apply)
	assert.Equal(t, afterFirst, dsReadFile(t, root, "design/tokens/color.tokens.json"),
		"a second apply leaves the token file byte-identical")
	require.Len(t, out2.Apply.Plan.Writes, 1, "the plan re-derives the same, byte-identical write")
	assert.Equal(t, out.Apply.Plan.Writes[0].Content, out2.Apply.Plan.Writes[0].Content,
		"the second plan's content is byte-identical (no-op)")
}

// TestDesignSyncHandler_ApplyModeStructuralCreatesWireframeAndFlow is the §5b
// fixture 2 apply half: a new route/screen → apply creates a skeleton wireframe
// and a flow edge, both with draft status.
func TestDesignSyncHandler_ApplyModeStructuralCreatesWireframeAndFlow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/routes.tsx", `export const routes = [
  { path: "/login", element: <Login /> },
  { path: "/check-deposit", element: <CheckDeposit /> },
];
`)
	before := dsTreeSnapshot(t, root)
	h := &designSyncHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "src/routes.tsx", "mode": "apply"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	out := dsOutput(t, res)
	require.NotNil(t, out.Apply)

	assert.Contains(t, out.Apply.Applied, "design/wireframes/check-deposit.svg")
	assert.Contains(t, out.Apply.Applied, "design/flows/sign-up.mmd")

	wf := dsReadFile(t, root, "design/wireframes/check-deposit.svg")
	assert.Contains(t, wf, "viewBox", "the skeleton is a viewBox-only SVG")
	assert.Contains(t, wf, design.FlowDraftStatus, "the created wireframe carries draft status")
	assert.Contains(t, wf, "check-deposit")

	flow := dsReadFile(t, root, "design/flows/sign-up.mmd")
	assert.Contains(t, flow, "check-deposit", "the flow edge was added")
	assert.Contains(t, flow, "login --> home", "the existing edge survives")

	dsAssertDiffConfinedToDesign(t, before, dsTreeSnapshot(t, root))

	// SECOND RUN: no-op.
	res2, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "src/routes.tsx", "mode": "apply"})
	require.NoError(t, err)
	out2 := dsOutput(t, res2)
	require.NotNil(t, out2.Apply)
	assert.Empty(t, out2.Apply.Applied, "a second apply has nothing left to write")
	assert.Empty(t, out2.Apply.Plan.Writes)
}

// TestDesignSyncHandler_ApplyModeInferredStaysAProposal is the §5b fixture 3
// apply half: raw-hex styling → apply does NOT auto-create a token.
func TestDesignSyncHandler_ApplyModeInferredStaysAProposal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/Badge.tsx", "export const Badge = () => <div style={{color:'#ff00ff'}} />;\n")
	h := &designSyncHandler{}

	before := dsTreeSnapshot(t, root)
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "src/Badge.tsx", "mode": "apply"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	out := dsOutput(t, res)
	require.NotNil(t, out.Apply)

	assert.Empty(t, out.Apply.Applied, "an inferred delta is never auto-applied")
	require.Equal(t, 1, out.Apply.ProposalCount)
	require.Len(t, out.Apply.Plan.Proposals, 1)
	assert.Equal(t, design.DeltaBasisInferred, out.Apply.Plan.Proposals[0].Basis)
	assert.Equal(t, "new-token", out.Apply.Plan.Proposals[0].ProposalKind)
	// Nothing was written at all: the tree is byte-identical.
	assert.Equal(t, before, dsTreeSnapshot(t, root), "an inferred-only apply writes nothing")
	assert.Contains(t, res.Output, "proposal")
}

// TestDesignSyncHandler_ApplyModeConfinesWritesToDesign is the AC hard
// assertion: after apply on a mixed set, the workspace diff is confined to
// design/.
func TestDesignSyncHandler_ApplyModeConfinesWritesToDesign(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	// A UI file with all three bases: a literal revalue, a new route, and raw
	// hex/magic spacing.
	dsWrite(t, root, "src/theme.css", ":root{--color-brand-primary:#ff0000;}\n.btn{background:#ff00ff;padding:13px;}\n")
	dsWrite(t, root, "src/routes.tsx", `{ path: "/check-deposit", element: <CheckDeposit /> }`+"\n")

	before := dsTreeSnapshot(t, root)
	h := &designSyncHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "src/theme.css,src/routes.tsx", "mode": "apply"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	out := dsOutput(t, res)
	require.NotNil(t, out.Apply)

	// At least one write happened, so the confinement check is meaningful.
	require.NotEmpty(t, out.Apply.Applied)
	assert.True(t, out.Apply.Plan.IsConfinedToDesign())
	for _, p := range out.Apply.Applied {
		assert.True(t, designSyncDesignFile(p), "applied path %q must be inside design/", p)
	}

	// The hard assertion: every changed file is under design/.
	after := dsTreeSnapshot(t, root)
	changed := dsChangedPaths(before, after)
	require.NotEmpty(t, changed, "apply should have written something")
	for _, p := range changed {
		assert.True(t, strings.HasPrefix(p, "design/"),
			"design_sync --apply modified a file outside design/: %s", p)
	}
	// The touched code file itself is untouched.
	assert.Contains(t, dsReadFile(t, root, "src/theme.css"), "#ff0000", "the implementation is not rewritten (§5e)")
	assert.Contains(t, dsReadFile(t, root, "src/routes.tsx"), "/check-deposit")
}

// TestDesignSyncHandler_ApplyModeRefusesOffWorkspaceWrite proves §5e is
// enforced even when a delta somehow names a non-design path: the plan refuses
// it and the handler does not write outside design/.
func TestDesignSyncHandler_ApplyModeRefusesOffWorkspaceWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{--color-brand-primary:#ff0000;}\n")

	// A hand-built report whose literal delta names a non-design token file:
	// the pure core drops (refuses) the write, the handler writes nothing.
	report := &design.SyncReport{
		Mode: SyncModeApply,
		Deltas: []design.SyncDelta{{
			Delta:       "token color.brand.primary revalued in code",
			Kind:        design.DeltaKindToken,
			Basis:       design.DeltaBasisLiteral,
			Confidence:  design.ConfidenceHigh,
			DesignFiles: []string{"src/theme.css"},
			SafeToApply: true,
			Token:       "color.brand.primary",
			TokenFile:   "src/theme.css", // off-design, must be refused
			TokenEntry:  "color.brand.primary",
			CodeFiles:   []string{"src/theme.css"},
			Evidence:    "--color-brand-primary: #ff0000; (line 1)",
		}},
	}
	plan := design.PlanSyncApply(report, nil)
	assert.Empty(t, plan.Writes, "a non-design write is dropped")
	require.Len(t, plan.Proposals, 1, "the refused delta is reported, not written")
	assert.Contains(t, plan.Proposals[0].Reason, "outside design/")
	assert.True(t, plan.IsConfinedToDesign())

	// The handler path treats the refusal as a proposal, writing nothing.
	before := dsTreeSnapshot(t, root)
	applied, err := writeSyncPlan(newTestCtx(root), newTestEnv(t, root), plan)
	require.NoError(t, err)
	assert.Empty(t, applied)
	assert.Equal(t, before, dsTreeSnapshot(t, root))
}

// TestDesignSyncHandler_ApplyModeGate1Deny proves Gate-1 is enforced on the
// apply path too: a deny scoped to the design/ token file blocks the write.
func TestDesignSyncHandler_ApplyModeGate1Deny(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{--color-brand-primary:#ff0000;}\n")
	h := &designSyncHandler{}

	env := newTestEnv(t, root)
	// Deny the token file write (a path-scoped deny), leaving the touched code
	// path allowed.
	env.FileAccessClassifier = dsDenyPathClassifier{substr: "color.tokens.json"}

	before := dsTreeSnapshot(t, root)
	res, err := h.Execute(newTestCtx(root), env,
		map[string]any{"files": "src/theme.css", "mode": "apply"})
	require.Error(t, err, "a Gate-1 deny on an apply write path is a hard failure")
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "design_sync blocked")
	assert.Equal(t, before, dsTreeSnapshot(t, root), "a denied apply writes nothing")
}

// TestDesignSyncHandler_ApplyModeUnknownModeStillErrors guards the mode switch.
func TestDesignSyncHandler_UnknownModeIsAnError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	h := &designSyncHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"mode": "wat"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "unknown mode")
}

// dsReadFile reads a workspace-relative file (slash path) as a string.
func dsReadFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoError(t, err, "reading %s", rel)
	return string(data)
}

// dsChangedPaths returns the workspace-relative slash paths whose content
// differs between two snapshots (or that appear in only one).
func dsChangedPaths(before, after []string) []string {
	parse := func(entries []string) map[string]string {
		out := map[string]string{}
		for _, e := range entries {
			idx := strings.Index(e, "=")
			if idx < 0 {
				continue
			}
			out[e[:idx]] = e[idx+1:]
		}
		return out
	}
	b, a := parse(before), parse(after)
	seen := map[string]bool{}
	for k := range b {
		seen[k] = true
	}
	for k := range a {
		seen[k] = true
	}
	var changed []string
	for k := range seen {
		if b[k] != a[k] {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed
}

// dsAssertDiffConfinedToDesign asserts that every path changed between two
// snapshots is inside design/ — the SP-140-5 AC hard assertion that
// design_sync --apply never modifies files outside design/.
func dsAssertDiffConfinedToDesign(t *testing.T, before, after []string) {
	t.Helper()
	changed := dsChangedPaths(before, after)
	require.NotEmpty(t, changed, "apply should have changed something")
	for _, p := range changed {
		assert.True(t, strings.HasPrefix(p, "design/"),
			"design_sync --apply modified a file outside design/: %s", p)
	}
}

// TestDesignSyncHandler_AnalyzeModeStillWritesNothing re-asserts that adding
// apply did not make analyze write: analyze is read-only.
func TestDesignSyncHandler_AnalyzeModeStillWritesNothing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{--color-brand-primary:#ff0000;}\n")
	before := dsTreeSnapshot(t, root)

	h := &designSyncHandler{}
	_, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "src/theme.css", "mode": "analyze"})
	require.NoError(t, err)
	assert.Equal(t, before, dsTreeSnapshot(t, root))
}

// ---------------------------------------------------------------------------
// Determinism / no writes
// ---------------------------------------------------------------------------

func TestDesignSyncHandler_Deterministic(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{--color-brand-primary:#ff0000;}\n.btn{background:#ff00ff;padding:13px;}\n")
	dsWrite(t, root, "src/routes.tsx", `{ path: "/check-deposit", element: <CheckDeposit /> }`+"\n")
	dsWrite(t, root, "src/Nav.tsx", `<Link to="/settings">S</Link>`+"\n")
	h := &designSyncHandler{}

	var reference string
	for run := 0; run < 8; run++ {
		order := []string{"src/theme.css", "src/routes.tsx", "src/Nav.tsx"}
		if run%2 == 1 {
			order = []string{"src/Nav.tsx", "src/routes.tsx", "src/theme.css"}
		}
		args := map[string]any{"files": strings.Join(order, ",")}
		res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), args)
		require.NoError(t, err)
		out := dsOutput(t, res)
		data, err := json.Marshal(out.Report)
		require.NoError(t, err)
		if run == 0 {
			reference = string(data)
			continue
		}
		assert.Equal(t, reference, string(data), "run %d drifted", run+1)
	}
}

// TestDesignSyncHandler_AnalyzeWritesNothing is the §5e-adjacent guard: analyze
// mode is read-only, so the workspace tree is byte-identical after a run.
func TestDesignSyncHandler_AnalyzeWritesNothing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{--color-brand-primary:#ff0000;}\n")
	before := dsTreeSnapshot(t, root)

	h := &designSyncHandler{}
	_, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"files": "src/theme.css"})
	require.NoError(t, err)

	assert.Equal(t, before, dsTreeSnapshot(t, root), "analyze mode must not create or modify files")
}

// dsTreeSnapshot returns a sorted path→content map of every file under root.
func dsTreeSnapshot(t *testing.T, root string) []string {
	t.Helper()
	var entries []string
	require.NoError(t, filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		require.NoError(t, relErr)
		data, readErr := os.ReadFile(p)
		require.NoError(t, readErr)
		entries = append(entries, filepath.ToSlash(rel)+"="+string(data))
		return nil
	}))
	sort.Strings(entries)
	return entries
}

// ---------------------------------------------------------------------------
// Report contract for apply (item 5.4)
// ---------------------------------------------------------------------------

// TestDesignSyncHandler_ReportIsApplyReady pins the contract the 5.4 apply half
// consumes: every delta carries the §5b fields, designFiles is always an array
// of design/-confined paths, and only literal/structural deltas are safe.
func TestDesignSyncHandler_ReportIsApplyReady(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{--color-brand-primary:#ff0000;}\n.btn{background:#ff00ff;padding:13px;}\n")
	dsWrite(t, root, "src/routes.tsx", `{ path: "/check-deposit", element: <CheckDeposit /> }`+"\n")
	h := &designSyncHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "src/theme.css,src/routes.tsx"})
	require.NoError(t, err)
	require.NotEmpty(t, dsOutput(t, res).Report.Deltas)

	data, err := json.Marshal(res.StructuredOut)
	require.NoError(t, err)
	var generic map[string]any
	require.NoError(t, json.Unmarshal(data, &generic))
	require.Contains(t, generic, "report")

	report, ok := generic["report"].(map[string]any)
	require.True(t, ok)
	deltas, ok := report["deltas"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, deltas)

	for _, raw := range deltas {
		d, ok := raw.(map[string]any)
		require.True(t, ok)
		for _, key := range []string{"delta", "kind", "basis", "confidence", "designFiles", "safeToApply", "codeFiles"} {
			assert.Contains(t, d, key, "delta must carry %q", key)
		}
		files, ok := d["designFiles"].([]any)
		require.True(t, ok, "designFiles is always an array")
		for _, f := range files {
			s, _ := f.(string)
			assert.True(t, designSyncDesignFile(s), "apply must be confined to design/, got %q", s)
		}
		basis, _ := d["basis"].(string)
		safe, _ := d["safeToApply"].(bool)
		if basis == "inferred" {
			assert.False(t, safe)
			assert.Equal(t, true, d["proposal"])
		} else {
			assert.True(t, safe)
		}
	}

	// The mode is recorded so a stored report says where it came from.
	assert.Equal(t, "analyze", report["mode"])
}

// ---------------------------------------------------------------------------
// No design/ tree
// ---------------------------------------------------------------------------

func TestDesignSyncHandler_NoDesignTreeIsGuidanceNotError(t *testing.T) {
	t.Parallel()
	root := t.TempDir() // no design/
	dsWrite(t, root, "src/theme.css", ".btn{background:#ff00ff;}\n")
	h := &designSyncHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"files": "src/theme.css"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	out := dsOutput(t, res)
	assert.Contains(t, strings.Join(out.Report.Notes, " "), "No design/ tree")
	// Still reports deltas — as proposals, since there is nothing to switch to.
	d := dsDeltaFor(t, out, func(d design.SyncDelta) bool { return d.Basis == design.DeltaBasisInferred })
	assert.True(t, d.Proposal)
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func TestDesignSyncHandler_RegisteredInAllTools(t *testing.T) {
	t.Parallel()
	found := false
	for _, h := range AllTools() {
		if h.Name() == "design_sync" {
			found = true
			break
		}
	}
	require.True(t, found, "AllTools() must register design_sync on native builds")
}

// TestDesignSyncHandler_NotInSharedList proves SP-140 invariant 7: the handler
// is reached through its build-tagged registrar, not constructed in all.go's
// unconditional shared list (which would put it on the WASM roster).
func TestDesignSyncHandler_NotInSharedList(t *testing.T) {
	t.Parallel()
	all, err := os.ReadFile("all.go")
	require.NoError(t, err)
	assert.NotContains(t, string(all), "&designSyncHandler{}",
		"design_sync must be registered via registerDesignSyncTools(), not the shared list")
	assert.Contains(t, string(all), "registerDesignSyncTools()")
}

// TestDesignSyncHandler_ApplyModeIsChangeTrackerVisible proves §5b's "All
// writes are ordinary workspace file edits — ChangeTracker-visible, revertible":
// every write apply performs is recorded through the same TrackFileWrite seam
// write_file uses, with the pre-write original captured (so the edit is
// revertible).
func TestDesignSyncHandler_ApplyModeIsChangeTrackerVisible(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{ --color-brand-primary: #ff0000; }\n")

	type tracked struct{ path, original, content string }
	var trackedWrites []tracked
	env := newTestEnv(t, root)
	env.ToolFuncs = &ToolFuncSet{
		TrackFileWrite: func(filePath, originalContent, content string) error {
			trackedWrites = append(trackedWrites, tracked{filePath, originalContent, content})
			return nil
		},
	}

	h := &designSyncHandler{}
	res, err := h.Execute(newTestCtx(root), env,
		map[string]any{"files": "src/theme.css", "mode": "apply"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	require.Len(t, trackedWrites, 1, "every apply write is tracked")
	tr := trackedWrites[0]
	assert.True(t, strings.HasSuffix(filepath.ToSlash(tr.path), "design/tokens/color.tokens.json"))
	assert.Contains(t, tr.original, "#0055ff", "the pre-write original is captured for revert")
	assert.Contains(t, tr.content, "#ff0000", "the tracked content is the post-write bytes")
}

// TestDesignSyncHandler_ApplyModeTrackedWriteOriginalIsEmptyForCreate proves
// the create case: a new wireframe is tracked with an empty original, so a
// revert deletes it.
func TestDesignSyncHandler_ApplyModeTrackedWriteOriginalIsEmptyForCreate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/routes.tsx", `{ path: "/check-deposit", element: <CheckDeposit /> }`+"\n")

	originals := map[string]string{}
	env := newTestEnv(t, root)
	env.ToolFuncs = &ToolFuncSet{
		TrackFileWrite: func(filePath, originalContent, content string) error {
			originals[filepath.Base(filePath)] = originalContent
			return nil
		},
	}

	h := &designSyncHandler{}
	_, err := h.Execute(newTestCtx(root), env, map[string]any{"files": "src/routes.tsx", "mode": "apply"})
	require.NoError(t, err)

	require.Contains(t, originals, "check-deposit.svg")
	assert.Empty(t, originals["check-deposit.svg"], "a created file's tracked original is empty (revert deletes it)")
}

// TestDesignSyncHandler_ApplyModeGate1DenyOnTouchedCodePath proves the
// dispatch-level Gate-1 still covers the touched code paths in apply mode.
func TestDesignSyncHandler_ApplyModeGate1DenyOnTouchedCodePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{--color-brand-primary:#ff0000;}\n")

	env := newTestEnv(t, root)
	env.FileAccessClassifier = dsDenyPathClassifier{substr: "/src/"}

	before := dsTreeSnapshot(t, root)
	h := &designSyncHandler{}
	res, err := h.Execute(newTestCtx(root), env,
		map[string]any{"files": "src/theme.css", "mode": "apply"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "design_sync blocked")
	assert.Equal(t, before, dsTreeSnapshot(t, root))
}

var _ = design.SyncModeApplyConst

// TestDesignSyncHandler_ApplyModeRollsBackOnWriteFailure proves a failure
// part-way through the plan leaves no half-applied state: the writes already
// applied are rolled back (created files removed, updated files restored).
//
// The structural plan writes the wireframe then the flow edge; denying the
// flow-file write makes the second write fail, so the first must be undone.
func TestDesignSyncHandler_ApplyModeRollsBackOnWriteFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/routes.tsx", `{ path: "/check-deposit", element: <CheckDeposit /> }`+"\n")
	before := dsTreeSnapshot(t, root)

	env := newTestEnv(t, root)
	env.FileAccessClassifier = dsDenyPathClassifier{substr: "sign-up.mmd"}

	h := &designSyncHandler{}
	res, err := h.Execute(newTestCtx(root), env,
		map[string]any{"files": "src/routes.tsx", "mode": "apply"})
	require.Error(t, err, "a denied write path fails the apply")
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "design_sync blocked")

	// The structured result reports an empty applied set, matching the tree.
	if out, ok := res.StructuredOut.(designSyncOutput); ok && out.Apply != nil {
		assert.Empty(t, out.Apply.Applied, "a rolled-back apply reports nothing applied")
	}

	// The tree is byte-identical: the wireframe write was rolled back.
	assert.Equal(t, before, dsTreeSnapshot(t, root),
		"a failed apply must leave no half-applied state")
}

// TestDesignSyncHandler_ApplyModePromptVerdictFallsThrough proves the write path
// mirrors write_file's Gate-1 contract: only an explicit deny is a hard
// refusal; a "prompt" (no classifier / no verdict) falls through to the
// workspace-confined resolver, which admits a design/ path inside the workspace.
func TestDesignSyncHandler_ApplyModePromptVerdictFallsThrough(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{--color-brand-primary:#ff0000;}\n")

	// A nil classifier yields "prompt": the write must still land (a design/
	// path inside the workspace), not be silently dropped.
	env := newTestEnv(t, root)
	env.FileAccessClassifier = nil

	h := &designSyncHandler{}
	res, err := h.Execute(newTestCtx(root), env,
		map[string]any{"files": "src/theme.css", "mode": "apply"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	out := dsOutput(t, res)
	require.NotNil(t, out.Apply)
	assert.Equal(t, []string{"design/tokens/color.tokens.json"}, out.Apply.Applied)
	assert.Contains(t, dsReadFile(t, root, "design/tokens/color.tokens.json"), "#ff0000")
}
