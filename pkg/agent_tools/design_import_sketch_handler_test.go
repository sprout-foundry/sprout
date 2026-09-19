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

	"github.com/sprout-foundry/sprout/pkg/design"
)

// ---------------------------------------------------------------------------
// design_import_sketch handler (SP-140-2 §2c)
// ---------------------------------------------------------------------------

// disWritePNG writes a header-valid 1x1 PNG at rel (slash-separated) under
// root, creating parent directories. validPNG() (vision_sp137_test.go) is the
// smallest image that passes DetectImageMagic.
func disWritePNG(t *testing.T, root, rel string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, validPNG(), 0o644))
	return path
}

// ---------------------------------------------------------------------------
// Definition / Validate / metadata
// ---------------------------------------------------------------------------

func TestDesignImportSketchHandler_Definition(t *testing.T) {
	t.Parallel()
	h := &designImportSketchHandler{}

	require.Equal(t, "design_import_sketch", h.Name())

	def := h.Definition()
	require.Equal(t, "design_import_sketch", def.Name)
	require.NotEmpty(t, def.Description)
	require.Contains(t, def.Description, "design_validate",
		"the tool description must point the agent at the validate step")
	require.ElementsMatch(t, []string{"image_path", "target"}, def.Required)

	params := map[string]ParameterDef{}
	for _, p := range def.Parameters {
		params[p.Name] = p
	}
	for _, want := range []string{"image_path", "target", "screen_name", "analysis_prompt"} {
		assert.Contains(t, params, want, "missing parameter %q", want)
	}
	assert.True(t, params["image_path"].Required)
	assert.True(t, params["target"].Required)
	assert.False(t, params["screen_name"].Required, "screen_name must be optional (§2c)")
	assert.Contains(t, params["target"].Description, "wireframes")
	assert.Contains(t, params["target"].Description, "tokens")
	assert.Contains(t, params["target"].Description, "flows")
}

func TestDesignImportSketchHandler_Metadata(t *testing.T) {
	t.Parallel()
	h := &designImportSketchHandler{}

	require.Nil(t, h.Aliases())
	require.Equal(t, 0, h.MaxResultSize())
	require.False(t, h.SafeForParallel(), "the vision tier is not safe for unbounded parallelism")
	require.False(t, h.Interactive())
}

func TestDesignImportSketchHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &designImportSketchHandler{}

	require.Error(t, h.Validate(map[string]any{}), "image_path and target are required")
	require.Error(t, h.Validate(map[string]any{"image_path": "a.png"}), "target is required")
	require.Error(t, h.Validate(map[string]any{"target": "wireframes"}), "image_path is required")
	require.Error(t, h.Validate(map[string]any{"image_path": 42, "target": "wireframes"}))
	require.Error(t, h.Validate(map[string]any{"image_path": "a.png", "target": 42}))
	require.Error(t, h.Validate(map[string]any{"image_path": "a.png", "target": "wireframes", "screen_name": 1}))
	require.Error(t, h.Validate(map[string]any{"image_path": "a.png", "target": "wireframes", "analysis_prompt": 1}))

	require.NoError(t, h.Validate(map[string]any{"image_path": "a.png", "target": "wireframes"}))
	require.NoError(t, h.Validate(map[string]any{
		"image_path":      "design/feedback/sketch.png",
		"target":          "flows",
		"screen_name":     "sign-up",
		"analysis_prompt": "just the happy path",
	}))
}

// ---------------------------------------------------------------------------
// Happy paths — one per target
// ---------------------------------------------------------------------------

func TestDesignImportSketchHandler_WireframesTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	disWritePNG(t, root, "design/feedback/login-sketch.png")

	h := &designImportSketchHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{
		"image_path":  "design/feedback/login-sketch.png",
		"target":      "wireframes",
		"screen_name": "login",
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "a non-vision primary must not fail the import: %s", res.Output)

	// The image rides along through the SP-137 attachment path.
	require.Len(t, res.Images, 1)
	assert.Equal(t, "image/png", res.Images[0].MIMEType)
	assert.True(t, strings.HasPrefix(res.Images[0].URI, "data:image/png;base64,"))

	out, ok := res.StructuredOut.(sketchImportOutput)
	require.True(t, ok, "StructuredOut should be sketchImportOutput, got %T", res.StructuredOut)
	assert.Equal(t, "design/feedback/login-sketch.png", out.ImagePath)
	assert.Equal(t, "wireframes", out.Target)
	assert.Equal(t, "design/wireframes", out.TargetDir)
	assert.Equal(t, "login", out.ScreenName)
	assert.Equal(t, "login", out.SuggestedName)
	assert.Equal(t, "design/wireframes/login.svg", out.Artifact)
	require.NotEmpty(t, out.Conventions)
	assert.Contains(t, strings.Join(out.Conventions, "\n"), "viewBox")
	require.NotEmpty(t, out.NextSteps)

	// Summary carries the target conventions and the validate reminder.
	require.Contains(t, res.Output, "design/wireframes/login.svg")
	require.Contains(t, res.Output, "Next steps:")
	require.Contains(t, res.Output, "design_validate")
}

func TestDesignImportSketchHandler_TokensTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	disWritePNG(t, root, "sketches/palette.png")

	h := &designImportSketchHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{
		"image_path":  "sketches/palette.png",
		"target":      "tokens",
		"screen_name": "color",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out, ok := res.StructuredOut.(sketchImportOutput)
	require.True(t, ok)
	assert.Equal(t, "tokens", out.Target)
	assert.Equal(t, "design/tokens", out.TargetDir)
	assert.Equal(t, "design/tokens/color.tokens.json", out.Artifact)
	assert.Contains(t, strings.Join(out.Conventions, "\n"), "$value")
	assert.Contains(t, strings.Join(out.Conventions, "\n"), "$type")
	require.Contains(t, res.Output, "design_validate")
}

func TestDesignImportSketchHandler_FlowsTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	disWritePNG(t, root, "sketches/flow-board.png")

	h := &designImportSketchHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{
		"image_path":  "sketches/flow-board.png",
		"target":      "flows",
		"screen_name": "sign-up",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out, ok := res.StructuredOut.(sketchImportOutput)
	require.True(t, ok)
	assert.Equal(t, "flows", out.Target)
	assert.Equal(t, "design/flows", out.TargetDir)
	assert.Equal(t, "design/flows/sign-up.mmd", out.Artifact)
	assert.Contains(t, strings.Join(out.Conventions, "\n"), "flowchart")
	require.Contains(t, res.Output, "design_validate")
}

// ---------------------------------------------------------------------------
// Target conventions are non-empty and target-specific for all three targets
// ---------------------------------------------------------------------------

func TestSketchConventions_CoverEveryTarget(t *testing.T) {
	t.Parallel()
	for _, target := range sketchTargets {
		conv := sketchConventions[target]
		require.NotEmpty(t, conv, "target %q must carry conventions", target)
		require.NotEmpty(t, targetDirFor(target), "target %q must map to a design/ subdir", target)
		require.NotEmpty(t, sketchArtifactPath(target, "example"), "target %q must map to an artifact path", target)
		require.True(t, isSketchTargetName(target))
	}

	// The target set is exactly §2c's three, and each is a real design/
	// subdirectory (guards against a typo drifting from the contract).
	require.Equal(t, []string{"wireframes", "tokens", "flows"}, sketchTargets)
	for _, target := range sketchTargets {
		_, ok := design.SubdirByName(target)
		assert.True(t, ok, "%q must be a canonical design/ subdirectory", target)
	}
	// Every artifact path must live under design/.
	for _, target := range sketchTargets {
		assert.True(t, strings.HasPrefix(sketchArtifactPath(target, "x"), design.DirName+"/"),
			"target %q artifact must be under design/", target)
	}
}

// ---------------------------------------------------------------------------
// screen_name handling
// ---------------------------------------------------------------------------

func TestDesignImportSketchHandler_SuggestedSlugFromImageBasename(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	disWritePNG(t, root, "sketches/Login Screen 2.PNG")

	h := &designImportSketchHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{
		"image_path": "sketches/Login Screen 2.PNG",
		"target":     "wireframes",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out, ok := res.StructuredOut.(sketchImportOutput)
	require.True(t, ok)
	assert.Empty(t, out.ScreenName, "no screen_name was supplied")
	assert.Equal(t, "login-screen-2", out.SuggestedName)
	assert.Equal(t, "design/wireframes/login-screen-2.svg", out.Artifact)
}

func TestSlugifyImageStem(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"a/Login Screen.png":     "login-screen",
		"shot-1.jpg":             "shot-1",
		"___.png":                "fallback",
		"Ünïcödé.png":            "n-c-d", // non-ASCII lowers to U+FFFD and is dropped
		"whiteboard photo 3.JPG": "whiteboard-photo-3",
		"already-kebab.png":      "already-kebab",
	}
	for in, want := range cases {
		assert.Equal(t, want, slugifyImageStem(in, "fallback"), "slugifyImageStem(%q)", in)
	}
}

func TestIsSlug(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"login", "sign-up", "a1", "login-screen-2"} {
		assert.True(t, isSlug(ok), "%q is a valid slug", ok)
	}
	for _, bad := range []string{"", "Login", "sign_up", "-leading", "trailing-", "double--hyphen", "sp ace", "é"} {
		assert.False(t, isSlug(bad), "%q is not a valid slug", bad)
	}
	// The hand-rolled rule must agree with the published pattern's rule for
	// the sample set above (design.SlugPattern is the contract text).
	require.Equal(t, `^[a-z0-9]+(-[a-z0-9]+)*$`, design.SlugPattern)
}

func TestDesignImportSketchHandler_InvalidScreenName(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	disWritePNG(t, root, "sketches/login.png")

	h := &designImportSketchHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{
		"image_path":  "sketches/login.png",
		"target":      "wireframes",
		"screen_name": "Login Screen",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "not a valid slug")
	require.Contains(t, res.Output, design.SlugPattern)
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

func TestDesignImportSketchHandler_UnsupportedTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	disWritePNG(t, root, "sketches/login.png")

	h := &designImportSketchHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{
		"image_path": "sketches/login.png",
		"target":     "brand",
	})
	require.Error(t, err)
	require.True(t, res.IsError, "brand is not one of the three §2c import targets")
	require.Contains(t, res.Output, "unsupported target")
	require.Contains(t, res.Output, "wireframes, tokens, flows")
}

func TestDesignImportSketchHandler_NonImageSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	disWritePNG(t, root, "sketches/login.png")
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes.md"), []byte("# hi"), 0o644))

	h := &designImportSketchHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{
		"image_path": "notes.md",
		"target":     "wireframes",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "not a recognized image file")
}

func TestDesignImportSketchHandler_URLRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	h := &designImportSketchHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{
		"image_path": "https://example.com/login.png",
		"target":     "wireframes",
	})
	require.Error(t, err)
	require.True(t, res.IsError, "a URL is not a workspace-local import source")
	require.Contains(t, res.Output, "workspace-local image")
}

func TestDesignImportSketchHandler_MissingArgs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	h := &designImportSketchHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError)

	res, err = h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"image_path": "sketches/a.png"})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestDesignImportSketchHandler_MissingFileStillReturnsBrief(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// The file does not exist: the attachment degrades to nothing, but the
	// brief is still complete and the tool does not fail.
	h := &designImportSketchHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{
		"image_path": "sketches/absent.png",
		"target":     "wireframes",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	assert.Empty(t, res.Images)
	out, ok := res.StructuredOut.(sketchImportOutput)
	require.True(t, ok)
	assert.Equal(t, "design/wireframes/absent.svg", out.Artifact)
}

// ---------------------------------------------------------------------------
// Gate-1 precheck (SP-140 invariant 7 / §2e)
// ---------------------------------------------------------------------------

func TestDesignImportSketchHandler_Gate1Deny(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	disWritePNG(t, root, "sketches/login.png")

	env := newTestEnv(t, root)
	env.FileAccessClassifier = denyClassifier{}

	h := &designImportSketchHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{
		"image_path": "sketches/login.png",
		"target":     "wireframes",
	})
	require.Error(t, err)
	require.True(t, res.IsError, "a Gate-1 deny is a tool failure")
	require.Contains(t, res.Output, "design_import_sketch blocked")
}

func TestDesignImportSketchHandler_Gate1Allow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	disWritePNG(t, root, "sketches/login.png")

	env := newTestEnv(t, root)
	env.FileAccessClassifier = allowClassifier{}

	h := &designImportSketchHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{
		"image_path": "sketches/login.png",
		"target":     "wireframes",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Len(t, res.Images, 1, "an allowed path must still attach the image")
}

// TestDesignImportSketchHandler_OffWorkspaceDenied is the SP-140-2 §2e
// negative test: an image_path pointing outside the workspace must be denied
// by Gate 1 and fail the tool before the vision tier ever sees it.
//
// Unlike the denyClassifier tests above (which stub the verdict), this drives
// the *real* precheck path: the classifier always answers "prompt", which is
// what the production classifier returns for a path outside the workspace when
// no session allowlist covers it, and the prompter declines. Nothing may leak
// off-workspace: no image attached, no brief produced.
func TestDesignImportSketchHandler_OffWorkspaceDenied(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := outsideWorkspaceTarget(t) // <home>/.sprout-test-outside/marker.txt

	denied := &fakeFSPrompter{} // approve=false
	env := newTestEnv(t, root)
	env.FileAccessClassifier = &fakeClassifier{verdict: "prompt"}
	env.FileAccessPrompter = denied

	h := &designImportSketchHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{
		"image_path": outside,
		"target":     "wireframes",
	})
	require.Error(t, err, "an off-workspace image_path must fail the tool")
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "design_import_sketch blocked")
	require.Contains(t, res.Output, "not approved")
	require.Equal(t, 1, denied.called, "the off-workspace path must reach the approval prompt")
	assert.Empty(t, res.Images, "no off-workspace image may be attached")
	_, ok := res.StructuredOut.(sketchImportOutput)
	assert.False(t, ok, "a denied import must not produce a structured brief")
}

// ---------------------------------------------------------------------------
// Output reminders (§2c: the tool's output reminds the agent to validate)
// ---------------------------------------------------------------------------

func TestSketchNextSteps_AlwaysEndWithValidate(t *testing.T) {
	t.Parallel()
	for _, target := range sketchTargets {
		steps := sketchNextSteps(target, "login")
		require.NotEmpty(t, steps)
		joined := strings.Join(steps, "\n")
		require.Contains(t, joined, "design_validate", "target %q must direct the agent to validate", target)

		// The reminder is the last annotation, not just present somewhere.
		assert.Contains(t, steps[len(steps)-1], "design_validate")

		// Every target must also point at the artifact location it produces.
		assert.Contains(t, joined, sketchArtifactPath(target, "login"))
	}
}

func TestBuildSketchImportSummary_VisionTierUnavailable(t *testing.T) {
	t.Parallel()
	out := sketchImportOutput{
		ImagePath:   "sketches/login.png",
		Target:      "wireframes",
		TargetDir:   "design/wireframes",
		Artifact:    "design/wireframes/login.svg",
		Conventions: sketchConventions[sketchTargetWireframes],
		NextSteps:   sketchNextSteps(sketchTargetWireframes, "login"),
	}

	// No analysis text, no error: non-vision primary guidance, not a failure.
	summary := buildSketchImportSummary(out, true, "", nil)
	require.Contains(t, summary, "analyze_image_content")
	require.Contains(t, summary, "design_validate")
	require.Contains(t, summary, "Conventions for wireframes")

	// Analysis text present: it is surfaced verbatim after the brief.
	summary = buildSketchImportSummary(out, true, "  Header, form, submit button.  ", nil)
	require.Contains(t, summary, "Extracted structure (vision tier):")
	require.Contains(t, summary, "Header, form, submit button.")

	// Real vision error: reported, still no failure.
	summary = buildSketchImportSummary(out, false, "", assert.AnError)
	require.Contains(t, summary, "analyze_image_content")
	require.NotContains(t, summary, "header")
}

func TestSketchAnalysisPrompt_CarriesTargetAndExtra(t *testing.T) {
	t.Parallel()
	out := sketchImportOutput{Artifact: "design/wireframes/login.svg", ScreenName: "login"}

	p := sketchAnalysisPrompt(sketchTargetWireframes, out, "focus on the form fields")
	require.Contains(t, p, "design/wireframes/login.svg")
	require.Contains(t, p, "focus on the form fields")
	require.NotContains(t, p, "propose a slug", "a supplied screen_name needs no slug proposal")

	// Token extraction prompt names DTCG concepts.
	pt := sketchAnalysisPrompt(sketchTargetTokens, sketchImportOutput{Artifact: "design/tokens/color.tokens.json"}, "")
	require.Contains(t, pt, "DTCG")
	require.Contains(t, pt, "propose a slug")

	// Flow extraction prompt asks for slugs.
	pf := sketchAnalysisPrompt(sketchTargetFlows, sketchImportOutput{Artifact: "design/flows/f.mmd"}, "")
	require.Contains(t, pf, "slug")
}

// ---------------------------------------------------------------------------
// Structured output shape
// ---------------------------------------------------------------------------

func TestDesignImportSketchHandler_StructuredJSONShape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	disWritePNG(t, root, "sketches/login.png")

	h := &designImportSketchHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{
		"image_path":  "sketches/login.png",
		"target":      "wireframes",
		"screen_name": "login",
	})
	require.NoError(t, err)

	data, err := json.Marshal(res.StructuredOut)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Equal(t, "sketches/login.png", raw["imagePath"])
	assert.Equal(t, "wireframes", raw["target"])
	assert.Equal(t, "design/wireframes", raw["targetDir"])
	assert.Equal(t, "login", raw["screenName"])
	assert.Equal(t, "design/wireframes/login.svg", raw["artifact"])
	assert.Contains(t, raw, "conventions")
	assert.Contains(t, raw, "nextSteps")
	// The tool never writes into design/, so there is no "created"/"wrote"
	// field to mislead the model into thinking the import already happened.
	assert.NotContains(t, raw, "created")
	assert.NotContains(t, raw, "wrote")
}

// ---------------------------------------------------------------------------
// Registration: build-tagged (native only), not in the shared roster
// ---------------------------------------------------------------------------

func TestRegisterDesignImportSketchTools_NativeRoster(t *testing.T) {
	t.Parallel()
	handlers := registerDesignImportSketchTools()
	require.Len(t, handlers, 1)
	assert.Equal(t, "design_import_sketch", handlers[0].Name())

	// AllTools() must include it on native builds (the build-tagged append).
	found := false
	for _, h := range AllTools() {
		if h.Name() == "design_import_sketch" {
			found = true
			break
		}
	}
	require.True(t, found, "AllTools() must register design_import_sketch on native builds")
}

// compile-time interface check.
var _ ToolHandler = (*designImportSketchHandler)(nil)

// The handler must be behind a build tag, like design_render: it is not in the
// shared literal list in all.go (which is unconditional). Guard that by
// checking AllTools sources it from the tagged registrar.
func TestDesignImportSketch_NotInSharedLiteralList(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("all.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(src), "designImportSketchHandler{}",
		"design_import_sketch must be registered via the build-tagged registrar, not unconditionally in all.go")
	assert.Contains(t, string(src), "registerDesignImportSketchTools()")
}

// silence the unused-import guard for context on some build configs.
var _ = context.Background
