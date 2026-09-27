//go:build !js

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// ---------------------------------------------------------------------------
// design_export_tokens `flows` target (SP-140-9 §9b): the derived-flow export
// write path through the ToolHandler seam — the command the generator banner
// and the drift validator both name (targets:flows).
// ---------------------------------------------------------------------------

// dxFNavScreen is a minimal screen pair for the flow export tests: home
// navigates to detail, so a flow stepping on home carries that data-nav edge
// as an off-path edge in the derived export.
const (
	dxFHome = `<!doctype html>
<html data-screen="home"><body><a data-nav="to:detail;trigger:tap item">detail</a></body>
</html>
`
	dxFDetail = `<!doctype html>
<html data-screen="detail"><body><h1>Detail</h1></body>
</html>
`
)

const dxFSourceJSON = `{
  "name": "sign-up",
  "steps": [
    {"id": "s1", "label": "Home", "screen": "home"},
    {"id": "s2", "label": "Detail", "screen": "detail", "trigger": "open", "next": "s3"},
    {"id": "s3", "label": "Done", "screen": "home"}
  ]
}`

// dxFWriteTree seeds the flow-export fixture: two screens, one flow source,
// the SP-143 runtime kit (the screen validators need it for a clean tree).
func dxFWriteTree(t *testing.T, root string) {
	t.Helper()
	daWrite(t, root, "design/screens/home.html", dxFHome)
	daWrite(t, root, "design/screens/detail.html", dxFDetail)
	daWrite(t, root, "design/flows/sign-up.json", dxFSourceJSON)
}

// dxFReadExport reads the derived export for the fixture flow.
func dxFReadExport(t *testing.T, root string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "design", "flows", "sign-up.mmd"))
	require.NoError(t, err, "the derived export must exist after targets:flows")
	return data
}

// TestDesignExportHandler_FlowsTargetWritesExports pins the happy path: a
// flows-only run writes design/flows/<name>.mmd beside its .json source,
// carrying the provenance header whose hash verifies against the recompute.
func TestDesignExportHandler_FlowsTargetWritesExports(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxFWriteTree(t, root)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "flows"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(designExportOutput)
	require.Len(t, out.Files, 1)
	assert.Equal(t, design.ExportTargetFlows, out.Files[0].Target)
	assert.Equal(t, "design/flows/sign-up.mmd", out.Files[0].Path)
	assert.Contains(t, res.Output, "sign-up.mmd")

	data := string(dxFReadExport(t, root))
	assert.Contains(t, data, "%% flow-source-hash: fnv1a64:")
	assert.Contains(t, data, "flowchart TD")
	assert.Contains(t, data, `s1["Home"]`)
	assert.Contains(t, data, "s2 -->|open| s3", "the step trigger renders as an edge label")
	assert.Contains(t, data, `home -->|tap item| detail`, "the off-path data-nav edge joins the graph")

	// The written hash is the current recompute: the tree validates clean of
	// flow_mmd_drift right after the export (the drift loop closes).
	findings, err := design.ValidateFile(root, "design/flows/sign-up.mmd")
	require.NoError(t, err)
	for _, f := range findings {
		assert.NotEqual(t, "flow_mmd_drift", f.Rule, "a fresh export must not drift: %s", f.Message)
	}
}

// TestDesignExportHandler_FlowsTargetRepairsDrift pins the drift loop through
// the handler: after the flow source changes, the stale export re-verifies
// clean following a regeneration.
func TestDesignExportHandler_FlowsTargetRepairsDrift(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxFWriteTree(t, root)
	h := &designExportHandler{}

	_, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "flows"})
	require.NoError(t, err)

	// Edit the source: the recorded hash no longer matches the recompute.
	grown := strings.Replace(dxFSourceJSON, `"label": "Done"`, `"label": "Complete"`, 1)
	daWrite(t, root, "design/flows/sign-up.json", grown)
	findings, err := design.ValidateFile(root, "design/flows/sign-up.mmd")
	require.NoError(t, err)
	require.True(t, hasRuleFindings(findings, "flow_mmd_drift"), "a stale export must be flagged before the regeneration")

	_, err = h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "flows"})
	require.NoError(t, err)
	assert.Contains(t, string(dxFReadExport(t, root)), `s3["Complete"]`)
	findings, err = design.ValidateFile(root, "design/flows/sign-up.mmd")
	require.NoError(t, err)
	assert.False(t, hasRuleFindings(findings, "flow_mmd_drift"), "the regeneration repairs the drift")
}

// TestDesignExportHandler_FlowsTargetNotMixed pins the isolation rule: the
// flows target is refused mixed with token targets or with the screens
// target, and `all` never carries it.
func TestDesignExportHandler_FlowsTargetNotMixed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxFWriteTree(t, root)
	daWrite(t, root, "design/tokens/color.tokens.json", dxTokenJSON)
	h := &designExportHandler{}

	for _, targets := range []string{"css,flows", "screens,flows"} {
		res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": targets})
		require.Errorf(t, err, "targets=%s", targets)
		require.True(t, res.IsError)
		assert.Contains(t, res.Output, "alone", "targets=%s", targets)
	}

	// `all` remains token-only even with flow sources present.
	_, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	_, statErr := os.Stat(filepath.Join(root, "design", "flows", "sign-up.mmd"))
	assert.True(t, os.IsNotExist(statErr), "`all` must not write flow exports")
}

// TestDesignExportHandler_FlowsTargetOutDirRefused pins the no-override rule:
// the .mmd must stay beside its .json (the drift check keys on that), so an
// out_dir override is a usage error that writes nothing.
func TestDesignExportHandler_FlowsTargetOutDirRefused(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxFWriteTree(t, root)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"targets": "flows", "out_dir": "design/exports"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "out_dir")
	_, statErr := os.Stat(filepath.Join(root, "design", "flows", "sign-up.mmd"))
	assert.True(t, os.IsNotExist(statErr), "nothing is written from the refused run")
}

// TestDesignExportHandler_FlowsTargetEmptyRefused pins the sentinel: no flow
// sources, no exports — a usage error naming the authoring path.
func TestDesignExportHandler_FlowsTargetEmptyRefused(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWrite(t, root, "design/screens/home.html", dxFHome)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "flows"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "no flow sources")
	_, statErr := os.Stat(filepath.Join(root, "design", "flows", "sign-up.mmd"))
	assert.True(t, os.IsNotExist(statErr))
}

// TestDesignExportHandler_FlowsTargetBadSourceRefused pins the refusal: a
// malformed flow source refuses the whole export rather than regenerating a
// subset (partial exports would look current while being stale).
func TestDesignExportHandler_FlowsTargetBadSourceRefused(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxFWriteTree(t, root)
	daWrite(t, root, "design/flows/other.json", `{"name": "other", "steps": []}`)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "flows"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "refused")
	assert.Contains(t, res.Output, "other.json")
	_, statErr := os.Stat(filepath.Join(root, "design", "flows", "sign-up.mmd"))
	assert.True(t, os.IsNotExist(statErr), "no export is written from a refused run")
}

// denyFlowsClassifier denies writes into design/flows (the Gate-1 test twin
// of denyGeneratedClassifier, scoped to this target's artifact paths).
type denyFlowsClassifier struct{}

func (denyFlowsClassifier) ClassifyFileAccess(_ context.Context, filePath, resolvedPath, _ string) string {
	target := resolvedPath
	if target == "" {
		target = filePath
	}
	if strings.Contains(filepath.ToSlash(target), "/design/flows") {
		return "deny"
	}
	return "allow"
}

func (denyFlowsClassifier) IsFolderSessionAllowed(_ string) bool { return false }

// TestDesignExportHandler_FlowsTargetGate1Deny pins Gate-1 on the flow export
// write path: a policy deny on the artifact path blocks the run.
func TestDesignExportHandler_FlowsTargetGate1Deny(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxFWriteTree(t, root)
	h := &designExportHandler{}
	env := newTestEnv(t, root)
	env.FileAccessClassifier = denyFlowsClassifier{}

	res, err := h.Execute(newTestCtx(root), env, map[string]any{"targets": "flows"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "denied")
}

// hasRuleFindings reports whether the findings carry at least one of the rule.
func hasRuleFindings(findings []design.Finding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}
