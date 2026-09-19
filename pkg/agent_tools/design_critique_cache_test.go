//go:build !js

package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// design_critique render cache + whole-tree cost cap (SP-140-4 §4e, item 4.3)
//
// AC 1: "Cache: second critique of unchanged content skips re-render (test
// asserts render-count)." — a repeat critique of a target whose source bytes
// are unchanged reuses the cached PNG (design/.cache/renders/<stem>.png) and
// its provenance header, and does NOT rasterize again. The assertions read the
// mock browser's render count (mock.calls) and the structured RenderCount.
//
// AC 2: "Whole-tree cap enforced with explicit notice at 21+ screens." — a
// whole-tree (design/) critique is capped at max_screens (default 20) and, when
// the tree exceeds it, the run says so explicitly and does not render/critique
// beyond the cap.
//
// The cache key is the source content hash + the render material (viewport +
// rubric), so a re-render with a different viewport or rubric is a cache miss
// rather than a stale reuse. Both halves are asserted.
// ---------------------------------------------------------------------------

// dcWriteManySVGs seeds n renderable wireframes (login-00.svg …) plus the
// standard tree, returning the number written.
func dcWriteManySVGs(t *testing.T, root string, n int) {
	t.Helper()
	dcWriteTree(t, root)
	for i := 0; i < n; i++ {
		rel := fmt.Sprintf("design/wireframes/login-%02d.svg", i)
		body := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Screen %d</text>
</svg>`, i)
		dcWrite(t, root, rel, body)
	}
}

// ---------------------------------------------------------------------------
// Content hash / cache key
// ---------------------------------------------------------------------------

func TestCritiqueContentHash_StableAndContentAddressed(t *testing.T) {
	t.Parallel()

	h := critiqueContentHash([]byte("flowchart TD\n  a --> b\n"))
	// Deterministic: the same bytes hash to the same digest.
	assert.Equal(t, h, critiqueContentHash([]byte("flowchart TD\n  a --> b\n")))
	// 64 hex chars (SHA-256).
	assert.Len(t, h, 64)
	// Content-addressed: a one-byte change changes the hash.
	assert.NotEqual(t, h, critiqueContentHash([]byte("flowchart TD\n  a --> c\n")))
	// Empty content still hashes (no panic, no empty digest).
	assert.Len(t, critiqueContentHash(nil), 64)
}

func TestCritiqueRenderMaterial_IncludesViewportAndRubric(t *testing.T) {
	t.Parallel()

	base := critiqueRenderMaterial(RenderInputOptions{ViewportWidth: 1280, ViewportHeight: 720}, "prompt")
	assert.Contains(t, base, "viewport=1280x720")
	assert.Contains(t, base, "rubric=prompt")

	// A different viewport ⇒ different material (so a re-render at a new size
	// cannot reuse a stale critique).
	other := critiqueRenderMaterial(RenderInputOptions{ViewportWidth: 390, ViewportHeight: 844}, "prompt")
	assert.NotEqual(t, base, other)
	// A different rubric/instruction ⇒ different material.
	assert.NotEqual(t, base, critiqueRenderMaterial(RenderInputOptions{ViewportWidth: 1280, ViewportHeight: 720}, "other"))

	// Integral floats render without a trailing ".0" so int vs float64 arg
	// forms produce the same material (and therefore the same cache key).
	assert.Contains(t, critiqueRenderMaterial(RenderInputOptions{ViewportWidth: 1280, ViewportHeight: 720}, ""), "1280x720")
}

func TestCritiqueCacheKey_ComposesHashAndMaterial(t *testing.T) {
	t.Parallel()

	k := critiqueCacheKey("hash", "material")
	assert.Equal(t, k, critiqueCacheKey("hash", "material"))
	assert.NotEqual(t, k, critiqueCacheKey("hash", "other"))
	assert.NotEqual(t, k, critiqueCacheKey("other", "material"))
	assert.Len(t, k, 64)
}

// ---------------------------------------------------------------------------
// AC 1 — cache hit skips the re-render; cache miss re-renders
// ---------------------------------------------------------------------------

// TestDesignCritiqueHandler_CacheHitSkipsRerender is the §4e acceptance test:
// the second critique of unchanged content does not re-rasterize. The first run
// renders (cache miss), the second reuses the stored artifact (cache hit) —
// asserted by the mock browser's render count and by the structured
// renderCount/cacheHits split.
func TestDesignCritiqueHandler_CacheHitSkipsRerender(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, mock := dcCritiqueEnv(t, root)
	h := &designCritiqueHandler{}

	// First critique: cold cache → exactly one render.
	res1, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.svg"})
	require.NoError(t, err)
	require.False(t, res1.IsError, "output: %s", res1.Output)
	out1, ok := res1.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	require.Equal(t, 1, out1.RenderCount, "first critique renders")
	assert.Equal(t, 0, out1.CacheHits, "nothing to reuse on a cold cache")
	require.Equal(t, 1, mock.calls)

	// Second critique of unchanged content: cache hit → no new render.
	res2, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.svg"})
	require.NoError(t, err)
	require.False(t, res2.IsError, "output: %s", res2.Output)
	out2, ok := res2.StructuredOut.(critiqueOutput)
	require.True(t, ok)

	assert.Equal(t, 1, mock.calls, "AC: the second critique of unchanged content must NOT re-render")
	assert.Equal(t, 0, out2.RenderCount, "no rasterization happened on the cache hit")
	assert.Equal(t, 1, out2.CacheHits, "the run reused the cached render")
	assert.Contains(t, out2.Note, "reused from the render cache")

	// The cached artifact still rides the result, with its provenance.
	require.Len(t, out2.Artifacts, 1)
	assert.Equal(t, "design/.cache/renders/login.png", out2.Artifacts[0].Path)
	assert.True(t, out2.Artifacts[0].Cached, "the artifact reports it was cached")
	require.Len(t, res2.Images, 1, "SP-137 attachment re-derived from the cached PNG")
	assert.Equal(t, "image/png", res2.Images[0].MIMEType)

	// The cached PNG on disk still carries the SP-140 invariant 2 header, and
	// the header records the §4e content hash so a consumer can verify it.
	raw := dcArtifactPNG(t, root, out2.Artifacts[0].Path)
	assert.Contains(t, string(raw), provenanceHeaderPrefix)
	assert.Contains(t, string(raw), "sourceHash: ")
	assert.Contains(t, string(raw), "renderMaterial: ")

	// The cached provenance records the same sourceHash the run keyed on.
	assert.Contains(t, out2.Artifacts[0].Provenance, "sourceHash: ")
	assert.Equal(t, extractProvenance(t, string(raw)), out2.Artifacts[0].Provenance)
}

// TestDesignCritiqueHandler_CacheMissRerendersOnChangedContent pins the other
// half: a change to the source bytes invalidates the cache (the content hash
// changed), so the target is re-rendered.
func TestDesignCritiqueHandler_CacheMissRerendersOnChangedContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, mock := dcCritiqueEnv(t, root)
	h := &designCritiqueHandler{}

	res1, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.svg"})
	require.NoError(t, err)
	require.False(t, res1.IsError, "output: %s", res1.Output)
	out1, ok := res1.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	require.Equal(t, 1, out1.RenderCount, "first critique renders")
	assert.Equal(t, 0, out1.CacheHits, "nothing to reuse on a cold cache")
	require.Equal(t, 1, mock.calls)
	// Change the source content — same path, different bytes.
	dcWrite(t, root, "design/wireframes/login.svg", dcTestSVG+"\n<!-- revised -->\n")

	res2, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.svg"})
	require.NoError(t, err)
	out2, ok := res2.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.Equal(t, 2, mock.calls, "changed content must re-render (cache miss)")
	assert.Equal(t, 1, out2.RenderCount)
	assert.Equal(t, 0, out2.CacheHits)
}

// TestDesignCritiqueHandler_CacheMissOnViewportChange pins that the cache key
// accounts for the render material: re-critiquing unchanged content at a
// different viewport re-renders rather than returning a stale critique.
func TestDesignCritiqueHandler_CacheMissOnViewportChange(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, mock := dcCritiqueEnv(t, root)
	h := &designCritiqueHandler{}

	_, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.svg"})
	require.NoError(t, err)
	require.Equal(t, 1, mock.calls)

	env2, mock2 := dcCritiqueEnv(t, root)
	_, err = h.Execute(newTestCtx(root), env2, map[string]any{
		"target":          "design/wireframes/login.svg",
		"viewport_width":  390,
		"viewport_height": 844,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, mock2.calls, "a different viewport must not reuse the cached render")
}

// TestDesignCritiqueHandler_CacheMissOnRubricChange pins the rubric half of the
// render material: the same pixels judged under a different rubric re-render
// (the critique must not be silently stale against a new rubric).
func TestDesignCritiqueHandler_CacheMissOnRubricChange(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, _ := dcCritiqueEnv(t, root)
	h := &designCritiqueHandler{}

	_, err := h.Execute(newTestCtx(root), env, map[string]any{
		"target": "design/wireframes/login.svg",
		"rubric": designRubricAll,
	})
	require.NoError(t, err)

	env2, mock2 := dcCritiqueEnv(t, root)
	_, err = h.Execute(newTestCtx(root), env2, map[string]any{
		"target": "design/wireframes/login.svg",
		"rubric": designRubricConsistency,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, mock2.calls, "a different rubric must not reuse the cached render")
}

// TestDesignCritiqueHandler_CacheHashForMermaidUsesMMD pins that a flow's cache
// hash comes from the .mmd content, not the generated HTML page: reusing the
// cached flow render is keyed on the source the caller edits.
func TestDesignCritiqueHandler_CacheHashForMermaidUsesMMD(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, mock := dcCritiqueEnv(t, root)
	h := &designCritiqueHandler{}

	_, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/flows/sign-up.mmd"})
	require.NoError(t, err)
	require.Equal(t, 1, mock.calls)

	// Unchanged .mmd content → cache hit.
	_, err = h.Execute(newTestCtx(root), env, map[string]any{"target": "design/flows/sign-up.mmd"})
	require.NoError(t, err)
	assert.Equal(t, 1, mock.calls, "an unchanged .mmd must not re-render")

	// An edited .mmd → cache miss.
	dcWrite(t, root, "design/flows/sign-up.mmd", "flowchart TD\n  login --> home\n  home --> done\n")
	_, err = h.Execute(newTestCtx(root), env, map[string]any{"target": "design/flows/sign-up.mmd"})
	require.NoError(t, err)
	assert.Equal(t, 2, mock.calls, "an edited .mmd must re-render")
}

// TestLoadCachedCritiqueArtifact_RejectsStaleAndCorruptEntries pins the
// defensive half of the cache: a missing sidecar, a mismatched key value, or
// corrupt JSON is a miss (the caller re-renders) rather than an error.
func TestLoadCachedCritiqueArtifact_RejectsStaleAndCorruptEntries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	env := newTestEnv(t, root)

	artifactPath := "design/.cache/renders/login.png"
	dcWrite(t, root, artifactPath, string(drTinyPNG))

	// No sidecar yet.
	_, ok := loadCachedCritiqueArtifact(newTestCtx(root), artifactPath, critiqueCacheKey("h", "m"))
	assert.False(t, ok, "no sidecar is a cache miss")

	// A sidecar whose material does not match the requested key.
	dcWrite(t, root, artifactPath+critiqueCacheFilenameSuffix,
		`{"sourceHash":"h","renderMaterial":"m","artifact":"`+artifactPath+`"}`)
	_, ok = loadCachedCritiqueArtifact(newTestCtx(root), artifactPath, critiqueCacheKey("h", "DIFFERENT"))
	assert.False(t, ok, "a mismatched render material is a cache miss")

	// Corrupt JSON is a miss, not a panic/error.
	dcWrite(t, root, artifactPath+critiqueCacheFilenameSuffix, "{not json")
	_, ok = loadCachedCritiqueArtifact(newTestCtx(root), artifactPath, critiqueCacheKey("h", "m"))
	assert.False(t, ok, "corrupt sidecar JSON is a cache miss")

	// A sidecar naming a different artifact is a miss (stale copy / rename).
	dcWrite(t, root, artifactPath+critiqueCacheFilenameSuffix,
		`{"sourceHash":"h","renderMaterial":"m","artifact":"design/.cache/renders/other.png"}`)
	_, ok = loadCachedCritiqueArtifact(newTestCtx(root), artifactPath, critiqueCacheKey("h", "m"))
	assert.False(t, ok, "a sidecar naming another artifact is a cache miss")

	// A matching sidecar but a missing PNG is a miss.
	dcWrite(t, root, artifactPath+critiqueCacheFilenameSuffix,
		`{"sourceHash":"h","renderMaterial":"m"}`)
	require.NoError(t, os.Remove(filepath.Join(root, filepath.FromSlash(artifactPath))))
	_, ok = loadCachedCritiqueArtifact(newTestCtx(root), artifactPath, critiqueCacheKey("h", "m"))
	assert.False(t, ok, "a sidecar without its PNG is a cache miss")

	// A matching sidecar + PNG is a hit, and the attachment is rebuilt.
	dcWrite(t, root, artifactPath, string(drTinyPNG))
	hit, ok := loadCachedCritiqueArtifact(newTestCtx(root), artifactPath, critiqueCacheKey("h", "m"))
	require.True(t, ok, "a matching sidecar + PNG is a cache hit")
	assert.True(t, hit.artifact.Cached)
	require.Len(t, hit.attachment.Images, 1)
	_ = env
}

// TestWriteCritiqueCache_RoundTripsKeyMaterial pins that the sidecar written by
// a render is exactly what the lookup expects, so the cache is self-consistent
// (a write followed by a lookup for the same key is a hit).
func TestWriteCritiqueCache_RoundTripsKeyMaterial(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ctx := newTestCtx(root)

	artifactPath := "design/.cache/renders/login.png"
	dcWrite(t, root, artifactPath, string(drTinyPNG))

	material := critiqueRenderMaterial(RenderInputOptions{ViewportWidth: 1280, ViewportHeight: 720}, "prompt")
	key := critiqueCacheKey("abc", material)
	require.NoError(t, writeCritiqueCache(ctx, artifactPath, key, "abc", material))

	hit, ok := loadCachedCritiqueArtifact(ctx, artifactPath, key)
	require.True(t, ok, "a freshly written cache entry must be a hit for its own key")
	assert.True(t, hit.artifact.Cached)

	// A key from different material must not hit.
	_, ok = loadCachedCritiqueArtifact(ctx, artifactPath, critiqueCacheKey("abc", "other"))
	assert.False(t, ok)
}

// TestDesignCritiqueHandler_CacheHitStillRunsVision pins that a cache hit skips
// the *render*, not the critique: the vision tier is still asked to judge the
// cached pixels, so the findings are not silently dropped.
func TestDesignCritiqueHandler_CacheHitStillRunsVision(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)

	scripted := &dcScriptedVisionClient{response: `[{"area":"hierarchy","severity":"major","note":"CTA reads secondary"}]`}
	env, mock := dcCritiqueEnv(t, root)
	env.VisionProcessor = &VisionProcessor{visionClient: scripted}
	h := &designCritiqueHandler{}

	_, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.svg"})
	require.NoError(t, err)
	require.Equal(t, 1, scripted.calls)

	res2, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.svg"})
	require.NoError(t, err)
	out2, ok := res2.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.Equal(t, 1, mock.calls, "the render was reused")
	assert.Equal(t, 2, scripted.calls, "the critique still ran against the cached pixels")
	assert.True(t, out2.Visual)
	require.Len(t, out2.Findings, 1)
	assert.Equal(t, "CTA reads secondary", out2.Findings[0].Note)
}

// ---------------------------------------------------------------------------
// AC 2 — whole-tree cap with explicit notice
// ---------------------------------------------------------------------------

// TestDesignCritiqueHandler_WholeTreeCapAtDefault20 is the §4e acceptance test:
// a whole-tree critique of a 21+ screen tree renders at most 20 screens, fires
// no more than 20 vision calls, and says so explicitly.
func TestDesignCritiqueHandler_WholeTreeCapAtDefault20(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// 4 standard targets + 20 more wireframes = 24 renderable targets.
	dcWriteManySVGs(t, root, 20)

	scripted := &dcScriptedVisionClient{response: `[{"area":"hierarchy","severity":"info","note":"n"}]`}
	env, mock := dcCritiqueEnv(t, root)
	env.VisionProcessor = &VisionProcessor{visionClient: scripted}

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design"})
	require.NoError(t, err)
	require.False(t, res.IsError, "output: %s", res.Output)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)

	// AC: capped at 20 screens.
	assert.Equal(t, designCritiqueDefaultMaxScreens, out.MaxScreens)
	assert.Equal(t, 20, out.RenderCount, "at most 20 screens may be rendered")
	assert.LessOrEqual(t, mock.calls, 20, "no more than 20 browser renders")
	assert.LessOrEqual(t, scripted.calls, 20, "no more than 20 vision calls")
	require.Len(t, out.Artifacts, 20)

	// AC: explicit notice.
	assert.True(t, out.Capped, "the run must report that it was capped")
	assert.Equal(t, 4, out.SkippedCount, "24 targets - 20 cap = 4 skipped")
	require.Contains(t, out.Note, "capped at 20 screens")
	assert.Contains(t, out.Note, "narrowed critiques")
	assert.Contains(t, res.Output, "WARNING: critique capped at 20 screens")
	assert.Contains(t, res.Output, "narrowed critiques")
	assert.Contains(t, res.Output, "4 further target(s)")

	// The cap is machine-readable too.
	data, err := json.Marshal(out)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Equal(t, true, raw["capped"])
	assert.Equal(t, float64(20), raw["maxScreens"])
	assert.Equal(t, float64(4), raw["skippedCount"])
}

// TestDesignCritiqueHandler_WholeTreeUnderCapHasNoNotice pins that the cap is
// only announced when it actually bit: a small tree is critiqued in full with
// no cap notice.
func TestDesignCritiqueHandler_WholeTreeUnderCapHasNoNotice(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, mock := dcCritiqueEnv(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design"})
	require.NoError(t, err)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.False(t, out.Capped)
	assert.Equal(t, 0, out.SkippedCount)
	assert.Equal(t, designCritiqueDefaultMaxScreens, out.MaxScreens, "the cap in force is always reported")
	assert.NotContains(t, out.Note, "capped at")
	assert.Equal(t, 4, mock.calls, "a tree under the cap is critiqued in full")
}

// TestDesignCritiqueHandler_MaxScreensArgOverridesCap pins that max_screens is
// a real tool argument: lowering it caps tighter, and the notice names the
// effective value.
func TestDesignCritiqueHandler_MaxScreensArgOverridesCap(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteManySVGs(t, root, 20) // 24 renderable targets

	env, mock := dcCritiqueEnv(t, root)
	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{
		"target":      "design",
		"max_screens": 5,
	})
	require.NoError(t, err)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.Equal(t, 5, out.MaxScreens)
	assert.Equal(t, 5, out.RenderCount)
	assert.Equal(t, 5, mock.calls)
	assert.Equal(t, 19, out.SkippedCount)
	require.Contains(t, out.Note, "capped at 5 screens")
	assert.Contains(t, res.Output, "capped at 5 screens")
}

// TestDesignCritiqueHandler_MaxScreensLargerThanTreeNoCap pins the upper half:
// raising the cap past the tree size critiques everything with no notice.
func TestDesignCritiqueHandler_MaxScreensLargerThanTreeNoCap(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, mock := dcCritiqueEnv(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{
		"target":      "design",
		"max_screens": 100,
	})
	require.NoError(t, err)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.False(t, out.Capped)
	assert.Equal(t, 100, out.MaxScreens)
	assert.Equal(t, 4, mock.calls)
}

// TestDesignCritiqueHandler_CapDoesNotApplyToNamedTargets pins the scope of the
// cap: a single named target, a slug, and a subtree are already the "narrowed
// critique" the notice recommends, so they are never truncated even when the
// cap is tiny.
func TestDesignCritiqueHandler_CapDoesNotApplyToNarrowedTargets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteManySVGs(t, root, 20)

	cases := []struct {
		target   string
		wantRend int
	}{
		{"design/wireframes/login.svg", 1},
		{"login", 1},
		{"design/wireframes", 22}, // 2 standard + 20 seeded
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.target, func(t *testing.T) {
			env, mock := dcCritiqueEnv(t, root)
			h := &designCritiqueHandler{}
			res, err := h.Execute(newTestCtx(root), env, map[string]any{
				"target":      tc.target,
				"max_screens": 3,
			})
			require.NoError(t, err)
			out, ok := res.StructuredOut.(critiqueOutput)
			require.True(t, ok)
			assert.False(t, out.Capped, "a narrowed target is never capped")
			assert.Equal(t, tc.wantRend, mock.calls)
			assert.Equal(t, tc.wantRend, out.RenderCount)
		})
	}
}

// TestCritiqueMaxScreensArg covers the arg parser's accept/reject surface.
func TestCritiqueMaxScreensArg(t *testing.T) {
	t.Parallel()

	// Absent/missing → default.
	n, err := critiqueMaxScreensArg(map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, designCritiqueDefaultMaxScreens, n)
	n, err = critiqueMaxScreensArg(map[string]any{"max_screens": nil})
	require.NoError(t, err)
	assert.Equal(t, designCritiqueDefaultMaxScreens, n)

	// int / int64 / float64 that is integral.
	for _, v := range []any{7, int64(8), float64(9)} {
		n, err = critiqueMaxScreensArg(map[string]any{"max_screens": v})
		require.NoError(t, err, "value %v", v)
		assert.Greater(t, n, 0)
	}

	// Rejections: wrong type, non-integral float, out-of-range.
	for _, bad := range []any{"20", true, 1.5, 0, -3} {
		_, err = critiqueMaxScreensArg(map[string]any{"max_screens": bad})
		assert.Error(t, err, "value %v must be rejected", bad)
	}

	// Validate surfaces the same rejections, so a bad arg fails before Execute.
	h := &designCritiqueHandler{}
	assert.Error(t, h.Validate(map[string]any{"target": "design", "max_screens": "20"}))
	assert.Error(t, h.Validate(map[string]any{"target": "design", "max_screens": -1}))
	assert.NoError(t, h.Validate(map[string]any{"target": "design", "max_screens": 5}))
}

// TestDesignCritiqueHandler_RejectsInvalidMaxScreens pins the whole-handler
// contract: an invalid max_screens fails the call (usage error) rather than
// silently falling back to the default and spending the budget.
func TestDesignCritiqueHandler_RejectsInvalidMaxScreens(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, mock := dcCritiqueEnv(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{
		"target":      "design",
		"max_screens": "many",
	})
	require.Error(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Output, "max_screens")
	assert.Equal(t, 0, mock.calls, "a rejected arg must not render anything")
}

// TestCritiqueCapNotice pins the notice text: it names the cap, the skipped
// count, and the remedy.
func TestCritiqueCapNotice(t *testing.T) {
	t.Parallel()
	notice := critiqueCapNotice(20, 40)
	assert.Contains(t, notice, "capped at 20 screens")
	assert.Contains(t, notice, "narrowed critiques")
	assert.Contains(t, notice, "40 further target(s)")
	assert.True(t, strings.HasPrefix(notice, designCritiqueCapNoticePrefix))
}

// TestCritiqueCacheNotice pins the cache-disclosure notice (singular/plural).
func TestCritiqueCacheNotice(t *testing.T) {
	t.Parallel()
	assert.Contains(t, critiqueCacheNotice(1), "1 render was reused")
	assert.Contains(t, critiqueCacheNotice(3), "3 renders were reused")
	assert.Contains(t, critiqueCacheNotice(3), "content unchanged")
}

// TestCleanCritiqueTarget pins the normalization the cap's scope test uses, so
// it agrees with discovery.
func TestCleanCritiqueTarget(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"design", "design/", " design ", "./design", "design//"} {
		assert.True(t, targetIsWholeTree(cleanCritiqueTarget(in)),
			"%q must classify as the whole tree", in)
		assert.Equal(t, "design", cleanCritiqueTarget(in))
	}
	for _, in := range []string{"design/wireframes", "design/wireframes/login.svg", "login"} {
		assert.False(t, targetIsWholeTree(cleanCritiqueTarget(in)),
			"%q must not classify as the whole tree", in)
	}
}

// TestDesignCritiqueHandler_CacheAndCapNoticeCoexist pins that the two per-run
// notices (a §4e cache hit and a §4e cap) do not overwrite each other: both
// reach the output.
func TestDesignCritiqueHandler_CacheAndCapNoticeCoexist(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteManySVGs(t, root, 20) // 24 targets

	env, _ := dcCritiqueEnv(t, root)
	h := &designCritiqueHandler{}

	// First capped run populates the cache for the first 20 targets.
	res1, err := h.Execute(newTestCtx(root), env, map[string]any{
		"target":      "design",
		"max_screens": 20,
	})
	require.NoError(t, err)
	out1, ok := res1.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	require.True(t, out1.Capped)

	// Second run: same caps, all cached → cache notice AND cap notice.
	res2, err := h.Execute(newTestCtx(root), env, map[string]any{
		"target":      "design",
		"max_screens": 20,
	})
	require.NoError(t, err)
	out2, ok := res2.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.Equal(t, 20, out2.CacheHits, "the second capped run reuses all 20 renders")
	assert.True(t, out2.Capped)
	assert.Contains(t, out2.Note, "capped at 20 screens")
	assert.Contains(t, out2.Note, "reused from the render cache")
	assert.Contains(t, res2.Output, "reused from the render cache")
	assert.Contains(t, res2.Output, "WARNING: critique capped at 20 screens")
}
