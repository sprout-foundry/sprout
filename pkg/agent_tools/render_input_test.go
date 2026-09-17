//go:build !js

package tools

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// IsRenderableInput — superset of IsHTMLInput covering .svg (SP-140-2 §2c)
// ---------------------------------------------------------------------------

func TestIsRenderableInput_LocalFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		// Inherited HTML behavior.
		{"html extension", "/path/to/wireframe.html", true},
		{"htm extension", "/path/to/page.htm", true},
		{"uppercase HTML", "/path/to/file.HTML", true},
		// New: SVG sources are renderable.
		{"svg extension", "design/wireframes/login.svg", true},
		{"uppercase SVG", "/path/to/icon.SVG", true},
		{"mixed case Svg", "/path/to/flow.Svg", true},
		// Explicitly not renderable.
		{"png", "screenshot.png", false},
		{"jpg", "photo.jpg", false},
		{"mmd", "design/flows/login.mmd", false},
		{"go file", "main.go", false},
		{"no extension", "file", false},
		{"dotfile", ".bashrc", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, IsRenderableInput(tt.path))
		})
	}
}

// TestIsRenderableInput_NonHTTPInputs verifies detection on non-http(s)
// inputs without relying on the HTTP HEAD sniff.
func TestIsRenderableInput_NonHTTPInputs(t *testing.T) {
	t.Parallel()
	// A file:// path points at a local .html file — IsHTMLInput's extension
	// check still recognizes it, so it remains renderable. RenderInputToPNG
	// then re-converts it from a local path (isHTTPURL is false), so the
	// browser receives a normalized file:// URL.
	require.True(t, IsRenderableInput("file:///tmp/page.html"))
	// Other schemes and empty strings are not renderable.
	require.False(t, IsRenderableInput("mailto:dev@example.com"))
	require.False(t, IsRenderableInput(""))
}

// ---------------------------------------------------------------------------
// RenderMode.String
// ---------------------------------------------------------------------------

func TestRenderModeString(t *testing.T) {
	t.Parallel()
	require.Equal(t, "auto", RenderModeAuto.String())
	require.Equal(t, "browser", RenderModeBrowser.String())
	require.Equal(t, "image", RenderModeImage.String())
	require.Equal(t, "unknown", RenderMode(99).String())
}

// ---------------------------------------------------------------------------
// shouldRenderInput — render-mode decision
// ---------------------------------------------------------------------------

func TestShouldRenderInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		mode RenderMode
		want bool
	}{
		// Auto: detection-driven.
		{"auto html", "page.html", RenderModeAuto, true},
		{"auto svg", "wireframe.svg", RenderModeAuto, true},
		{"auto png", "shot.png", RenderModeAuto, false},
		// Browser: forced.
		{"browser png", "shot.png", RenderModeBrowser, true},
		{"browser mmd", "flow.mmd", RenderModeBrowser, true},
		{"browser html", "page.html", RenderModeBrowser, true},
		// Image: never.
		{"image html", "page.html", RenderModeImage, false},
		{"image svg", "wireframe.svg", RenderModeImage, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shouldRenderInput(tc.path, RenderInputOptions{Mode: tc.mode})
			require.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// renderInputToPNG — shared browser render path
// ---------------------------------------------------------------------------

// TestRenderInputToPNG_AutoHTMLAllowsFileURL verifies the shared helper
// browses a local HTML file with a file:// URL and allow_file_url: true,
// returning a cleanup func that removes the temp artifact.
func TestRenderInputToPNG_AutoHTMLAllowsFileURL(t *testing.T) {
	t.Parallel()

	htmlFile, err := os.CreateTemp("", "sprout-render-helper-*.html")
	require.NoError(t, err)
	defer os.Remove(htmlFile.Name())
	htmlFile.WriteString("<html><body>hi</body></html>")
	htmlFile.Close()

	mock := &mockWebBrowserForUIScreenshot{returnResult: "ok"}
	env := ToolEnv{WebBrowser: mock}

	pngPath, cleanup, err := renderInputToPNG(context.Background(), env, "design_render",
		htmlFile.Name(), RenderInputOptions{Mode: RenderModeAuto})
	require.NoError(t, err)
	require.NotEmpty(t, pngPath)

	require.True(t, strings.HasPrefix(mock.lastURL, "file://"), "got %q", mock.lastURL)
	require.Contains(t, mock.lastURL, "sprout-render-helper-")
	require.Equal(t, "screenshot", mock.lastOpts["action"])
	require.Equal(t, true, mock.lastOpts["allow_file_url"], "local render must allow file:// URLs")
	require.Equal(t, float64(1280), mock.lastOpts["viewport_width"], "default width")
	require.Equal(t, float64(720), mock.lastOpts["viewport_height"], "default height")

	// File exists before cleanup, gone after.
	_, statErr := os.Stat(pngPath)
	require.NoError(t, statErr, "artifact should exist before cleanup")
	cleanup()
	_, statErr = os.Stat(pngPath)
	require.True(t, os.IsNotExist(statErr), "artifact should be removed by cleanup")
}

// TestRenderInputToPNG_SVGRendersUnderBrowserMode verifies the SP-140-2 §2c
// acceptance criterion: an .svg source renders to PNG under RenderModeBrowser
// instead of falling through to the raw image/svg+xml vision branch.
func TestRenderInputToPNG_SVGRendersUnderBrowserMode(t *testing.T) {
	t.Parallel()

	svgFile, err := os.CreateTemp("", "sprout-render-*.svg")
	require.NoError(t, err)
	defer os.Remove(svgFile.Name())
	svgFile.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`)
	svgFile.Close()

	mock := &mockWebBrowserForUIScreenshot{returnResult: "ok"}
	env := ToolEnv{WebBrowser: mock}

	pngPath, cleanup, err := renderInputToPNG(context.Background(), env, "design_render",
		svgFile.Name(), RenderInputOptions{Mode: RenderModeBrowser, ViewportWidth: 640, ViewportHeight: 480})
	require.NoError(t, err)
	defer cleanup()

	require.NotEmpty(t, mock.lastURL, "browser must be invoked for SVG under browser mode")
	require.True(t, strings.HasPrefix(mock.lastURL, "file://"), "got %q", mock.lastURL)
	require.Equal(t, true, mock.lastOpts["allow_file_url"])
	require.Equal(t, float64(640), mock.lastOpts["viewport_width"])
	require.Equal(t, float64(480), mock.lastOpts["viewport_height"])
	require.NotEmpty(t, pngPath)
}

// TestRenderInputToPNG_NoBrowserAutoModeFails verifies a browser-less env is
// a fatal error once auto mode has decided to render.
func TestRenderInputToPNG_NoBrowserAutoModeFails(t *testing.T) {
	t.Parallel()

	_, _, err := renderInputToPNG(context.Background(), ToolEnv{}, "design_render",
		"/tmp/page.html", RenderInputOptions{Mode: RenderModeAuto})
	require.Error(t, err)
	require.Contains(t, err.Error(), "browser not available in this environment")
}

// TestRenderInputToPNG_BrowseErrorCleansUp verifies the temp artifact is
// removed when the browser reports a failure.
func TestRenderInputToPNG_BrowseErrorCleansUp(t *testing.T) {
	t.Parallel()

	htmlFile, err := os.CreateTemp("", "sprout-render-err-*.html")
	require.NoError(t, err)
	defer os.Remove(htmlFile.Name())
	htmlFile.WriteString("<html></html>")
	htmlFile.Close()

	mock := &mockWebBrowserForUIScreenshot{returnError: os.ErrInvalid}
	env := ToolEnv{WebBrowser: mock}

	_, _, renderErr := renderInputToPNG(context.Background(), env, "design_render",
		htmlFile.Name(), RenderInputOptions{Mode: RenderModeBrowser})
	require.Error(t, renderErr)
	require.Contains(t, renderErr.Error(), "browser rendering failed")

	screenshotPath, ok := mock.lastOpts["screenshot_path"].(string)
	require.True(t, ok)
	_, statErr := os.Stat(screenshotPath)
	require.True(t, os.IsNotExist(statErr), "temp artifact should be cleaned up on browser error")
}

// ---------------------------------------------------------------------------
// renderInputToString — render-then-analyze contract
// ---------------------------------------------------------------------------

func TestRenderInputToString_NonRenderableSkips(t *testing.T) {
	t.Parallel()

	mock := &mockWebBrowserForUIScreenshot{}
	called := false
	result, rendered, err := renderInputToString(context.Background(), ToolEnv{WebBrowser: mock},
		"design_render", "screenshot.png", RenderInputOptions{Mode: RenderModeAuto},
		func(context.Context, string) (string, error) {
			called = true
			return "analyzed", nil
		})
	require.NoError(t, err)
	require.False(t, rendered)
	require.Empty(t, result)
	require.False(t, called, "analyze must not run for non-renderable input")
	require.Empty(t, mock.lastURL, "browser must not be invoked")
}

func TestRenderInputToString_AnalyzesRenderedArtifact(t *testing.T) {
	t.Parallel()

	htmlFile, err := os.CreateTemp("", "sprout-render-analyze-*.html")
	require.NoError(t, err)
	defer os.Remove(htmlFile.Name())
	htmlFile.WriteString("<html></html>")
	htmlFile.Close()

	mock := &mockWebBrowserForUIScreenshot{returnResult: "ok"}
	var analyzedPath string
	cleanupChecked := false
	result, rendered, err := renderInputToString(context.Background(), ToolEnv{WebBrowser: mock},
		"design_render", htmlFile.Name(), RenderInputOptions{Mode: RenderModeAuto},
		func(_ context.Context, pngPath string) (string, error) {
			analyzedPath = pngPath
			_, statErr := os.Stat(pngPath)
			cleanupChecked = statErr == nil
			return "analysis text", nil
		})
	require.NoError(t, err)
	require.True(t, rendered)
	require.Equal(t, "analysis text", result)
	require.True(t, cleanupChecked, "artifact must exist while analyze runs")
	require.NotEmpty(t, analyzedPath)

	// Cleanup happens after analyze returns.
	_, statErr := os.Stat(analyzedPath)
	require.True(t, os.IsNotExist(statErr), "artifact should be removed after analyze")
}

// TestRenderInputToString_NoBrowserResultCarriesMessage verifies the
// ToolResult.Output text matches the returned Go error message.
func TestRenderInputToString_NoBrowserResultCarriesMessage(t *testing.T) {
	t.Parallel()

	result, rendered, err := renderInputToString(context.Background(), ToolEnv{},
		"analyze_ui_screenshot", "/tmp/page.html",
		RenderInputOptions{Mode: RenderModeAuto, ReportBrowserUnavailable: true},
		nil)
	require.Error(t, err)
	require.True(t, rendered)
	require.Contains(t, result, "no browser is available")
	require.Contains(t, err.Error(), "no browser is available")
}

// ---------------------------------------------------------------------------
// renderSourceFileURL / hasRenderableExtension
// ---------------------------------------------------------------------------

func TestRenderSourceFileURL(t *testing.T) {
	t.Parallel()

	got, err := renderSourceFileURL("https://example.com/a.html")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/a.html", got)

	got, err = renderSourceFileURL("./design/wireframes/login.svg")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(got, "file://"), "got %q", got)
	require.Contains(t, got, "login.svg")
}

func TestHasRenderableExtension(t *testing.T) {
	t.Parallel()

	require.True(t, hasRenderableExtension("a.html"))
	require.True(t, hasRenderableExtension("a.htm"))
	require.True(t, hasRenderableExtension("a.svg"))
	require.True(t, hasRenderableExtension("a.SVG"))
	require.False(t, hasRenderableExtension("a.png"))
	require.False(t, hasRenderableExtension("a.mmd"))
	require.False(t, hasRenderableExtension("https://example.com/a.svg"))
}
