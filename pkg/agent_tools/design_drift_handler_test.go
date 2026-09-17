package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// -----------------------------------------------------------------------------
// SP-140-5 §5c — drift direction at the tool boundary (TODO item 5.5)
//
// The AC: "design-ahead and code-ahead states report distinct rows with
// distinct remedies in design_assets output." These tests assert that at the
// tool boundary for BOTH surfaces that report drift (design_assets and
// design_validate), that the two agree on the vocabulary, that severities are
// advisory, and that the whole thing is deterministic and read-only.
// -----------------------------------------------------------------------------

// ddWrite writes rel (slash-separated) under root with parent directories.
func ddWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// ddTree writes a minimal design/ tree (tokens, two wireframes, one flow) that
// yields no validator findings. It has no design/generated/ unless ddExport
// runs.
func ddTree(t *testing.T, root string) {
	t.Helper()
	ddWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {
    "brand": {
      "primary": { "$type": "color", "$value": "#0055ff" }
    }
  }
}`)
	ddWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><rect data-nav="home"/></svg>`)
	ddWrite(t, root, "design/wireframes/home.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Home</text></svg>`)
	ddWrite(t, root, "design/flows/sign-up.mmd", "flowchart TD\n  login --> home\n")
}

// ddExport writes a genuine §5a export (with provenance headers) from the
// current token sources.
func ddExport(t *testing.T, root string) {
	t.Helper()
	tokens, err := design.ResolveExportTokens(root)
	require.NoError(t, err)
	artifacts, err := design.RenderArtifacts(tokens, ddAllTargets())
	require.NoError(t, err)
	require.NoError(t, design.WriteExportedArtifacts(root, artifacts))
}

// ddAllTargets resolves every export target (the drift fixture only needs one
// artifact, but a full export mirrors reality).
func ddAllTargets() []design.ExportTarget {
	targets, err := design.ResolveExportTargets(design.ExportTargetAll)
	if err != nil {
		panic(err)
	}
	return targets
}

// ddEnvWithChanges builds a ToolEnv whose ListChanges seam returns the given
// paths (the ChangeTracker default for the code-ahead signal).
func ddEnvWithChanges(t *testing.T, root string, paths ...string) ToolEnv {
	t.Helper()
	env := newTestEnv(t, root)
	type fileEntry struct {
		Path string `json:"path"`
		Op   string `json:"op"`
	}
	type envelope struct {
		Count int         `json:"count"`
		Files []fileEntry `json:"files"`
	}
	e := envelope{Count: len(paths)}
	for _, p := range paths {
		e.Files = append(e.Files, fileEntry{Path: p, Op: "modified"})
	}
	payload, err := json.Marshal(e)
	require.NoError(t, err)
	env.ToolFuncs = &ToolFuncSet{
		ListChanges: func(context.Context, map[string]any) (string, error) {
			return string(payload), nil
		},
	}
	return env
}

// ddAssetsOutput extracts the design_assets structured result.
func ddAssetsOutput(t *testing.T, res ToolResult) designAssetsOutput {
	t.Helper()
	out, ok := res.StructuredOut.(designAssetsOutput)
	require.True(t, ok, "StructuredOut must be a designAssetsOutput, got %T", res.StructuredOut)
	return out
}

// ddRow finds a drift direction row by name in the tool-layer section.
func ddRow(t *testing.T, out designDriftOut, direction string) designDriftRowOut {
	t.Helper()
	for _, r := range out.Rows {
		if r.Direction == direction {
			return r
		}
	}
	require.FailNow(t, "missing drift row", "direction=%s rows=%v", direction, out.Rows)
	return designDriftRowOut{}
}

// -----------------------------------------------------------------------------
// design_assets: the AC
// -----------------------------------------------------------------------------

// TestDesignAssets_DriftBothDirectionsDistinctRows pins the AC exactly:
// design_assets reports design-ahead and code-ahead as two distinct rows, each
// with its own remedy and its own next-step tool.
func TestDesignAssets_DriftBothDirectionsDistinctRows(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ddTree(t, root)
	ddExport(t, root)
	// Move the tokens after the export so design-ahead holds...
	ddWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {"brand": {"primary": { "$type": "color", "$value": "#ff0000" }}}
}`)
	// ...and touch code with an importable delta so code-ahead holds.
	ddWrite(t, root, "webui/src/theme.css", ":root { --color-brand-primary: #0055ff; }\n")

	h := &designAssetsHandler{}
	res, err := h.Execute(newTestCtx(root), ddEnvWithChanges(t, root, "webui/src/theme.css"), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := ddAssetsOutput(t, res)
	require.Len(t, out.Drift.Rows, 2, "both directions are always present")

	designAhead := ddRow(t, out.Drift, design.DriftRowDesignAhead)
	codeAhead := ddRow(t, out.Drift, design.DriftRowCodeAhead)

	require.True(t, designAhead.Ahead, "design-ahead must hold")
	require.True(t, codeAhead.Ahead, "code-ahead must hold")
	assert.False(t, designAhead.Synced)
	assert.False(t, codeAhead.Synced)

	// Distinct remedies with distinct next-step tools.
	assert.NotEqual(t, designAhead.Remedy, codeAhead.Remedy)
	assert.NotEqual(t, designAhead.NextStep, codeAhead.NextStep)
	assert.Equal(t, "design_export_tokens", designAhead.NextStep)
	assert.Equal(t, "design_sync", codeAhead.NextStep)
	assert.Contains(t, designAhead.Remedy, "design_export_tokens")
	assert.Contains(t, codeAhead.Remedy, "design_sync")

	// Advisory only, never error.
	assert.NotEqual(t, "error", designAhead.Advisory)
	assert.NotEqual(t, "error", codeAhead.Advisory)
	assert.Equal(t, design.DriftSeverityWarn, designAhead.Advisory)
	assert.Equal(t, design.DriftSeverityWarn, codeAhead.Advisory)

	// Concrete, distinct magnitudes.
	assert.Equal(t, 3, designAhead.Count, "2 wireframes + 1 flow ahead of the build")
	assert.Equal(t, 1, codeAhead.Count, "one importable semantic delta")
	assert.Equal(t, 3, out.Drift.DesignAheadCount)
	assert.Equal(t, 1, out.Drift.CodeAheadCount)
	assert.False(t, out.Drift.Synced)

	// The summary text names both directions and their remedies.
	assert.Contains(t, res.Output, "design-ahead 3 (remedy: design_export_tokens)")
	assert.Contains(t, res.Output, "code-ahead 1 (remedy: design_sync)")
}

// TestDesignAssets_DriftSyncedTree pins the balanced state: no drift, both rows
// synced with empty remedies, and the summary says so.
func TestDesignAssets_DriftSyncedTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ddTree(t, root)
	ddExport(t, root) // export matching the current tokens

	h := &designAssetsHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)

	out := ddAssetsOutput(t, res)
	require.Len(t, out.Drift.Rows, 2)
	assert.True(t, out.Drift.Synced)
	for _, r := range out.Drift.Rows {
		assert.False(t, r.Ahead, "row %s must be synced", r.Direction)
		assert.True(t, r.Synced)
		assert.Empty(t, r.Remedy)
	}
	assert.Contains(t, res.Output, "Drift: in sync")
	assert.Contains(t, res.Output, "design-ahead: none")
	assert.Contains(t, res.Output, "code-ahead: none")
}

// TestDesignAssets_DriftDesignAheadOnly pins the healthy active-project state:
// design-ahead holds with its remedy, and code-ahead is explicitly synced (not
// absent), so the two-row shape survives a one-sided report.
func TestDesignAssets_DriftDesignAheadOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ddTree(t, root)
	ddExport(t, root)
	ddWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {"brand": {"primary": { "$type": "color", "$value": "#00ff00" }}}
}`)

	h := &designAssetsHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)

	out := ddAssetsOutput(t, res)
	designAhead := ddRow(t, out.Drift, design.DriftRowDesignAhead)
	codeAhead := ddRow(t, out.Drift, design.DriftRowCodeAhead)
	assert.True(t, designAhead.Ahead)
	assert.Equal(t, "design_export_tokens", designAhead.NextStep)
	assert.False(t, codeAhead.Ahead)
	assert.True(t, codeAhead.Synced, "the other direction is present and synced, not missing")
	assert.Empty(t, codeAhead.Remedy)
}

// TestDesignAssets_DriftCodeAheadOnly pins the state design_sync exists to fix:
// code-ahead holds with the run-me pointer, and design-ahead is synced.
func TestDesignAssets_DriftCodeAheadOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ddTree(t, root)
	ddExport(t, root) // design side current
	ddWrite(t, root, "webui/src/theme.css", ":root { --color-brand-primary: #0055ff; }\n")

	h := &designAssetsHandler{}
	res, err := h.Execute(newTestCtx(root), ddEnvWithChanges(t, root, "webui/src/theme.css"), map[string]any{})
	require.NoError(t, err)

	out := ddAssetsOutput(t, res)
	designAhead := ddRow(t, out.Drift, design.DriftRowDesignAhead)
	codeAhead := ddRow(t, out.Drift, design.DriftRowCodeAhead)
	assert.False(t, designAhead.Ahead)
	assert.True(t, designAhead.Synced)
	require.True(t, codeAhead.Ahead)
	assert.Equal(t, "design_sync", codeAhead.NextStep)
	assert.Contains(t, res.Output, "code-ahead 1 (remedy: design_sync)")
}

// TestDesignAssets_DriftIgnoresDesignOnlyChanges pins that a design-only edit
// session cannot masquerade as code-ahead: design/ paths in the change set are
// not "the implementation".
func TestDesignAssets_DriftIgnoresDesignOnlyChanges(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ddTree(t, root)
	ddExport(t, root)
	ddWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><rect data-nav="home"/></svg>`)

	h := &designAssetsHandler{}
	res, err := h.Execute(newTestCtx(root), ddEnvWithChanges(t, root, "design/wireframes/login.svg"), map[string]any{})
	require.NoError(t, err)

	out := ddAssetsOutput(t, res)
	assert.False(t, ddRow(t, out.Drift, design.DriftRowCodeAhead).Ahead,
		"a design/ path is the semantic layer, not the implementation")
}

// TestDesignAssets_DriftSectionIsStableJSON pins the JSON contract: the drift
// section always serialises both directions, with the fields a consumer reads.
func TestDesignAssets_DriftSectionIsStableJSON(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ddTree(t, root)

	h := &designAssetsHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)

	data, err := json.Marshal(res.StructuredOut)
	require.NoError(t, err)
	var decoded struct {
		Drift struct {
			Directions []struct {
				Direction string `json:"direction"`
				Synced    bool   `json:"synced"`
				Advisory  string `json:"advisory"`
			} `json:"directions"`
			Synced bool `json:"synced"`
		} `json:"drift"`
	}
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Len(t, decoded.Drift.Directions, 2)
	assert.Equal(t, "design-ahead", decoded.Drift.Directions[0].Direction)
	assert.Equal(t, "code-ahead", decoded.Drift.Directions[1].Direction)
	assert.True(t, decoded.Drift.Directions[0].Synced)
}

// TestDesignAssets_DriftMissingTreeIsSyncedButStable pins the no-tree path: the
// section is still present with both rows synced (the exists:false guidance is
// the scaffold signal).
func TestDesignAssets_DriftMissingTreeIsSyncedButStable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	h := &designAssetsHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)

	out := ddAssetsOutput(t, res)
	assert.False(t, out.Exists)
	require.Len(t, out.Drift.Rows, 2)
	assert.True(t, out.Drift.Synced)
	assert.Equal(t, "design-ahead", out.Drift.Rows[0].Direction)
	assert.Equal(t, "code-ahead", out.Drift.Rows[1].Direction)
}

// TestDesignAssets_DriftIsDeterministic pins that repeated runs produce
// identical drift sections.
func TestDesignAssets_DriftIsDeterministic(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ddTree(t, root)
	ddExport(t, root)
	ddWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {"brand": {"primary": { "$type": "color", "$value": "#ff0000" }}}
}`)
	ddWrite(t, root, "webui/src/theme.css", ":root { --color-brand-primary: #0055ff; }\n")

	h := &designAssetsHandler{}
	env := ddEnvWithChanges(t, root, "webui/src/theme.css")
	first, err := h.Execute(newTestCtx(root), env, map[string]any{})
	require.NoError(t, err)
	second, err := h.Execute(newTestCtx(root), env, map[string]any{})
	require.NoError(t, err)

	assert.Equal(t, ddAssetsOutput(t, first).Drift, ddAssetsOutput(t, second).Drift)
	assert.Equal(t, first.Output, second.Output)
}

// TestDesignAssets_DriftIsReadOnly pins that drift analysis writes nothing.
func TestDesignAssets_DriftIsReadOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ddTree(t, root)
	ddExport(t, root)
	ddWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {"brand": {"primary": { "$type": "color", "$value": "#ff0000" }}}
}`)

	h := &designAssetsHandler{}
	before := ddSnapshot(t, root)
	_, err := h.Execute(newTestCtx(root), ddEnvWithChanges(t, root, "design/tokens/color.tokens.json"), map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, before, ddSnapshot(t, root), "drift analysis must not write")
}

// ddSnapshot returns {relative path: content} for every regular file under root.
func ddSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	}))
	return out
}

// -----------------------------------------------------------------------------
// design_validate: the same two rows, as advisory findings
// -----------------------------------------------------------------------------

// dvOutput extracts the design_validate structured result.
func dvOutput(t *testing.T, res ToolResult) findingsOutput {
	t.Helper()
	out, ok := res.StructuredOut.(findingsOutput)
	require.True(t, ok, "StructuredOut must be a findingsOutput, got %T", res.StructuredOut)
	return out
}

// dvFindingByRule returns the first finding with the given rule, or fails.
func dvFindingByRule(t *testing.T, out findingsOutput, rule string) findingOut {
	t.Helper()
	for _, f := range out.Findings {
		if f.Rule == rule {
			return f
		}
	}
	require.FailNow(t, "missing finding", "rule=%s findings=%v", rule, out.Findings)
	return findingOut{}
}

// TestDesignValidate_DriftBothDirectionsDistinctFindings pins that
// design_validate reports the two drift directions as two distinct advisory
// findings with distinct remedies, matching design_assets' vocabulary.
func TestDesignValidate_DriftBothDirectionsDistinctFindings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ddTree(t, root)
	ddExport(t, root)
	ddWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {"brand": {"primary": { "$type": "color", "$value": "#ff0000" }}}
}`)
	ddWrite(t, root, "webui/src/theme.css", ":root { --color-brand-primary: #0055ff; }\n")

	h := &designValidateHandler{}
	res, err := h.Execute(newTestCtx(root), ddEnvWithChanges(t, root, "webui/src/theme.css"), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError, "drift findings are advisory and never fail the run")

	out := dvOutput(t, res)

	// The structured drift section carries both rows.
	require.Len(t, out.Drift.Rows, 2)
	designAhead := ddRow(t, out.Drift, design.DriftRowDesignAhead)
	codeAhead := ddRow(t, out.Drift, design.DriftRowCodeAhead)
	assert.True(t, designAhead.Ahead)
	assert.True(t, codeAhead.Ahead)
	assert.Equal(t, "design_export_tokens", designAhead.NextStep)
	assert.Equal(t, "design_sync", codeAhead.NextStep)

	// The same rows appear as distinct advisory findings.
	dFinding := dvFindingByRule(t, out, "drift_design_ahead")
	cFinding := dvFindingByRule(t, out, "drift_code_ahead")
	assert.Equal(t, "warn", dFinding.Severity)
	assert.Equal(t, "warn", cFinding.Severity)
	assert.NotEqual(t, "error", dFinding.Severity)
	assert.NotEqual(t, "error", cFinding.Severity)
	assert.Contains(t, dFinding.Message, "design_export_tokens")
	assert.Contains(t, cFinding.Message, "design_sync")
	assert.Equal(t, "design", dFinding.File)
	assert.Equal(t, "design", cFinding.File)

	// The summary line names both directions.
	assert.Contains(t, res.Output, "design-ahead")
	assert.Contains(t, res.Output, "code-ahead")
}

// TestDesignValidate_DriftSyncedNoFindings pins that a balanced tree emits no
// drift findings (no "guilt" on a clean run) while the structured section still
// reports both rows synced.
func TestDesignValidate_DriftSyncedNoFindings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ddTree(t, root)
	ddExport(t, root)

	h := &designValidateHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)

	out := dvOutput(t, res)
	for _, f := range out.Findings {
		assert.False(t, strings.HasPrefix(f.Rule, "drift_"),
			"a synced direction must not emit a finding, got rule %s", f.Rule)
	}
	require.Len(t, out.Drift.Rows, 2)
	assert.True(t, out.Drift.Synced)
	for _, r := range out.Drift.Rows {
		assert.False(t, r.Ahead)
	}
	assert.Contains(t, res.Output, "Drift: in sync")
}

// TestDesignValidate_DriftNoTreeStillReportsRows pins the no-tree path: rows
// present, synced, and no drift findings.
func TestDesignValidate_DriftNoTreeStillReportsRows(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)

	out := dvOutput(t, res)
	require.Len(t, out.Drift.Rows, 2)
	assert.True(t, out.Drift.Synced)
	assert.Empty(t, out.Findings)
}

// TestDesignValidate_DriftAgreesWithDesignAssets pins that the two surfaces
// report the same rows for the same workspace: same directions, same counts,
// same remedies (one vocabulary, §5c "both report both").
func TestDesignValidate_DriftAgreesWithDesignAssets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ddTree(t, root)
	ddExport(t, root)
	ddWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {"brand": {"primary": { "$type": "color", "$value": "#ff0000" }}}
}`)
	ddWrite(t, root, "webui/src/theme.css", ":root { --color-brand-primary: #0055ff; }\n")
	env := ddEnvWithChanges(t, root, "webui/src/theme.css")

	assetsRes, err := (&designAssetsHandler{}).Execute(newTestCtx(root), env, map[string]any{})
	require.NoError(t, err)
	validateRes, err := (&designValidateHandler{}).Execute(newTestCtx(root), env, map[string]any{})
	require.NoError(t, err)

	assetsDrift := ddAssetsOutput(t, assetsRes).Drift
	validateDrift := dvOutput(t, validateRes).Drift

	assert.Equal(t, assetsDrift, validateDrift, "the two surfaces must report the same drift section")
}

// TestDesignValidate_DriftIsDeterministic pins that repeated runs produce
// identical drift sections and summaries.
func TestDesignValidate_DriftIsDeterministic(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ddTree(t, root)
	ddExport(t, root)
	ddWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {"brand": {"primary": { "$type": "color", "$value": "#ff0000" }}}
}`)

	h := &designValidateHandler{}
	first, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	second, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)

	assert.Equal(t, dvOutput(t, first).Drift, dvOutput(t, second).Drift)
	assert.Equal(t, first.Output, second.Output)
}

// TestDesignValidate_DriftIsReadOnly pins that drift analysis writes nothing.
func TestDesignValidate_DriftIsReadOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ddTree(t, root)
	ddExport(t, root)
	ddWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": {"brand": {"primary": { "$type": "color", "$value": "#ff0000" }}}
}`)

	h := &designValidateHandler{}
	before := ddSnapshot(t, root)
	_, err := h.Execute(newTestCtx(root), ddEnvWithChanges(t, root, "design/tokens/color.tokens.json"), map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, before, ddSnapshot(t, root), "drift analysis must not write")
}
