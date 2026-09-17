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
// design_sync handler tests (SP-140-5 §5b, TODO item 5.3 — ANALYZE mode)
//
// The pure analysis is unit tested in pkg/design/sync_test.go. These tests
// cover the ToolHandler seam: argument resolution, the touched-file default via
// the sanctioned ListChanges seam and the explicit `files` override, Gate-1
// prechecks (including off-workspace denial), the structured report, determinism
// through the real filesystem, and the explicit refusal of apply mode (item 5.4).
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
// Mode handling — apply is item 5.4 and must refuse explicitly
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
}

func TestDesignSyncHandler_ApplyModeRefusedExplicitly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dsFixtureTree(t, root)
	dsWrite(t, root, "src/theme.css", ":root{--color-brand-primary:#ff0000;}\n")

	// Snapshot the tree so we can assert apply wrote nothing.
	before := dsTreeSnapshot(t, root)

	h := &designSyncHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"files": "src/theme.css", "mode": "apply"})
	require.Error(t, err, "apply is item 5.4 and must refuse explicitly")
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "mode=apply is not available yet")
	assert.Contains(t, res.Output, "not available")
	assert.Equal(t, before, dsTreeSnapshot(t, root), "a refused apply writes nothing")
}

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
