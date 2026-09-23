//go:build !js

package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// ---------------------------------------------------------------------------
// design_critique findings sidecar (SP-140-6 §6d)
// ---------------------------------------------------------------------------

// dcFindingsSidecar reads and parses a target's findings sidecar (path per
// the exported naming rule the webui shares).
func dcFindingsSidecar(t *testing.T, root, targetLabel string) critiqueFindingsSidecarDoc {
	t.Helper()
	rel := CritiqueFindingsSidecarPath(targetLabel)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoErrorf(t, err, "sidecar for %s must exist at %s", targetLabel, rel)
	var doc critiqueFindingsSidecarDoc
	require.NoError(t, json.Unmarshal(data, &doc))
	return doc
}

// TestDesignCritique_WritesFindingsSidecar pins the §6d contract on the happy
// vision path: the critique writes <artifact>.findings.json with the target,
// rubric, source hash, visual marker, and the target's own findings.
func TestDesignCritique_WritesFindingsSidecar(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, _ := dcCritiqueEnv(t, root)
	// A scripted vision tier returning one finding, so the vision path (not
	// the §4a static fallback) produces the sidecar.
	env.VisionProcessor = &VisionProcessor{visionClient: &dcScriptedVisionClient{
		response: `[{"area":"hierarchy","severity":"major","note":"CTA reads secondary"}]`,
	}}
	h := &designCritiqueHandler{}

	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.svg"})
	require.NoError(t, err)
	require.False(t, res.IsError, "output: %s", res.Output)
	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	require.Len(t, out.Artifacts, 1)
	require.Equal(t, "design/.cache/renders/login.png", out.Artifacts[0].Path)

	doc := dcFindingsSidecar(t, root, "design/wireframes/login.svg")
	assert.Equal(t, "design/wireframes/login.svg", doc.Target)
	assert.Equal(t, "all", doc.Rubric)
	assert.NotEmpty(t, doc.Generated, "provenance: generated timestamp")
	assert.NotEmpty(t, doc.SourceHash, "provenance: §4e content hash of the render source")
	assert.True(t, doc.Visual, "the scripted vision tier ran")
	// Every persisted finding belongs to the sidecar's target.
	for _, f := range doc.Findings {
		assert.Equal(t, doc.Target, f.Target)
	}
	// The sidecar's findings are the run's findings for this target.
	own := 0
	for _, f := range out.Findings {
		if f.Target == "design/wireframes/login.svg" {
			own++
		}
	}
	assert.Len(t, doc.Findings, own)
}

// TestDesignCritique_FindingsSidecarIsPerTarget pins the whole-tree rule: one
// sidecar per critiqued target, each carrying only its own findings, and none
// for comparison renders.
func TestDesignCritique_FindingsSidecarIsPerTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, _ := dcCritiqueEnv(t, root)
	h := &designCritiqueHandler{}

	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/"})
	require.NoError(t, err)
	require.False(t, res.IsError, "output: %s", res.Output)
	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	require.NotEmpty(t, out.Artifacts)

	sidebarTargets := map[string]bool{}
	for _, a := range out.Artifacts {
		rel := CritiqueFindingsSidecarPath(a.Target)
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("sidecar missing for target %s at %s", a.Target, rel)
		}
		var doc critiqueFindingsSidecarDoc
		require.NoError(t, json.Unmarshal(data, &doc))
		sidebarTargets[doc.Target] = true
		for _, f := range doc.Findings {
			assert.Equal(t, doc.Target, f.Target, "a sidecar carries only its own target's findings")
		}
	}
	// Every non-compare artifact has a sidecar naming a distinct target.
	nonCompare := 0
	for _, a := range out.Artifacts {
		if !strings.Contains(a.Path, "~") {
			nonCompare++
		}
	}
	assert.Len(t, sidebarTargets, nonCompare, "one sidecar per non-compare artifact")
}

// TestDesignCritique_FindingsSidecarNonVision pins the §4a degradation marker:
// a run with no vision tier writes visual=false with its static findings.
func TestDesignCritique_FindingsSidecarNonVision(t *testing.T) {
	root := t.TempDir()
	dcWriteTree(t, root)
	env, _ := dcCritiqueEnvNoVision(t, root)
	h := &designCritiqueHandler{}

	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.svg"})
	require.NoError(t, err)
	require.False(t, res.IsError, "output: %s", res.Output)
	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	require.False(t, out.Visual)

	doc := dcFindingsSidecar(t, root, "design/wireframes/login.svg")
	assert.False(t, doc.Visual, "the sidecar records the §4a visual:false degradation")
}

// TestDesignCritique_FindingsSidecarRefreshesOnRerun pins that a second run
// (a cache hit for the PNG) still refreshes the sidecar: the findings record
// the LAST critique, not the first.
func TestDesignCritique_FindingsSidecarRefreshesOnRerun(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, mock := dcCritiqueEnv(t, root)
	h := &designCritiqueHandler{}

	_, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.svg"})
	require.NoError(t, err)
	first := dcFindingsSidecar(t, root, "design/wireframes/login.svg")

	res2, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.svg"})
	require.NoError(t, err)
	out2, ok := res2.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	require.Equal(t, 1, out2.CacheHits, "the PNG came from the cache")
	assert.GreaterOrEqual(t, mock.calls, 1)

	second := dcFindingsSidecar(t, root, "design/wireframes/login.svg")
	assert.GreaterOrEqual(t, second.Generated, first.Generated, "the sidecar refreshed")
}

// TestDesignCritique_FindingsSidecarNotAValidatorSource pins invariant 2:
// the sidecar is derived output — design_validate must not report it (a
// findings-for-findings rule would be noise).
func TestDesignCritique_FindingsSidecarNotAValidatorSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, _ := dcCritiqueEnv(t, root)
	h := &designCritiqueHandler{}

	_, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.svg"})
	require.NoError(t, err)

	findings, verr := design.ValidateTree(root)
	require.NoError(t, verr)
	for _, f := range findings {
		assert.NotContains(t, f.File, ".cache/", "derived artifacts are never validator sources")
	}
}
