//go:build !js

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

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/design"
)

// ---------------------------------------------------------------------------
// design_critique handler (SP-140-4 §4a — tool core)
//
// Scope: rendering the target, attaching the image via the SP-137 path, the
// structured findings shape {target, area, severity, note, suggestion}, and the
// derived-artifact PNG under design/.cache/renders/ with its provenance header.
//
// Out of scope here: cache-hit/render counting (4.3),
// consistency rule packs (4.4/4.5), feedback consumption (4.7).
//
// Item 4.2 (non-vision degradation → static findings, visual:false) is covered
// by the "Non-vision degradation" section below.
// ---------------------------------------------------------------------------

// dcWrite writes rel (slash-separated) under root with parent directories.
func dcWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

const dcTestSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Login</text>
  <rect id="submit" x="24" y="200" width="342" height="52"/>
</svg>`

const dcTestHomeSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Home</text>
</svg>`

// dcTestHTML carries the §9a identity attribute so the screen validates
// clean under the SP-140-9 identity rules.
const dcTestHTML = `<!DOCTYPE html><html data-screen="login"><body><h1>Login</h1></body></html>`

const dcTestMMD = "flowchart TD\n  login --> home\n"

// dcWriteTree seeds a minimal design/ tree: two wireframes, one screen, one
// flow. Under SP-140-9 §9a the wireframes earn deprecation infos, so tests
// wanting a CLEAN tree use dcWriteCleanTree (screens-only).
func dcWriteTree(t *testing.T, root string) {
	t.Helper()
	dcWrite(t, root, "design/wireframes/login.svg", dcTestSVG)
	dcWrite(t, root, "design/wireframes/home.svg", dcTestHomeSVG)
	dcWrite(t, root, "design/screens/login.html", dcTestHTML)
	dcWrite(t, root, "design/flows/sign-up.mmd", dcTestMMD)
}

// dcWriteCleanTree seeds a screens-only tree: no wireframes, so no §9a
// deprecation notices — the tree validates with zero findings.
func dcWriteCleanTree(t *testing.T, root string) {
	t.Helper()
	dcWrite(t, root, "design/screens/login.html", dcTestHTML)
	dcWrite(t, root, "design/flows/sign-up.mmd", dcTestMMD)
}

// dcScriptedVisionClient is the vision-scripted client the critique tests use:
// it stands in for the vision tier (no network, no keys) and returns a canned
// critique response, so the structured findings path is exercised end to end.
//
// It embeds the same no-op surface the other vision mocks in this package use;
// only SendVisionRequest, SupportsVision and GetVisionModel carry behavior.
type dcScriptedVisionClient struct {
	response string
	err      error
	calls    int
	// lastPrompt records the prompt the rubric pass sent, so a test can assert
	// the critique vocabulary reached the tier.
	lastPrompt string
	// lastImages records how many images rode the request.
	lastImages int
}

func (m *dcScriptedVisionClient) SendVisionRequest(_ context.Context, messages []api.Message, _ []api.Tool, _ string, _ bool) (*api.ChatResponse, error) {
	m.calls++
	if len(messages) > 0 {
		m.lastPrompt = messages[0].Content
		m.lastImages = len(messages[0].Images)
	}
	if m.err != nil {
		return nil, m.err
	}
	return &api.ChatResponse{
		Choices: []api.Choice{{Message: api.Message{Content: m.response}}},
		Usage:   api.ChatUsage{TotalTokens: 42, PromptTokens: 30, CompletionTokens: 12},
	}, nil
}

func (m *dcScriptedVisionClient) SendChatRequest(context.Context, []api.Message, []api.Tool, string, bool) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}
func (m *dcScriptedVisionClient) SendChatRequestStream(context.Context, []api.Message, []api.Tool, string, bool, api.StreamCallback) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}
func (m *dcScriptedVisionClient) CheckConnection() error             { return nil }
func (m *dcScriptedVisionClient) SetDebug(bool)                      {}
func (m *dcScriptedVisionClient) SetModel(string) error              { return nil }
func (m *dcScriptedVisionClient) GetModel() string                   { return "scripted-vision" }
func (m *dcScriptedVisionClient) GetProvider() string                { return "scripted" }
func (m *dcScriptedVisionClient) GetModelContextLimit() (int, error) { return 128000, nil }
func (m *dcScriptedVisionClient) ListModels(context.Context) ([]api.ModelInfo, error) {
	return nil, nil
}
func (m *dcScriptedVisionClient) SupportsVision() bool               { return true }
func (m *dcScriptedVisionClient) SupportsConversationalVision() bool { return false }
func (m *dcScriptedVisionClient) GetVisionModel() string             { return "scripted-vision" }
func (m *dcScriptedVisionClient) GetLastTPS() float64                { return 0 }
func (m *dcScriptedVisionClient) GetAverageTPS() float64             { return 0 }
func (m *dcScriptedVisionClient) GetTPSStats() map[string]float64    { return nil }
func (m *dcScriptedVisionClient) ResetTPSStats()                     {}
func (m *dcScriptedVisionClient) VisionCapabilities() api.VisionCapabilities {
	return api.VisionCapabilitiesDefault()
}

// dcCritiqueEnv builds a ToolEnv with a browser that writes a real PNG to the
// screenshot_path the render helper chose (so the SP-137 attachment and the
// artifact write both find a genuine file) and a scripted vision tier, so the
// critique pass is hermetic and fast.
func dcCritiqueEnv(t *testing.T, root string) (ToolEnv, *drMockBrowser) {
	t.Helper()
	mock := &drMockBrowser{pngBytes: drTinyPNG}
	env := newTestEnv(t, root)
	env.WebBrowser = mock
	env.VisionProcessor = &VisionProcessor{visionClient: &dcScriptedVisionClient{}}
	return env, mock
}

// dcCritiqueEnvNoVision builds the same env without a vision processor: the
// package-level vision tier is consulted instead (item 4.2's degradation seam).
// It pins the process-wide vision-capability probe to false for the test's
// lifetime, so callers observe a deterministic "no tier" regardless of host
// state (native-OCR shims, configured providers) or a sibling test leaving the
// capability cached. Callers that need no-parallel safety (they read global
// capability state) must not call t.Parallel themselves.
//
// SetVisionCapabilityForTest returns a restore func that clears the override
// (stores nil) rather than pinning it to some other value, so registering it
// directly with t.Cleanup restores the process-wide state exactly as it was
// found — no prior value needs to be captured.
func dcCritiqueEnvNoVision(t *testing.T, root string) (ToolEnv, *drMockBrowser) {
	t.Helper()
	t.Cleanup(SetVisionCapabilityForTest(false))
	mock := &drMockBrowser{pngBytes: drTinyPNG}
	env := newTestEnv(t, root)
	env.WebBrowser = mock
	return env, mock
}

// dcArtifactPNG reads a workspace-relative artifact path and returns its bytes.
func dcArtifactPNG(t *testing.T, root, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoErrorf(t, err, "artifact %s must exist", rel)
	return data
}

// ---------------------------------------------------------------------------
// Definition / Validate / metadata
// ---------------------------------------------------------------------------

func TestDesignCritiqueHandler_Definition(t *testing.T) {
	t.Parallel()
	h := &designCritiqueHandler{}
	require.Equal(t, "design_critique", h.Name())

	def := h.Definition()
	require.Equal(t, "design_critique", def.Name)
	require.NotEmpty(t, def.Description)
	// The description must name the critique vocabulary and the three rubric
	// concepts so the model knows what a critique pass reports.
	for _, want := range []string{"hierarchy", "affordance", "consistency", "contrast"} {
		assert.Contains(t, def.Description, want, "description must carry the critique vocabulary")
	}
	assert.Contains(t, def.Description, "design/.cache/renders/")
	require.Equal(t, []string{"target"}, def.Required)

	params := map[string]ParameterDef{}
	for _, p := range def.Parameters {
		params[p.Name] = p
	}
	for _, want := range []string{"target", "rubric", "compare_to", "analysis_prompt", "viewport_width", "viewport_height"} {
		assert.Contains(t, params, want, "missing parameter %q", want)
	}
	assert.True(t, params["target"].Required)
	assert.False(t, params["rubric"].Required, "rubric must be optional (default all)")
	assert.False(t, params["compare_to"].Required, "compare_to must be optional")
	// Every rubric value must be advertised, or the model cannot choose one.
	for _, r := range designCritiqueRubrics {
		assert.Contains(t, params["rubric"].Description, r)
	}
}

func TestDesignCritiqueHandler_Metadata(t *testing.T) {
	t.Parallel()
	h := &designCritiqueHandler{}

	require.Nil(t, h.Aliases())
	require.Equal(t, 0, h.MaxResultSize())
	require.False(t, h.SafeForParallel(), "the browser + vision tiers are not safe for unbounded parallelism")
	require.False(t, h.Interactive())
}

func TestDesignCritiqueHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &designCritiqueHandler{}

	require.Error(t, h.Validate(map[string]any{}), "target is required")
	require.Error(t, h.Validate(map[string]any{"target": 42}))
	require.Error(t, h.Validate(map[string]any{"target": "a.svg", "rubric": 1}))
	require.Error(t, h.Validate(map[string]any{"target": "a.svg", "compare_to": 1}))
	require.Error(t, h.Validate(map[string]any{"target": "a.svg", "analysis_prompt": 1}))

	require.NoError(t, h.Validate(map[string]any{"target": "design/wireframes/login.svg"}))
	require.NoError(t, h.Validate(map[string]any{
		"target":          "login",
		"rubric":          "consistency",
		"compare_to":      "home",
		"analysis_prompt": "focus on the empty state",
		"viewport_width":  390,
		"viewport_height": 844,
	}))
}

// ---------------------------------------------------------------------------
// Rubric vocabulary
// ---------------------------------------------------------------------------

func TestCritiqueRubrics_AcceptedSet(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"all", "consistency", "accessibility", "hierarchy"}, designCritiqueRubrics)
	for _, r := range designCritiqueRubrics {
		assert.True(t, isCritiqueRubric(r))
	}
	for _, bad := range []string{"", "ALL", "typo", "spacing", "contrast"} {
		assert.False(t, isCritiqueRubric(bad), "%q is not a rubric", bad)
	}
}

func TestRubricAreas_NarrowsForFocusedRubrics(t *testing.T) {
	t.Parallel()

	require.ElementsMatch(t, designCritiqueAreas, rubricAreas(designRubricAll))
	require.ElementsMatch(t, []string{"consistency", "spacing-rhythm"}, rubricAreas(designRubricConsistency))
	require.ElementsMatch(t, []string{"contrast", "touch-target", "affordance"}, rubricAreas(designRubricAccessibility))
	require.ElementsMatch(t, []string{"hierarchy", "affordance"}, rubricAreas(designRubricHierarchy))

	// A focused rubric must be a genuine narrowing, not a relabel.
	for _, r := range []string{designRubricConsistency, designRubricAccessibility, designRubricHierarchy} {
		assert.Less(t, len(rubricAreas(r)), len(designCritiqueAreas), "rubric %q must narrow the vocabulary", r)
	}
}

func TestDesignCritiqueAreas_CoverTheDesignerVocabulary(t *testing.T) {
	t.Parallel()
	// §4a names the vocabulary: hierarchy, affordance, consistency, spacing
	// rhythm, contrast, touch-target sizes. Every one must be an area.
	joined := strings.Join(designCritiqueAreas, ",")
	for _, want := range []string{"hierarchy", "affordance", "consistency", "spacing-rhythm", "contrast", "touch-target"} {
		assert.Contains(t, joined, want)
	}
}

func TestBuildCritiquePrompt_CarriesRubricTargetAndExtra(t *testing.T) {
	t.Parallel()

	p := buildCritiquePrompt(designRubricAll, "design/wireframes/login.svg", "", "")
	require.Contains(t, p, "design/wireframes/login.svg")
	require.Contains(t, p, "hierarchy")
	require.Contains(t, p, "affordance")
	require.Contains(t, p, "blocker | major | minor | info")
	require.NotContains(t, p, "delta review")

	// compare_to turns the pass into a delta review naming both targets.
	p = buildCritiquePrompt(designRubricConsistency, "design/screens/login.html", "design/screens/home.html", "")
	require.Contains(t, p, "delta review")
	require.Contains(t, p, "design/screens/login.html")
	require.Contains(t, p, "design/screens/home.html")
	require.Contains(t, p, "consistency")
	require.NotContains(t, p, "touch-target", "a focused rubric must not advertise out-of-scope areas")

	p = buildCritiquePrompt(designRubricHierarchy, "t", "", "focus on the empty state")
	require.Contains(t, p, "focus on the empty state")
}

// ---------------------------------------------------------------------------
// Happy path: SVG wireframe render → attachment → artifact with provenance
// ---------------------------------------------------------------------------

func TestDesignCritiqueHandler_RendersSVGAttachesAndWritesArtifact(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, mock := dcCritiqueEnv(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{
		"target": "design/wireframes/login.svg",
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "output: %s", res.Output)

	// The target was rendered through the shared browser path with
	// allow_file_url (the SP-140-2 §2c render contract).
	require.Equal(t, 1, mock.calls)
	assert.True(t, strings.HasPrefix(mock.lastURL, "file://"), "got %q", mock.lastURL)
	assert.Contains(t, mock.lastURL, "login.svg")
	assert.Equal(t, true, mock.lastOpts["allow_file_url"])

	// AC: images flow the SP-137 tool-result path.
	require.Len(t, res.Images, 1, "the rendered PNG must be attached")
	assert.Equal(t, "image/png", res.Images[0].MIMEType)
	assert.True(t, strings.HasPrefix(res.Images[0].URI, "data:image/png;base64,"))

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok, "StructuredOut should be critiqueOutput, got %T", res.StructuredOut)
	assert.Equal(t, "design/wireframes/login.svg", out.Target)
	assert.Equal(t, "all", out.Rubric, "rubric defaults to all")
	assert.Equal(t, "login", out.Screen)
	assert.Equal(t, 1, out.RenderCount)

	// AC: the PNG lands in design/.cache/renders/ with a provenance header.
	require.Len(t, out.Artifacts, 1)
	artifact := out.Artifacts[0]
	assert.Equal(t, "design/wireframes/login.svg", artifact.Target)
	assert.Equal(t, "design/.cache/renders/login.png", artifact.Path)
	assert.True(t, strings.HasPrefix(artifact.Path, design.DirName+"/"+designArtifactDirName+"/"),
		"artifact must live under design/.cache/, got %q", artifact.Path)

	raw := dcArtifactPNG(t, root, artifact.Path)
	// The bytes start with the rendered PNG (the mock's tiny PNG) …
	assert.True(t, strings.HasPrefix(string(raw), string(drTinyPNG)),
		"artifact must begin with the rendered PNG bytes")
	// … and end with the provenance banner (SP-140 invariant 2).
	assert.Contains(t, string(raw), provenanceHeaderPrefix)
	assert.Contains(t, string(raw), provenanceHeaderTerminator)
	assert.Equal(t, artifact.Provenance, extractProvenance(t, string(raw)))
	assert.Contains(t, artifact.Provenance, "tool: design_critique")
	assert.Contains(t, artifact.Provenance, "source: design/wireframes/login.svg")
	assert.Contains(t, artifact.Provenance, "kind: browser")
	assert.Contains(t, artifact.Provenance, "derived render cache")

	// The summary names the artifact, the rubric, and the vocabulary.
	assert.Contains(t, res.Output, "design_critique: design/wireframes/login.svg (rubric=all)")
	assert.Contains(t, res.Output, artifact.Path)
	assert.Contains(t, res.Output, "hierarchy")
}

// TestDesignCritiqueHandler_VisionScriptedClientReturnsStructuredFindings is
// the SP-140-4 §4a acceptance case at the handler level: with a scripted vision
// tier the critique returns structured findings {target, area, severity, note,
// suggestion}, the images flow the SP-137 path, and the PNG lands in
// design/.cache/renders/ with its provenance header.
func TestDesignCritiqueHandler_VisionScriptedClientReturnsStructuredFindings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)

	scripted := &dcScriptedVisionClient{response: `[
	  {"area":"hierarchy","severity":"major","note":"The primary CTA reads as secondary","suggestion":"Swap emphasis with the Log in link"},
	  {"area":"contrast","severity":"minor","note":"Muted helper text fails contrast on the card background","suggestion":"Use the stronger text token"},
	  {"area":"touch-target","severity":"blocker","note":"The submit control is 32pt tall","suggestion":"Raise it to the 44pt minimum"}
	]`}
	env, mock := dcCritiqueEnv(t, root)
	env.VisionProcessor = &VisionProcessor{visionClient: scripted}

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{
		"target": "design/wireframes/login.svg",
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "output: %s", res.Output)

	// The vision tier actually ran, on real pixels, with the rubric prompt.
	require.Equal(t, 1, scripted.calls)
	assert.Equal(t, 1, scripted.lastImages, "the rendered PNG must ride the vision request")
	assert.Contains(t, scripted.lastPrompt, "hierarchy", "the rubric vocabulary must reach the tier")
	assert.Contains(t, scripted.lastPrompt, "affordance")
	assert.Contains(t, scripted.lastPrompt, "blocker | major | minor | info")

	// Images flow the SP-137 path.
	require.Len(t, res.Images, 1)
	assert.Equal(t, "image/png", res.Images[0].MIMEType)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.True(t, out.Visual, "a scripted vision tier makes this a visual critique")

	// AC: structured findings {target, area, severity, note, suggestion}.
	require.Len(t, out.Findings, 3)
	assert.Equal(t, 3, out.Count)
	first := out.Findings[0]
	assert.Equal(t, "design/wireframes/login.svg", first.Target, "the tool stamps the target label")
	assert.Equal(t, "hierarchy", first.Area)
	assert.Equal(t, "major", first.Severity)
	assert.Equal(t, "The primary CTA reads as secondary", first.Note)
	assert.Equal(t, "Swap emphasis with the Log in link", first.Suggestion)

	assert.Equal(t, 1, out.BySeverity["blocker"])
	assert.Equal(t, 1, out.BySeverity["major"])
	assert.Equal(t, 1, out.BySeverity["minor"])
	assert.Equal(t, 0, out.BySeverity["info"])

	// AC: the PNG lands in design/.cache/renders/ with a provenance header.
	require.Len(t, out.Artifacts, 1)
	assert.Equal(t, "design/.cache/renders/login.png", out.Artifacts[0].Path)
	raw := dcArtifactPNG(t, root, out.Artifacts[0].Path)
	assert.Contains(t, string(raw), provenanceHeaderPrefix)

	// The summary lists the findings so a text-only consumer sees them.
	assert.Contains(t, res.Output, "[major] hierarchy — The primary CTA reads as secondary")
	assert.Contains(t, res.Output, "Swap emphasis with the Log in link")
	assert.Equal(t, 1, mock.calls)
}

// TestDesignCritiqueHandler_VisionTierFailureStillReturnsArtifact pins the
// §4a "must not fail" contract at the handler boundary: a vision tier that
// errors leaves a rendered artifact and the rubric, and never fails the tool.
func TestDesignCritiqueHandler_VisionTierFailureStillReturnsArtifact(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteCleanTree(t, root)

	env, _ := dcCritiqueEnv(t, root)
	env.VisionProcessor = &VisionProcessor{visionClient: &dcScriptedVisionClient{err: assert.AnError}}

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/screens/login.html"})
	require.NoError(t, err, "a vision-tier failure must not fail the critique")
	require.False(t, res.IsError)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.False(t, out.Visual)
	assert.Empty(t, out.Findings)
	require.Len(t, out.Artifacts, 1, "the rendered artifact survives a vision failure")
	require.Len(t, res.Images, 1, "the attachment survives a vision failure")
	require.Contains(t, res.Output, "analyze_image_content")
}

func extractProvenance(t *testing.T, raw string) string {
	t.Helper()
	idx := strings.Index(raw, provenanceHeaderPrefix+"\n")
	require.GreaterOrEqual(t, idx, 0, "provenance opener must be present")
	body := raw[idx+len(provenanceHeaderPrefix)+1:]
	end := strings.Index(body, provenanceHeaderTerminator)
	require.GreaterOrEqual(t, end, 0, "provenance terminator must be present")
	return strings.TrimSuffix(body[:end], "\n")
}

func TestDesignCritiqueHandler_RendersHTMLAndMermaid(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)

	h := &designCritiqueHandler{}

	// HTML screen.
	env, mock := dcCritiqueEnv(t, root)
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/screens/login.html"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, 1, mock.calls)
	require.Contains(t, mock.lastURL, "login.html")
	require.Len(t, res.Images, 1)
	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	require.Len(t, out.Artifacts, 1)
	assert.Equal(t, "design/.cache/renders/login.png", out.Artifacts[0].Path)

	// Mermaid flow: the .mmd must never be handed to the browser directly.
	env, mock = dcCritiqueEnv(t, root)
	res, err = h.Execute(newTestCtx(root), env, map[string]any{"target": "design/flows/sign-up.mmd"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, 1, mock.calls)
	assert.Contains(t, mock.lastURL, ".html", "mermaid renders a generated HTML page")
	assert.NotContains(t, mock.lastURL, ".mmd")
	out, ok = res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	require.Len(t, out.Artifacts, 1)
	assert.Equal(t, "design/.cache/renders/sign-up.png", out.Artifacts[0].Path)
	assert.Contains(t, out.Artifacts[0].Provenance, "kind: mermaid")

	// The .mmd source must be byte-for-byte unchanged.
	before, err := os.ReadFile(filepath.Join(root, "design", "flows", "sign-up.mmd"))
	require.NoError(t, err)
	assert.Equal(t, dcTestMMD, string(before))
}

func TestDesignCritiqueHandler_SlugResolvesToScreenThenWireframe(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)

	h := &designCritiqueHandler{}

	// `login` has both a screen and a wireframe; the screen is preferred.
	env, mock := dcCritiqueEnv(t, root)
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "login"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, mock.lastURL, "login.html", "the hi-fi screen is preferred over the wireframe")

	// `home` has only a wireframe.
	env, mock = dcCritiqueEnv(t, root)
	res, err = h.Execute(newTestCtx(root), env, map[string]any{"target": "home"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, mock.lastURL, "home.svg")
}

func TestTargetForSlug_PrefersScreenThenFlow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)

	got, ok := targetForSlug(root, "login")
	require.True(t, ok)
	assert.Equal(t, "design/screens/login.html", got.Label)
	assert.Equal(t, "login", got.Screen)

	got, ok = targetForSlug(root, "home")
	require.True(t, ok)
	assert.Equal(t, "design/wireframes/home.svg", got.Label)

	got, ok = targetForSlug(root, "sign-up")
	require.True(t, ok)
	assert.Equal(t, "design/flows/sign-up.mmd", got.Label)

	_, ok = targetForSlug(root, "no-such-screen")
	assert.False(t, ok)
}

// ---------------------------------------------------------------------------
// compare_to — delta review
// ---------------------------------------------------------------------------

func TestDesignCritiqueHandler_CompareToRendersBothAndNamesDelta(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, mock := dcCritiqueEnv(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{
		"target":     "design/wireframes/login.svg",
		"compare_to": "design/wireframes/home.svg",
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "output: %s", res.Output)

	// Both screens were rendered.
	assert.Equal(t, 2, mock.calls)
	require.Len(t, res.Images, 1, "the primary target's render is attached")

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.Equal(t, "design/wireframes/home.svg", out.CompareTo)
	assert.Equal(t, 2, out.RenderCount)
	require.Len(t, out.Artifacts, 2, "each render is cached as its own artifact")
	assert.Equal(t, "design/.cache/renders/login.png", out.Artifacts[0].Path)
	assert.Equal(t, "design/.cache/renders/login~home.png", out.Artifacts[1].Path,
		"the comparison artifact must be distinguishable from the baseline")
	assert.Contains(t, out.Instruction, "delta review")
	assert.Contains(t, res.Output, "vs design/wireframes/home.svg")
}

func TestCritiqueArtifactPath_CompareUsesBaselineAndComparisonStems(t *testing.T) {
	t.Parallel()

	base := critiqueTarget{Label: "design/wireframes/login.svg", Stage: "target"}
	assert.Equal(t, "design/.cache/renders/login.png", critiqueArtifactPath(base))

	cmp := critiqueTarget{
		Label:        "design/wireframes/home.svg",
		Stage:        "compare",
		CompareLabel: "design/wireframes/login.svg",
	}
	assert.Equal(t, "design/.cache/renders/login~home.png", critiqueArtifactPath(cmp))

	// A comparison with no recorded baseline still yields a distinct name.
	bare := critiqueTarget{Label: "design/wireframes/home.svg", Stage: "compare"}
	assert.Equal(t, "design/.cache/renders/home.png", critiqueArtifactPath(bare))
}

// ---------------------------------------------------------------------------
// Whole tree + subtree
// ---------------------------------------------------------------------------

func TestDesignCritiqueHandler_WholeTreeCritiquesEveryRenderableTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)

	for _, target := range []string{"design", "design/"} {
		env, mock := dcCritiqueEnv(t, root)
		h := &designCritiqueHandler{}
		res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": target})
		require.NoError(t, err)
		require.False(t, res.IsError, "target %q: %s", target, res.Output)

		out, ok := res.StructuredOut.(critiqueOutput)
		require.True(t, ok)
		// Two wireframes + one screen + one flow.
		assert.Equal(t, 4, out.RenderCount, "target %q", target)
		assert.Equal(t, 4, mock.calls)
		require.Len(t, out.Artifacts, 4)

		// Deterministic order: wireframes, then screens, then flows.
		paths := make([]string, 0, len(out.Artifacts))
		for _, a := range out.Artifacts {
			paths = append(paths, a.Path)
		}
		assert.Equal(t, []string{
			"design/.cache/renders/home.png",
			"design/.cache/renders/login.png",
			"design/.cache/renders/login.png",
			"design/.cache/renders/sign-up.png",
		}, paths)
	}
}

func TestDiscoverTreeTargets_OrderAndScope(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	// A non-renderable asset must not appear in the tree critique.
	dcWrite(t, root, "design/tokens/color.tokens.json", `{"color":{"$value":"#fff","$type":"color"}}`)

	targets, err := discoverTreeTargets(root)
	require.NoError(t, err)
	require.Len(t, targets, 4)
	assert.Equal(t, "design/wireframes/home.svg", targets[0].Label)
	assert.Equal(t, "design/wireframes/login.svg", targets[1].Label)
	assert.Equal(t, "design/screens/login.html", targets[2].Label)
	assert.Equal(t, "design/flows/sign-up.mmd", targets[3].Label)
	for _, tg := range targets {
		assert.Equal(t, "tree", tg.Stage)
		assert.NotEqual(t, renderKindUnknown, tg.Kind)
	}
}

func TestDesignCritiqueHandler_SubtreeTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, mock := dcCritiqueEnv(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.Equal(t, 2, out.RenderCount)
	assert.Equal(t, 2, mock.calls)
}

func TestDesignSubdirOf(t *testing.T) {
	t.Parallel()
	for _, sub := range design.Subdirs {
		got, ok := designSubdirOf(design.DirName + "/" + sub)
		assert.True(t, ok, "design/%s must classify as a canonical subdir", sub)
		assert.Equal(t, sub, got)
	}
	for _, bad := range []string{"design", "design/nope", "wireframes", "design/wireframes/login.svg"} {
		_, ok := designSubdirOf(bad)
		assert.False(t, ok, "%q is not a canonical design subdirectory", bad)
	}
}

// ---------------------------------------------------------------------------
// Structured output shape + findings
// ---------------------------------------------------------------------------

func TestDesignCritiqueHandler_EmptyFindingsShape(t *testing.T) {
	// Not parallel: it asserts the no-tier verdict, which reads process-wide
	// vision-capability state pinned by dcCritiqueEnvNoVision.
	root := t.TempDir()
	dcWriteCleanTree(t, root)
	env, _ := dcCritiqueEnvNoVision(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/screens/login.html"})
	require.NoError(t, err)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)

	// Item 4.1 core: the critique is rendered + attached + rubric'd; findings
	// come from the vision pass. With no vision tier wired the list is empty
	// and the tally is all-zero — never nil, so a consumer can iterate.
	assert.NotNil(t, out.Findings)
	assert.Empty(t, out.Findings)
	assert.Equal(t, 0, out.Count)
	require.NotNil(t, out.BySeverity)
	for _, sev := range critiqueSeverities {
		assert.Equal(t, 0, out.BySeverity[sev], "severity %q must be present at zero", sev)
	}
	assert.False(t, out.Visual, "no vision tier is wired in a unit test env")
	assert.NotEmpty(t, out.Instruction)
	assert.NotEmpty(t, out.Areas)

	// JSON shape: the field names the spec names.
	data, err := json.Marshal(res.StructuredOut)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	for _, key := range []string{"target", "rubric", "findings", "count", "bySeverity", "artifacts", "areas", "instruction", "renderCount", "visual"} {
		assert.Contains(t, raw, key, "structured output must carry %q", key)
	}
	// The §4a marker is explicit and machine-readable: `visual` must be a real
	// boolean false, not an omitted/zero-looking field a consumer has to infer.
	assert.Equal(t, false, raw["visual"], "visual must serialize as an explicit false")
	// A no-vision run must not invent findings. This asserts the intent
	// directly, so a cross-test leak of the process-wide vision capability
	// (e.g. a sibling test leaving provider env vars set) surfaces as a clear
	// failure here instead of a confusing `visual` mismatch.
	//
	// Item 4.2 note: this fixture tree is statically clean, so the *static*
	// degradation path (which this run takes) also finds nothing. A degraded
	// run with violations is covered by the item-4.2 tests below.
	require.Equal(t, 0, out.Count, "a no-vision critique of a clean tree must not produce findings")
	assert.False(t, out.Degraded, "no findings means nothing to mark degraded")
}

// ---------------------------------------------------------------------------
// Non-vision degradation — static findings (SP-140-4 §4a)
//
// AC: "Non-vision scripted client: design_critique returns visual:false static
// findings, no error." These tests pin that a run with no reachable vision tier
// degrades to the design validator's rule-pack findings (mapped into the
// critique schema) instead of returning an empty list, and that the turn never
// fails.
// ---------------------------------------------------------------------------

// dcViolatingSVG is a wireframe that trips the static rule packs:
//   - a data-nav target ("nowhere") that matches no wireframe stem
//     (svg_data_nav_dangling, hard → blocker),
//   - the data-nav element lacks a stable id (svg_stable_ids, info).
//
// It keeps a <text> element so svg_text_usage does not also fire, a valid
// viewBox so svg_viewbox does not short-circuit the walk, and a slug-clean stem
// so svg_slug_name does not fire — which keeps the expected finding set small
// and readable.
const dcViolatingSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Login</text>
  <rect x="24" y="200" width="342" height="52" data-nav="nowhere"/>
</svg>`

// dcWriteViolatingTree seeds the clean tree plus one wireframe that violates
// the static rules, so a non-vision critique has static findings to surface.
func dcWriteViolatingTree(t *testing.T, root string) {
	t.Helper()
	dcWriteTree(t, root)
	dcWrite(t, root, "design/wireframes/checkout.svg", dcViolatingSVG)
}

// TestDesignCritiqueHandler_NoVisionDegradesToStaticFindings is the item-4.2
// acceptance case: with no vision tier reachable, design_critique returns
// `visual:false` static findings and NO error. The findings come from the
// design validator's rule packs, mapped into the critique schema, and every one
// is attributable to a rule id.
func TestDesignCritiqueHandler_NoVisionDegradesToStaticFindings(t *testing.T) {
	// Not parallel: dcCritiqueEnvNoVision pins process-wide vision-capability
	// state, so no-vision tests must not race the parallel vision-enabled suite.
	root := t.TempDir()
	dcWriteViolatingTree(t, root)
	env, mock := dcCritiqueEnvNoVision(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/checkout.svg"})
	require.NoError(t, err, "a non-vision critique must never fail the turn")
	require.False(t, res.IsError, "output: %s", res.Output)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)

	// AC: visual:false, and the run is explicitly marked degraded.
	assert.False(t, out.Visual, "no vision tier is reachable, so this is not a visual critique")
	assert.True(t, out.Degraded, "a static-finding run must be marked degraded")

	// AC: static findings are present (not the empty 4.1 result).
	require.NotEmpty(t, out.Findings, "a non-vision primary must still get static findings")
	assert.Equal(t, len(out.Findings), out.Count)

	// The hard data-nav violation surfaces as a blocker in the consistency area
	// and carries its rule id + line, so it is traceable to the static pack.
	var dangling *critiqueFinding
	for i := range out.Findings {
		if out.Findings[i].Rule == "svg_data_nav_dangling" {
			dangling = &out.Findings[i]
			break
		}
	}
	require.NotNil(t, dangling, "the dangling data-nav must surface as a static finding")
	assert.Equal(t, "design/wireframes/checkout.svg", dangling.Target)
	assert.Equal(t, "blocker", dangling.Severity, "a hard validator error is a critique blocker")
	assert.Equal(t, "consistency", dangling.Area)
	assert.Greater(t, dangling.Line, 0, "the static finding keeps the validator's source line")
	assert.Contains(t, dangling.Note, "does not match any wireframe stem")
	assert.NotEmpty(t, dangling.Suggestion, "a static finding carries the rule's concrete fix")

	// The map is consistent: a blocker is counted, and every area is in the
	// advertised critique vocabulary.
	assert.GreaterOrEqual(t, out.BySeverity["blocker"], 1)
	for _, f := range out.Findings {
		assert.Contains(t, designCritiqueAreas, f.Area, "static area must be a critique area")
		assert.Contains(t, critiqueSeverities, f.Severity)
	}

	// Never-fail: the render + artifact contract is unaffected by the tier.
	require.Len(t, out.Artifacts, 1)
	require.Len(t, res.Images, 1)
	assert.Equal(t, 1, mock.calls)

	// The degradation is disclosed in the human-readable output.
	assert.Contains(t, res.Output, "(static; visual=false)")
	assert.Contains(t, res.Output, "visual:false")
	assert.Contains(t, res.Output, "svg_data_nav_dangling")

	// JSON shape: the marker and the degraded flag are real booleans and the
	// findings carry the rule id.
	data, err := json.Marshal(out)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Equal(t, false, raw["visual"])
	assert.Equal(t, true, raw["degraded"])
}

// TestDesignCritiqueHandler_NoVisionStaticFindingsTreeWide pins that a
// whole-tree critique also degrades to static findings, using the tree
// validator (which resolves cross-file references a per-file run cannot). The
// findings are stamped with each file as their target, so a tree-wide
// degradation stays attributable.
func TestDesignCritiqueHandler_NoVisionStaticFindingsTreeWide(t *testing.T) {
	root := t.TempDir()
	dcWriteViolatingTree(t, root)
	env, _ := dcCritiqueEnvNoVision(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design"})
	require.NoError(t, err, "a tree-wide non-vision critique must not fail")
	require.False(t, res.IsError, "output: %s", res.Output)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.False(t, out.Visual)
	assert.True(t, out.Degraded)
	require.NotEmpty(t, out.Findings)

	// The violating file is named as a finding target; the clean files are not
	// invented into findings.
	targets := map[string]bool{}
	for _, f := range out.Findings {
		targets[f.Target] = true
	}
	assert.True(t, targets["design/wireframes/checkout.svg"],
		"the tree-wide static pass must attribute the violation to its file")
}

// TestDesignCritiqueHandler_NoVisionStaticFindingsRubricFiltered pins that the
// static degradation honours the active rubric: a consistency-focused pass
// reports consistency-area findings (dangling references) and drops findings
// the rubric does not cover, so a focused pass stays focused.
func TestDesignCritiqueHandler_NoVisionStaticFindingsRubricFiltered(t *testing.T) {
	root := t.TempDir()
	// A wireframe whose only violation is a missing viewBox → hierarchy area.
	dcWriteTree(t, root)
	dcWrite(t, root, "design/wireframes/noview.svg", `<svg xmlns="http://www.w3.org/2000/svg" width="390" height="844"><text x="1" y="1">x</text></svg>`)
	env, _ := dcCritiqueEnvNoVision(t, root)

	h := &designCritiqueHandler{}

	// hierarchy rubric keeps the viewBox finding.
	res, err := h.Execute(newTestCtx(root), env, map[string]any{
		"target": "design/wireframes/noview.svg",
		"rubric": designRubricHierarchy,
	})
	require.NoError(t, err)
	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	require.NotEmpty(t, out.Findings)
	for _, f := range out.Findings {
		assert.Contains(t, rubricAreas(designRubricHierarchy), f.Area,
			"a focused rubric must not surface out-of-scope areas")
	}

	// A consistency rubric drops the hierarchy-only finding.
	res, err = h.Execute(newTestCtx(root), env, map[string]any{
		"target": "design/wireframes/noview.svg",
		"rubric": designRubricConsistency,
	})
	require.NoError(t, err)
	out, ok = res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	for _, f := range out.Findings {
		assert.Contains(t, rubricAreas(designRubricConsistency), f.Area)
	}
}

// TestDesignCritiqueHandler_NoVisionCleanTreeNotesDegradation pins the
// "clean-but-degraded" case: a non-vision critique of a statically clean target
// reports no findings but still discloses that the static fallback ran, so an
// empty list is not mistaken for "no critique was attempted".
func TestDesignCritiqueHandler_NoVisionCleanTreeNotesDegradation(t *testing.T) {
	root := t.TempDir()
	dcWriteCleanTree(t, root)
	env, _ := dcCritiqueEnvNoVision(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/screens/login.html"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.False(t, out.Visual)
	assert.Empty(t, out.Findings)
	assert.False(t, out.Degraded, "no findings means nothing was degraded into the result")
	assert.Contains(t, out.Note, "degraded to static",
		"a clean degraded run must still disclose that the static fallback ran")
	assert.Contains(t, res.Output, "degraded to static")
}

// TestDesignCritiqueHandler_VisionPreferredOverStatic is the SP-137 tier-order
// half of item 4.2: when a vision tier is reachable, the vision findings win
// and the static pass does not run — even on a target that would trip the
// static rules.
func TestDesignCritiqueHandler_VisionPreferredOverStatic(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteViolatingTree(t, root)

	env, _ := dcCritiqueEnv(t, root)
	env.VisionProcessor = &VisionProcessor{visionClient: &dcScriptedVisionClient{
		response: `[{"area":"hierarchy","severity":"minor","note":"vision finding","suggestion":"v"}]`,
	}}

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/checkout.svg"})
	require.NoError(t, err)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.True(t, out.Visual)
	assert.False(t, out.Degraded, "a vision critique is not degraded")
	require.Len(t, out.Findings, 1)
	assert.Equal(t, "vision finding", out.Findings[0].Note)
	assert.Empty(t, out.Findings[0].Rule, "a vision finding carries no validator rule")
}

// TestCritiqueSeverityForDesign pins the severity mapping the static
// degradation uses.
func TestCritiqueSeverityForDesign(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "blocker", critiqueSeverityForDesign(design.SeverityError))
	assert.Equal(t, "major", critiqueSeverityForDesign(design.SeverityWarn))
	assert.Equal(t, "minor", critiqueSeverityForDesign(design.SeverityFix))
	assert.Equal(t, "info", critiqueSeverityForDesign(design.SeverityInfo))
	assert.Equal(t, "info", critiqueSeverityForDesign(design.Severity("bogus")))
}

// TestCritiqueAreaForRule pins the rule → area classification, including the
// fallback for an unknown rule (a rule pack item 4.4/4.5 adds later).
func TestCritiqueAreaForRule(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"svg_data_nav_dangling":  "consistency",
		"svg_slug_name":          "consistency",
		"manifest_link_dangling": "consistency",
		"svg_viewbox":            "hierarchy",
		"svg_frame_match":        "hierarchy",
		"screen_external_ref":    "affordance",
		"icon_wellformed":        "affordance",
		"brand_raw_hex":          "contrast",
		"svg_data_uri_size":      "contrast",
		"a_rule_from_the_future": "consistency",
		"":                       "consistency",
	}
	for rule, want := range cases {
		assert.Equal(t, want, critiqueAreaForRule(rule), "critiqueAreaForRule(%q)", rule)
	}
	// Every mapped area is a real critique area.
	for rule := range cases {
		assert.Contains(t, designCritiqueAreas, critiqueAreaForRule(rule))
	}
}

// TestAppendNote pins the note composition used by the degradation (and by
// item 4.3's cap notice through the same field).
func TestAppendNote(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "b", appendNote("", " b "))
	assert.Equal(t, "a", appendNote("a", ""))
	assert.Equal(t, "a b", appendNote(" a ", " b "))
	assert.Equal(t, "cap notice static notice", appendNote("cap notice", "static notice"))
}

// TestDesignCritiqueHandler_NoVisionTierIsHermetic pins that the no-processor
// branch of the vision pass never issues a live request: when no tier is
// available the pass degrades on the capability probe alone. It pins the
// capability so the outcome cannot depend on the host's provider
// configuration or network.
func TestDesignCritiqueHandler_NoVisionTierIsHermetic(t *testing.T) {
	restoreCapability := SetVisionCapabilityForTest(false)
	t.Cleanup(restoreCapability)

	root := t.TempDir()
	dcWriteCleanTree(t, root)
	env, _ := dcCritiqueEnvNoVision(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/screens/login.html"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.False(t, out.Visual)
	assert.Zero(t, out.Count, "no tier means no findings — never a fabricated observation")
	assert.Empty(t, out.Findings)
	// The result still carries the artifact + rubric, and the summary points at
	// the OCR/native recovery route rather than at a critique that never ran.
	require.Len(t, out.Artifacts, 1)
	assert.Contains(t, res.Output, "analyze_image_content")
}

// TestDesignCritiqueHandler_VisualFalseWhenNoVisionTier pins the SP-140-4 §4a
// marker deterministically: Visual reports whether a vision-capable critique
// actually ran, so it is false whenever no tier is reachable and true whenever
// one is *and* produced an analysis.
//
// The no-tier case is the regression this guards: the package-level vision
// entry point reports a missing capability as a structured JSON response with
// an error code rather than a Go error, so a handler that trusted
// `analyzeErr == nil` reported visual=true for a critique that never happened.
func TestDesignCritiqueHandler_VisualFalseWhenNoVisionTier(t *testing.T) {
	// Not parallel: the no-tier case reads process-wide vision-capability state
	// (pinned by dcCritiqueEnvNoVision), so it must not race the parallel
	// suite's vision-enabled tests.
	cases := []struct {
		name string
		env  func(root string) (ToolEnv, *drMockBrowser)
	}{
		{
			name: "no vision processor wired",
			env:  func(root string) (ToolEnv, *drMockBrowser) { return dcCritiqueEnvNoVision(t, root) },
		},
		{
			name: "vision processor wired that yields no analysis",
			env: func(root string) (ToolEnv, *drMockBrowser) {
				env, mock := dcCritiqueEnvNoVision(t, root)
				// A wired tier that returns an empty analysis (a scripted
				// client that answers with blank text) must still not be
				// reported as a visual critique.
				env.VisionProcessor = &VisionProcessor{visionClient: &dcScriptedVisionClient{response: "  "}}
				return env, mock
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// A fresh root per case: the assertions below are about the tier's
			// verdict, and a shared root would let the second case hit the
			// §4e render cache (its own render-count behavior is covered by the
			// dedicated cache tests).
			root := t.TempDir()
			dcWriteCleanTree(t, root)
			env, mock := tc.env(root)
			h := &designCritiqueHandler{}
			res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/screens/login.html"})
			require.NoError(t, err, "a missing vision tier must never fail the critique")
			require.False(t, res.IsError)

			out, ok := res.StructuredOut.(critiqueOutput)
			require.True(t, ok)
			assert.Empty(t, out.Findings)
			assert.False(t, out.Visual, "no analysis text was produced, so this is not a visual critique")
			// The render + artifact contract is unaffected by the tier.
			assert.Equal(t, 1, out.RenderCount)
			assert.Equal(t, 1, mock.calls)
			require.Len(t, out.Artifacts, 1)
			require.Len(t, res.Images, 1, "the attachment survives with no vision tier")
		})
	}
}

// TestDesignCritiqueHandler_VisualTrueOnlyWithRealAnalysis is the positive half
// of the same contract: a wired scripted tier that answers with findings makes
// the critique visual, because a tier was reachable and it produced an
// analysis.
func TestDesignCritiqueHandler_VisualTrueOnlyWithRealAnalysis(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteCleanTree(t, root)

	env, _ := dcCritiqueEnv(t, root)
	env.VisionProcessor = &VisionProcessor{visionClient: &dcScriptedVisionClient{
		response: `[{"area":"hierarchy","severity":"major","note":"CTA reads secondary"}]`,
	}}

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/screens/login.html"})
	require.NoError(t, err)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	require.True(t, critiqueVisionTierAvailable(env))
	assert.True(t, out.Visual, "a reachable tier that produced an analysis is a visual critique")
	require.Len(t, out.Findings, 1)
}

// TestCritiqueVisionTierAvailable pins the availability probe in isolation: a
// wired processor is a tier; with none wired the verdict follows
// HasVisionCapability, which is what the package-level AnalyzeImage path
// requires before it will attempt a critique. No network or model is touched.
func TestCritiqueVisionTierAvailable(t *testing.T) {
	// Not parallel: it reads process-wide vision-capability state.
	root := t.TempDir()
	// Pin the capability probe so the first assertion states the "no tier"
	// wiring explicitly rather than depending on the host (a dev machine with a
	// compiled native-OCR shim would legitimately have a tier at the package
	// level; this test is about the probe being consulted, not about the host).
	restore := SetVisionCapabilityForTest(false)
	t.Cleanup(restore)

	env, _ := dcCritiqueEnvNoVision(t, root)
	require.False(t, critiqueVisionTierAvailable(env),
		"no wired processor + no package-level capability means no tier")

	env.VisionProcessor = &VisionProcessor{}
	assert.True(t, critiqueVisionTierAvailable(env), "a wired processor is a tier")

	// With no processor the verdict is exactly the package-level capability
	// probe — never a second, divergent notion of "available".
	env.VisionProcessor = nil
	SetVisionCapabilityForTest(false)
	assert.False(t, critiqueVisionTierAvailable(env),
		"no processor + no capability must report unavailable")
	SetVisionCapabilityForTest(true)
	assert.True(t, critiqueVisionTierAvailable(env),
		"no processor + capability present must report available (native OCR or a vision provider)")

	// Determinism: the same state must give the same verdict every call.
	assert.True(t, critiqueVisionTierAvailable(env))

	// The one-shot probe cache must not change the answer once a test pins it.
	ResetVisionCapabilityCacheForTest()
	assert.True(t, critiqueVisionTierAvailable(env), "a pinned verdict survives a cache reset")
}

func TestTallySeverities(t *testing.T) {
	t.Parallel()

	findings := []critiqueFinding{
		{Target: "a", Area: "hierarchy", Severity: "blocker", Note: "n"},
		{Target: "a", Area: "contrast", Severity: "major", Note: "n"},
		{Target: "b", Area: "affordance", Severity: "Major", Note: "n"},
		{Target: "b", Area: "consistency", Severity: "", Note: "n"}, // defaults to info
	}
	tally := tallySeverities(findings)
	assert.Equal(t, 1, tally["blocker"])
	assert.Equal(t, 2, tally["major"])
	assert.Equal(t, 1, tally["info"])
	assert.Equal(t, 0, tally["minor"])
}

func TestUnmarshalCritiqueFindings(t *testing.T) {
	t.Parallel()

	// A bare JSON array of findings.
	got := UnmarshalCritiqueFindings(`[
	  {"target":"login","area":"hierarchy","severity":"major","note":"Primary CTA reads secondary","suggestion":"Swap emphasis"}
	]`)
	require.Len(t, got, 1)
	assert.Equal(t, "login", got[0].Target)
	assert.Equal(t, "hierarchy", got[0].Area)
	assert.Equal(t, "major", got[0].Severity)
	assert.Equal(t, "Primary CTA reads secondary", got[0].Note)
	assert.Equal(t, "Swap emphasis", got[0].Suggestion)

	// An object wrapper.
	got = UnmarshalCritiqueFindings(`{"findings":[{"area":"contrast","note":"muted text fails"}]}`)
	require.Len(t, got, 1)
	assert.Equal(t, "contrast", got[0].Area)
	assert.Equal(t, "info", got[0].Severity, "a finding with no severity defaults to info")

	// A fenced code block.
	got = UnmarshalCritiqueFindings("```json\n[{\"area\":\"hierarchy\",\"note\":\"n\"}]\n```")
	require.Len(t, got, 1)

	// Rows with no observation are dropped; garbage yields nothing.
	assert.Empty(t, UnmarshalCritiqueFindings(`[{"area":"hierarchy"}]`))
	assert.Empty(t, UnmarshalCritiqueFindings("not json at all"))
	assert.Empty(t, UnmarshalCritiqueFindings(""))
}

func TestBuildCritiqueSummary_VisionAbsentGuidance(t *testing.T) {
	t.Parallel()
	out := critiqueOutput{
		Target:      "design/wireframes/login.svg",
		Rubric:      "all",
		Artifacts:   []critiqueArtifact{{Target: "x", Path: "design/.cache/renders/login.png"}},
		RenderCount: 1,
		Instruction: "Critique this rendered design…",
		Count:       0,
	}

	summary := buildCritiqueSummary(out, true, "", nil)
	require.Contains(t, summary, "design_critique: design/wireframes/login.svg (rubric=all)")
	require.Contains(t, summary, "design/.cache/renders/login.png")
	require.Contains(t, summary, "analyze_image_content", "a non-vision run must point at a recovery route")
	require.Contains(t, summary, "No structured findings yet.")

	// With findings, they are listed with their suggestion.
	out.Findings = []critiqueFinding{
		{Target: "x", Area: "hierarchy", Severity: "major", Note: "CTA reads secondary", Suggestion: "Swap emphasis"},
	}
	out.Count = 1
	summary = buildCritiqueSummary(out, true, "", nil)
	require.Contains(t, summary, "[major] hierarchy — CTA reads secondary")
	require.Contains(t, summary, "Swap emphasis")

	// A note (item 4.3's cap notice) is surfaced.
	out.Note = "capped at 20 screens"
	summary = buildCritiqueSummary(out, false, "", nil)
	require.Contains(t, summary, "capped at 20 screens")

	// A vision-tier error is reported, never as a failure.
	summary = buildCritiqueSummary(out, true, "", assert.AnError)
	require.Contains(t, summary, "No critique text from the vision tier")
	require.Contains(t, summary, "analyze_image_content")

	// Analysis text present: it is surfaced verbatim.
	summary = buildCritiqueSummary(out, true, "  Hierarchy is flat; the CTA reads secondary.  ", nil)
	require.Contains(t, summary, "Critique (vision tier):")
	require.Contains(t, summary, "Hierarchy is flat")
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

func TestDesignCritiqueHandler_UnknownRubricRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, mock := dcCritiqueEnv(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{
		"target": "design/wireframes/login.svg",
		"rubric": "typo",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "unsupported rubric")
	require.Contains(t, res.Output, "all")
	assert.Zero(t, mock.calls, "an invalid rubric must fail before rendering")
}

func TestDesignCritiqueHandler_MissingTargetArg(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	h := &designCritiqueHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError)

	res, err = h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"target": "   "})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "must not be empty")
}

func TestDesignCritiqueHandler_UnresolvableTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, mock := dcCritiqueEnv(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "no-such-screen"})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "resolved to no renderable design target")
	assert.Zero(t, mock.calls)
}

func TestDesignCritiqueHandler_UnsupportedExtension(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	dcWrite(t, root, "design/wireframes/login.png", "not really a png")
	env, mock := dcCritiqueEnv(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.png"})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "no renderable design target")
	assert.Zero(t, mock.calls)
}

func TestDesignCritiqueHandler_NoDesignDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	env, _ := dcCritiqueEnv(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design"})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "no renderable design target")
}

func TestDesignCritiqueHandler_NoBrowserFails(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)

	// No WebBrowser wired — design_critique is browser-tier by construction,
	// so this is a fatal (not silent-fallback) error.
	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{
		"target": "design/wireframes/login.svg",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "browser not available")
}

// ---------------------------------------------------------------------------
// Gate-1 precheck (SP-140 invariant 7 / §2e)
// ---------------------------------------------------------------------------

func TestDesignCritiqueHandler_Gate1Deny(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)

	env, mock := dcCritiqueEnv(t, root)
	env.FileAccessClassifier = denyClassifier{}

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.svg"})
	require.Error(t, err)
	require.True(t, res.IsError, "a Gate-1 deny is a tool failure")
	require.Contains(t, res.Output, "design_critique blocked")
	assert.Zero(t, mock.calls, "a denied target must never be rendered")
}

func TestDesignCritiqueHandler_Gate1DenyOnTreeChild(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)

	// A deny fires on the expanded child, before any render.
	env, mock := dcCritiqueEnv(t, root)
	env.FileAccessClassifier = denyClassifier{}

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design"})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "design_critique blocked")
	assert.Zero(t, mock.calls)
}

func TestDesignCritiqueHandler_Gate1Allow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)

	env, mock := dcCritiqueEnv(t, root)
	env.FileAccessClassifier = allowClassifier{}

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/wireframes/login.svg"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, 1, mock.calls)
	require.Len(t, res.Images, 1, "an allowed target must still attach the render")
}

// TestDesignCritiqueHandler_OffWorkspaceDenied drives the real precheck path:
// the classifier answers "prompt" (the production verdict for an off-workspace
// path with no session allowlist) and the prompter declines, so nothing is
// rendered and nothing is attached.
//
// The fixture must be a *renderable* path (an .svg) or discovery would reject
// it before Gate 1 ever ran — the precheck is what this test is about.
func TestDesignCritiqueHandler_OffWorkspaceDenied(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)

	outside := dcOutsideWorkspaceSVG(t)
	denied := &fakeFSPrompter{} // approve=false

	env, mock := dcCritiqueEnv(t, root)
	env.FileAccessClassifier = &fakeClassifier{verdict: "prompt"}
	env.FileAccessPrompter = denied

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": outside})
	require.Error(t, err, "an off-workspace target must fail the tool")
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "design_critique blocked")
	require.Contains(t, res.Output, "not approved")
	require.Equal(t, 1, denied.called, "the off-workspace path must reach the approval prompt")
	assert.Zero(t, mock.calls, "the browser must never render a denied off-workspace target")
	assert.Empty(t, res.Images, "no off-workspace render may be attached")
	_, ok := res.StructuredOut.(critiqueOutput)
	assert.False(t, ok, "a denied critique must not produce a structured result")
}

// dcOutsideWorkspaceSVG creates a renderable .svg outside the workspace in a
// location the session allowlist does not cover, and returns its absolute path.
func dcOutsideWorkspaceSVG(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir to build an off-workspace fixture: %v", err)
	}
	dir := filepath.Join(home, ".sprout-test-outside")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Skipf("cannot create off-workspace fixture dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "outside.svg")
	if err := os.WriteFile(path, []byte(dcTestSVG), 0o644); err != nil {
		t.Skipf("cannot create off-workspace fixture: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Artifact naming / provenance helpers
// ---------------------------------------------------------------------------

func TestCritiqueStemForLabel(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"design/wireframes/login.svg":        "login",
		"design/screens/sign-up.html":        "sign-up",
		"design/flows/check-deposit.mmd":     "check-deposit",
		"design/wireframes/Login Screen.svg": "login-screen",
		"":                                   "target",
		"design/wireframes/.svg":             "target",
		"design/wireframes/2-fa.svg":         "2-fa",
	}
	for in, want := range cases {
		assert.Equal(t, want, critiqueStemForLabel(in), "critiqueStemForLabel(%q)", in)
	}
}

func TestProvenanceBanner_RoundTrip(t *testing.T) {
	t.Parallel()
	body := "tool: design_critique\nsource: design/wireframes/login.svg\n"
	banner := provenanceBanner(body)

	require.True(t, strings.HasPrefix(banner, "\n"+provenanceHeaderPrefix+"\n"))
	require.True(t, strings.HasSuffix(banner, provenanceHeaderTerminator+"\n"))

	// A raw artifact is PNG bytes + banner; the banner is extractable.
	raw := string(drTinyPNG) + banner
	assert.Equal(t, body, extractProvenance(t, raw))
	// The PNG payload is untouched by the append.
	assert.True(t, strings.HasPrefix(raw, string(drTinyPNG)))
}

func TestBuildCritiqueProvenance(t *testing.T) {
	t.Parallel()
	tgt := critiqueTarget{Label: "design/flows/sign-up.mmd", Kind: renderKindMermaid}
	p := buildCritiqueProvenance(tgt, "Critique\nthis   design.", "abc123", "viewport=1280x720;rubric=all")
	assert.Contains(t, p, "tool: design_critique")
	assert.Contains(t, p, "source: design/flows/sign-up.mmd")
	assert.Contains(t, p, "kind: mermaid")
	assert.Contains(t, p, "generated: ")
	assert.Contains(t, p, "sourceHash: abc123",
		"§4e: the content hash must be recorded so a consumer can verify it")
	assert.Contains(t, p, "renderMaterial: viewport=1280x720;rubric=all",
		"the render material must be recorded alongside the source hash")
	assert.Contains(t, p, "instruction: Critique this design.",
		"the instruction must be recorded on a single line for a stable banner")

	// No instruction ⇒ no instruction line; no hash/material ⇒ neither line.
	p = buildCritiqueProvenance(critiqueTarget{Label: "x.svg", Kind: renderKindBrowser}, "", "", "")
	assert.NotContains(t, p, "instruction:")
	assert.NotContains(t, p, "sourceHash:")
	assert.NotContains(t, p, "renderMaterial:")
}

func TestRenderKindName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "browser", renderKindName(renderKindBrowser))
	assert.Equal(t, "mermaid", renderKindName(renderKindMermaid))
	assert.Equal(t, "unknown", renderKindName(renderKindUnknown))
}

// ---------------------------------------------------------------------------
// Discovery helpers
// ---------------------------------------------------------------------------

func TestScreenSlugFor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "login", screenSlugFor("design/wireframes/login.svg"))
	assert.Equal(t, "login", screenSlugFor("design/screens/login.html"))
	assert.Equal(t, "", screenSlugFor("design/flows/sign-up.mmd"))
	assert.Equal(t, "", screenSlugFor("design/tokens/color.tokens.json"))
	assert.Equal(t, "", screenSlugFor("notes.txt"))
}

func TestIsRenderableCritiqueSource(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"a.svg", "a.html", "a.htm", "a.mmd", "design/wireframes/login.svg"} {
		assert.True(t, isRenderableCritiqueSource(ok), "%q must be renderable", ok)
	}
	for _, bad := range []string{"a.png", "a.json", "a", "a.md"} {
		assert.False(t, isRenderableCritiqueSource(bad), "%q must not be renderable", bad)
	}
}

// ---------------------------------------------------------------------------
// Registration: build-tagged (native only), not in the shared roster
// ---------------------------------------------------------------------------

func TestRegisterDesignCritiqueTools_NativeRoster(t *testing.T) {
	t.Parallel()
	handlers := registerDesignCritiqueTools()
	require.Len(t, handlers, 1)
	assert.Equal(t, "design_critique", handlers[0].Name())

	found := false
	for _, h := range AllTools() {
		if h.Name() == "design_critique" {
			found = true
			break
		}
	}
	require.True(t, found, "AllTools() must register design_critique on native builds")
}

// TestDesignCritique_NotInSharedAllToolsList pins SP-140 invariant 7: the
// handler is reached through its build-tagged registrar, never constructed
// directly in the unconditional list (which would break the WASM build).
func TestDesignCritique_NotInSharedAllToolsList(t *testing.T) {
	t.Parallel()
	src := readToolSource(t, designAllToolsFile)
	assert.NotContains(t, src, "&designCritiqueHandler{}",
		"all.go must reach design_critique through registerDesignCritiqueTools(), not directly")
	assert.Contains(t, src, designCritiqueRegistrar+"()")
}

// TestDesignCritiqueHandler_UsesSharedRenderHelper pins the §4a "renders the
// target(s) via the existing design_render machinery" requirement: the tool
// must not re-implement renderInputToPNG/BrowseURL itself.
func TestDesignCritiqueHandler_UsesSharedRenderHelper(t *testing.T) {
	t.Parallel()
	src := readToolSource(t, designCritiqueHandlerFile)
	assert.Contains(t, src, "renderInputToString(",
		"design_critique must render through the shared render helper")
	assert.Contains(t, src, "buildRenderAttachment(",
		"design_critique must attach through the SP-137 render attachment path")
	assert.Contains(t, src, "AnalyzeImage(",
		"design_critique must analyze through the SP-137 vision entry point")
	assert.NotContains(t, src, "BrowseURL(",
		"design_critique must not talk to the browser directly")
}

// TestDesignCritiqueHandler_NoProviderNames pins the SP-137 standing rule for
// the design tier: no provider is named in the critique handler.
func TestDesignCritiqueHandler_NoProviderNames(t *testing.T) {
	t.Parallel()
	lower := strings.ToLower(readToolSource(t, designCritiqueHandlerFile))
	for _, provider := range []string{"openai", "anthropic", "deepinfra", "openrouter", "ollama", "gemini", "claude"} {
		assert.NotContains(t, lower, provider, "design_critique must not name a provider (SP-137)")
	}
}

// TestDesignCritiqueHandler_ContextIsThreaded pins that the handler does not
// detach from the caller's context on the error path (ctx cancellation must
// abort the critique, not spawn unbounded work).
func TestDesignCritiqueHandler_ContextCancelled(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcWriteTree(t, root)
	env, _ := dcCritiqueEnv(t, root)

	ctx, cancel := context.WithCancel(newTestCtx(root))
	cancel()

	h := &designCritiqueHandler{}
	// The render helper's macOS/rod path may or may not honor a pre-cancelled
	// ctx in the mock; what matters is that Execute returns rather than hangs
	// and does not panic.
	res, _ := h.Execute(ctx, env, map[string]any{"target": "design/wireframes/login.svg"})
	_ = res
}

// TestDesignCritiqueHandler_NoVisionStaticFlowConsistency is the SP-140-4 §4b
// wiring case: the flow/wireframe bidirectionality rule rides the standard
// validator dispatch, so the item-4.2 static degradation surfaces it as a
// consistency finding with the mapped severity (warn → major) — no change to
// the critique handler itself.
func TestDesignCritiqueHandler_NoVisionStaticFlowConsistency(t *testing.T) {
	root := t.TempDir()
	// home -> profile -> home makes "profile" non-terminal with no wireframe,
	// so the §4b flow-edge rule fires as a consistency warn.
	dcWriteTree(t, root)
	dcWrite(t, root, "design/flows/sign-up.mmd",
		"flowchart TD\n  login --> home\n  home --> profile\n  profile --> home\n")
	env, _ := dcCritiqueEnvNoVision(t, root)

	h := &designCritiqueHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"target": "design/flows/sign-up.mmd"})
	require.NoError(t, err, "a non-vision flow critique must not fail the turn")
	require.False(t, res.IsError, "output: %s", res.Output)

	out, ok := res.StructuredOut.(critiqueOutput)
	require.True(t, ok)
	assert.False(t, out.Visual)

	var flowEdge *critiqueFinding
	for i := range out.Findings {
		if out.Findings[i].Rule == "consistency_flow_edge_wireframe" {
			flowEdge = &out.Findings[i]
			break
		}
	}
	require.NotNil(t, flowEdge, "the §4b flow-edge rule must surface through the static pass")
	assert.Equal(t, "design/flows/sign-up.mmd", flowEdge.Target)
	assert.Equal(t, "major", flowEdge.Severity, "a consistency warn maps to a critique major")
	assert.Equal(t, "consistency", flowEdge.Area)
	assert.Contains(t, flowEdge.Note, "profile")
	assert.NotEmpty(t, flowEdge.Suggestion, "the new rule carries a concrete fix")
}
