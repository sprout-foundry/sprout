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
)

// ---------------------------------------------------------------------------
// design_render handler (SP-140-2 §2c)
// ---------------------------------------------------------------------------

// drWrite writes rel (slash-separated) under root with parent directories.
func drWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

const drTestSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Login</text>
</svg>`

const drTestHTML = `<!DOCTYPE html><html><body><h1>Login</h1></body></html>`

const drTestMMD = "flowchart TD\n  login --> home\n"

// drWriteTree seeds a minimal design/ tree with an SVG wireframe, an HTML
// screen, and a mermaid flow.
func drWriteTree(t *testing.T, root string) {
	t.Helper()
	drWrite(t, root, "design/wireframes/login.svg", drTestSVG)
	drWrite(t, root, "design/screens/login.html", drTestHTML)
	drWrite(t, root, "design/flows/sign-up.mmd", drTestMMD)
}

// drMockBrowser writes a minimal valid PNG to the screenshot_path option so
// the handler's SP-137 attachment step finds a real artifact, and records the
// URL/opts for assertions.
type drMockBrowser struct {
	lastURL  string
	lastOpts map[string]any
	pngBytes []byte
	calls    int
}

func (m *drMockBrowser) BrowseURL(_ context.Context, url string, opts map[string]any) (string, error) {
	m.calls++
	m.lastURL = url
	m.lastOpts = opts
	if p, ok := opts["screenshot_path"].(string); ok && len(m.pngBytes) > 0 {
		_ = os.WriteFile(p, m.pngBytes, 0o644)
	}
	return "ok", nil
}

// drTinyPNG is a 1x1 PNG so buildRenderAttachment finds a readable image.
var drTinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

// ---------------------------------------------------------------------------
// Definition / Validate
// ---------------------------------------------------------------------------

func TestDesignRenderHandler_Definition(t *testing.T) {
	t.Parallel()
	h := &designRenderHandler{}
	require.Equal(t, "design_render", h.Name())

	def := h.Definition()
	require.Equal(t, "design_render", def.Name)
	require.Contains(t, def.Required, "source")

	names := map[string]bool{}
	for _, p := range def.Parameters {
		names[p.Name] = true
	}
	for _, want := range []string{"source", "analysis_prompt", "viewport_width", "viewport_height", "flow_layout"} {
		assert.True(t, names[want], "missing parameter %q", want)
	}
}

func TestDesignRenderHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &designRenderHandler{}

	require.Error(t, h.Validate(map[string]any{}), "source is required")
	require.NoError(t, h.Validate(map[string]any{"source": "design/wireframes/login.svg"}))
	require.Error(t, h.Validate(map[string]any{"source": 42}))
	require.Error(t, h.Validate(map[string]any{"source": "a.svg", "analysis_prompt": 1}))
	require.Error(t, h.Validate(map[string]any{"source": "a.svg", "flow_layout": 1}))
	require.NoError(t, h.Validate(map[string]any{"source": "a.mmd", "flow_layout": "left-right"}))
}

// ---------------------------------------------------------------------------
// SVG / HTML render paths
// ---------------------------------------------------------------------------

func TestDesignRenderHandler_RendersSVGViaBrowser(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	drWriteTree(t, root)

	mock := &drMockBrowser{pngBytes: drTinyPNG}
	env := newTestEnv(t, root)
	env.WebBrowser = mock

	h := &designRenderHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{
		"source":          "design/wireframes/login.svg",
		"viewport_width":  640,
		"viewport_height": 480,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	require.Equal(t, 1, mock.calls, "browser must render the SVG")
	require.True(t, strings.HasPrefix(mock.lastURL, "file://"), "got %q", mock.lastURL)
	require.Contains(t, mock.lastURL, "login.svg")
	require.Equal(t, true, mock.lastOpts["allow_file_url"])
	require.Equal(t, float64(640), mock.lastOpts["viewport_width"])
	require.Equal(t, float64(480), mock.lastOpts["viewport_height"])

	require.Len(t, res.Images, 1, "the rendered PNG must be attached (SP-137)")
	assert.True(t, strings.HasPrefix(res.Images[0].URI, "data:image/png;base64,"))
	assert.Equal(t, "image/png", res.Images[0].MIMEType)
	require.Contains(t, res.Output, "design_render: rendered design/wireframes/login.svg")
}

func TestDesignRenderHandler_RendersHTMLViaBrowser(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	drWriteTree(t, root)

	mock := &drMockBrowser{pngBytes: drTinyPNG}
	env := newTestEnv(t, root)
	env.WebBrowser = mock

	h := &designRenderHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"source": "design/screens/login.html"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	require.Equal(t, 1, mock.calls)
	require.Contains(t, mock.lastURL, "login.html")
	// Defaults apply when viewport args are omitted.
	require.Equal(t, float64(defaultViewportWidth), mock.lastOpts["viewport_width"])
	require.Equal(t, float64(defaultViewportHeight), mock.lastOpts["viewport_height"])
	require.Len(t, res.Images, 1)
}

// ---------------------------------------------------------------------------
// Mermaid path — standalone HTML, source never mutated
// ---------------------------------------------------------------------------

func TestDesignRenderHandler_MermaidRendersStandaloneHTML(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	drWriteTree(t, root)

	mock := &drMockBrowser{pngBytes: drTinyPNG}
	env := newTestEnv(t, root)
	env.WebBrowser = mock

	h := &designRenderHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"source": "design/flows/sign-up.mmd"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Len(t, res.Images, 1)

	// The browser must render a generated HTML page, not the .mmd itself.
	require.Contains(t, mock.lastURL, ".html", "mermaid must render a generated HTML page")
	require.NotContains(t, mock.lastURL, ".mmd", "the .mmd must never be handed to the browser")
	require.Contains(t, res.Output, "rendered flow design/flows/sign-up.mmd")
	require.Contains(t, res.Output, "source unchanged")
}

func TestDesignRenderHandler_MermaidSourceNeverMutated(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	drWriteTree(t, root)

	mmdPath := filepath.Join(root, "design", "flows", "sign-up.mmd")
	before, err := os.ReadFile(mmdPath)
	require.NoError(t, err)

	mock := &drMockBrowser{pngBytes: drTinyPNG}
	env := newTestEnv(t, root)
	env.WebBrowser = mock

	h := &designRenderHandler{}
	_, err = h.Execute(newTestCtx(root), env, map[string]any{
		"source":      "design/flows/sign-up.mmd",
		"flow_layout": "left-right",
	})
	require.NoError(t, err)

	after, err := os.ReadFile(mmdPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "the .mmd source must be byte-for-byte unchanged")
}

// ---------------------------------------------------------------------------
// flow_layout / applyFlowLayout / buildMermaidHTML
// ---------------------------------------------------------------------------

func TestNormaliseFlowLayout(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"top-down":    "TD",
		"TD":          "TD",
		"left-right":  "LR",
		"lr":          "LR",
		"bottom-up":   "BT",
		"right-left":  "RL",
		"":            "",
		"sideways":    "",
		"  top-down ": "TD",
	}
	for in, want := range cases {
		assert.Equal(t, want, normaliseFlowLayout(in), "input %q", in)
	}
}

func TestApplyFlowLayout(t *testing.T) {
	t.Parallel()

	// Replaces an existing direction keyword.
	require.Equal(t, "flowchart LR\n  a --> b\n",
		applyFlowLayout("flowchart TD\n  a --> b\n", "LR"))
	// Adds a keyword to a bare declaration.
	require.Equal(t, "flowchart LR\n  a --> b\n",
		applyFlowLayout("flowchart\n  a --> b\n", "LR"))
	// Preserves a leading comment line.
	require.Equal(t, "%% note\nflowchart BT\n  a --> b\n",
		applyFlowLayout("%% note\nflowchart TD\n  a --> b\n", "BT"))
	// graph keyword is recognized too.
	require.Equal(t, "graph RL\n  a --> b\n",
		applyFlowLayout("graph TD\n  a --> b\n", "RL"))
	// Empty direction is a no-op.
	require.Equal(t, "flowchart TD\n  a --> b\n",
		applyFlowLayout("flowchart TD\n  a --> b\n", ""))
	// No declaration is a no-op.
	require.Equal(t, "a --> b\n", applyFlowLayout("a --> b\n", "LR"))
}

func TestBuildMermaidHTML_EmbedsScriptAndSource(t *testing.T) {
	t.Parallel()
	html, err := buildMermaidHTML("flowchart TD\n  login --> home\n", "LR")
	require.NoError(t, err)
	require.Contains(t, html, "<!DOCTYPE html>")
	require.Contains(t, html, "globalThis.mermaid") // the vendored bundle is inline
	require.Contains(t, html, "mermaid.render")
	require.Contains(t, html, `flowchart LR`) // layout applied at render time
	// No external script/style references — the page is offline-safe. The
	// vendored bundle may carry URLs inside its own comments, so check the
	// tags we emit, not arbitrary substrings.
	require.NotContains(t, html, `src="http`)
	require.NotContains(t, html, `<link`)
	require.NotContains(t, html, `<script src`)
}

func TestBuildMermaidHTML_EscapesClosingScriptTag(t *testing.T) {
	t.Parallel()
	// A malicious source containing </script> must not terminate the block.
	html, err := buildMermaidHTML("flowchart TD\n  a[\"</script><script>alert(1)</script>\"] --> b\n", "")
	require.NoError(t, err)
	require.NotContains(t, html, "</script><script>alert(1)</script>",
		"the embedded source must not be able to inject a script tag")
	require.Contains(t, html, `\u003c/script`)
}

// ---------------------------------------------------------------------------
// analysis_prompt passthrough
// ---------------------------------------------------------------------------

func TestDesignRenderHandler_AnalysisPromptPassthrough(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	drWriteTree(t, root)

	mock := &drMockBrowser{pngBytes: drTinyPNG}
	env := newTestEnv(t, root)
	env.WebBrowser = mock

	h := &designRenderHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{
		"source":          "design/wireframes/login.svg",
		"analysis_prompt": "does the primary action read clearly?",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	// The prompt is surfaced in the summary; the vision tier receives it via
	// the analyze callback (AnalyzeImage's analysisPrompt argument).
	require.Contains(t, res.Output, "does the primary action read clearly?")
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

func TestDesignRenderHandler_UnsupportedSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	drWriteTree(t, root)

	h := &designRenderHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"source": "design/wireframes/login.png"})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "unsupported source")
}

func TestDesignRenderHandler_MissingSourceArg(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	h := &designRenderHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestDesignRenderHandler_NoBrowserFails(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	drWriteTree(t, root)

	// No WebBrowser wired — design_render is browser-tier by construction,
	// so this is a fatal (not silent-fallback) error.
	h := &designRenderHandler{}
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"source": "design/wireframes/login.svg"})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "browser not available")
}

func TestDesignRenderHandler_Gate1Deny(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	drWriteTree(t, root)

	env := newTestEnv(t, root)
	env.FileAccessClassifier = denyClassifier{}
	// A browser is present so the deny (which happens before render) is the
	// cause of failure, not a missing browser.
	env.WebBrowser = &drMockBrowser{pngBytes: drTinyPNG}

	h := &designRenderHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"source": "design/wireframes/login.svg"})
	require.Error(t, err)
	require.True(t, res.IsError, "a Gate-1 deny is a tool failure")
	require.Contains(t, res.Output, "design_render blocked")
}

func TestDesignRenderHandler_Gate1AllowRenders(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	drWriteTree(t, root)

	env := newTestEnv(t, root)
	env.FileAccessClassifier = allowClassifier{}
	mock := &drMockBrowser{pngBytes: drTinyPNG}
	env.WebBrowser = mock

	h := &designRenderHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"source": "design/wireframes/login.svg"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, 1, mock.calls)
}

// TestDesignRenderHandler_OffWorkspaceDenied is the SP-140-2 §2e negative
// test: a source file outside the workspace must be denied by Gate 1 and fail
// the tool before the headless browser is ever pointed at it.
//
// The classifier answers "prompt" (the production verdict for an off-workspace
// path with no session allowlist) and the prompter declines, so the render
// must not happen: the injected browser records zero calls.
func TestDesignRenderHandler_OffWorkspaceDenied(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	drWriteTree(t, root)

	// design/-relative (so the source-type gate passes) but escaping the
	// workspace via path traversal: the Gate-1 precheck is the cause of
	// failure.
	source := "design/../../design/wireframes/login.svg"

	denied := &fakeFSPrompter{} // approve=false
	mock := &drMockBrowser{pngBytes: drTinyPNG}

	env := newTestEnv(t, root)
	env.FileAccessClassifier = &fakeClassifier{verdict: "prompt"}
	env.FileAccessPrompter = denied
	env.WebBrowser = mock

	h := &designRenderHandler{}
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"source": source})
	require.Error(t, err, "an off-workspace source must fail the tool")
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "design_render blocked")
	require.Contains(t, res.Output, "not approved")
	require.Equal(t, 1, denied.called, "the off-workspace path must reach the approval prompt")
	assert.Zero(t, mock.calls, "the browser must never render a denied off-workspace source")
	assert.Empty(t, res.Images, "no off-workspace render may be attached")
}

// ---------------------------------------------------------------------------
// Classification
// ---------------------------------------------------------------------------

func TestClassifyDesignSource(t *testing.T) {
	t.Parallel()
	require.Equal(t, renderKindBrowser, classifyDesignSource("design/wireframes/login.svg"))
	require.Equal(t, renderKindBrowser, classifyDesignSource("design/screens/login.html"))
	require.Equal(t, renderKindBrowser, classifyDesignSource("design/screens/login.htm"))
	require.Equal(t, renderKindMermaid, classifyDesignSource("design/flows/sign-up.mmd"))
	require.Equal(t, renderKindUnknown, classifyDesignSource("design/wireframes/login.png"))
	require.Equal(t, renderKindUnknown, classifyDesignSource("README.md"))
}
