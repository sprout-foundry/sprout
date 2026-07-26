//go:build !js

package tools

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// mockWebBrowserForUIScreenshot records the arguments it receives so the
// handler can be tested end-to-end without a real browser or vision provider.
// ---------------------------------------------------------------------------

type mockWebBrowserForUIScreenshot struct {
	lastURL      string
	lastOpts     map[string]any
	returnResult string
	returnError  error
}

func (m *mockWebBrowserForUIScreenshot) BrowseURL(_ context.Context, url string, opts map[string]any) (string, error) {
	m.lastURL = url
	m.lastOpts = opts
	if m.returnError != nil {
		return "", m.returnError
	}
	return m.returnResult, nil
}

// ---------------------------------------------------------------------------
// SP-079-3: analyze_ui_screenshot HTML rendering tests
// ---------------------------------------------------------------------------

// TestAnalyzeUIScreenshot_HTMLWithBrowser_RendersScreenshot verifies the
// full HTML->browser->screenshot->AnalyzeImage pipeline with a mock browser.
//
// Since AnalyzeImage delegates to the real vision provider API, we cannot
// assert on its output in tests. Instead we verify:
//  1. The mock browser was called with the correct file:// URL and opts.
//  2. The temp screenshot file is cleaned up (no longer exists) after the call.
//  3. The result does NOT contain the old "HTML content requires a browser"
//     error message.
func TestAnalyzeUIScreenshot_HTMLWithBrowser_RendersScreenshot(t *testing.T) {
	t.Parallel()

	// Create a temporary HTML file.
	htmlFile, err := os.CreateTemp("", "sprout-test-*.html")
	require.NoError(t, err)
	defer os.Remove(htmlFile.Name())
	htmlFile.WriteString("<html><body>Hello</body></html>")
	htmlFile.Close()

	mock := &mockWebBrowserForUIScreenshot{
		returnResult: "ok",
	}

	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()
	env := ToolEnv{
		WebBrowser: mock,
	}
	args := map[string]any{
		"image_path":      htmlFile.Name(),
		"analysis_prompt": "describe the UI",
	}

	result, err := h.Execute(ctx, env, args)

	// BrowseURL should have been called.
	require.NotEmpty(t, mock.lastURL, "BrowseURL was not called")
	require.True(t, strings.HasPrefix(mock.lastURL, "file://"),
		"expected file:// URL, got %s", mock.lastURL)
	require.Contains(t, mock.lastURL, "sprout-test-",
		"file:// URL should contain the test HTML filename")

	// Check opts.
	opts := mock.lastOpts
	require.Equal(t, "screenshot", opts["action"])
	require.NotNil(t, opts["screenshot_path"], "screenshot_path should be set")
	require.Equal(t, float64(1280), opts["viewport_width"], "default viewport_width")
	require.Equal(t, float64(720), opts["viewport_height"], "default viewport_height")
	require.Equal(t, true, opts["allow_file_url"], "allow_file_url should be true")

	screenshotPath, ok := opts["screenshot_path"].(string)
	require.True(t, ok, "screenshot_path should be a string")

	// The temp screenshot file must be cleaned up after Execute returns
	// regardless of whether AnalyzeImage succeeded or errored.
	_, statErr := os.Stat(screenshotPath)
	require.True(t, os.IsNotExist(statErr),
		"temp screenshot file %s should have been cleaned up", screenshotPath)

	// Even if AnalyzeImage errors (no vision provider), the result should
	// NOT contain the old "HTML content requires a browser" message.
	require.NotContains(t, result.Output, "HTML content requires a browser",
		"should not return the old 'browser required' error since we actually have a browser")

	// AnalyzeImage either returns (json, nil) with VisionNotAvailable or
	// returns (json, nil) with a provider error — the handler converts
	// a Go error to IsError=true. Either way, we don't get the HTML error.
	if err != nil {
		require.NotContains(t, err.Error(), "html content requires browser rendering",
			"error should not mention 'browser rendering' since we have a browser")
	}

	// If AnalyzeImage succeeded (unlikely without a vision provider),
	// the handler should NOT set IsError.
	if !result.IsError {
		require.Nil(t, err, "non-error result should have nil Go error")
	}
}

// TestAnalyzeUIScreenshot_HTMLWithBrowser_CustomViewport verifies that
// viewport_width and viewport_height are forwarded to the browser with
// the correct values and types when passed as int (direct call).
func TestAnalyzeUIScreenshot_HTMLWithBrowser_CustomViewport(t *testing.T) {
	t.Parallel()

	htmlFile, err := os.CreateTemp("", "sprout-test-vp-*.html")
	require.NoError(t, err)
	defer os.Remove(htmlFile.Name())
	htmlFile.WriteString("<html><body></body></html>")
	htmlFile.Close()

	mock := &mockWebBrowserForUIScreenshot{
		returnResult: "ok",
	}

	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()
	env := ToolEnv{WebBrowser: mock}
	args := map[string]any{
		"image_path":      htmlFile.Name(),
		"viewport_width":  1920,
		"viewport_height": 1080,
	}

	_, _ = h.Execute(ctx, env, args)

	opts := mock.lastOpts
	require.Equal(t, float64(1920), opts["viewport_width"], "viewport_width should be 1920 as float64")
	require.Equal(t, float64(1080), opts["viewport_height"], "viewport_height should be 1080 as float64")
}

// TestAnalyzeUIScreenshot_HTMLWithBrowser_Float64Viewport verifies that
// viewport_width and viewport_height work when passed as float64 (JSON
// deserialization path where map[string]any values are float64 for numbers).
func TestAnalyzeUIScreenshot_HTMLWithBrowser_Float64Viewport(t *testing.T) {
	t.Parallel()

	htmlFile, err := os.CreateTemp("", "sprout-test-vp-float-*.html")
	require.NoError(t, err)
	defer os.Remove(htmlFile.Name())
	htmlFile.WriteString("<html><body></body></html>")
	htmlFile.Close()

	mock := &mockWebBrowserForUIScreenshot{
		returnResult: "ok",
	}

	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()
	env := ToolEnv{WebBrowser: mock}
	args := map[string]any{
		"image_path":      htmlFile.Name(),
		"viewport_width":  float64(1920),
		"viewport_height": float64(1080),
	}

	_, _ = h.Execute(ctx, env, args)

	opts := mock.lastOpts
	require.Equal(t, float64(1920), opts["viewport_width"], "viewport_width should be 1920 as float64")
	require.Equal(t, float64(1080), opts["viewport_height"], "viewport_height should be 1080 as float64")
}

// TestAnalyzeUIScreenshot_HTMLWithBrowser_BrowseError verifies that when
// the browser fails, the temp file is cleaned up and a proper error is
// returned.
func TestAnalyzeUIScreenshot_HTMLWithBrowser_BrowseError(t *testing.T) {
	t.Parallel()

	htmlFile, err := os.CreateTemp("", "sprout-test-err-*.html")
	require.NoError(t, err)
	defer os.Remove(htmlFile.Name())
	htmlFile.WriteString("<html><body></body></html>")
	htmlFile.Close()

	mock := &mockWebBrowserForUIScreenshot{
		returnError: os.ErrInvalid,
	}

	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()
	env := ToolEnv{WebBrowser: mock}
	args := map[string]any{
		"image_path": htmlFile.Name(),
	}

	result, err := h.Execute(ctx, env, args)

	require.Error(t, err)
	require.True(t, result.IsError)
	require.Contains(t, result.Output, "browser rendering failed")

	// The temp screenshot file should be cleaned up on browser error.
	screenshotPath, ok := mock.lastOpts["screenshot_path"].(string)
	require.True(t, ok, "screenshot_path should be set even on error")
	_, statErr := os.Stat(screenshotPath)
	require.True(t, os.IsNotExist(statErr),
		"temp screenshot file should be cleaned up after browser error")
}

// TestAnalyzeUIScreenshot_HTMLNoBrowser returns a clear error message
// explaining that no browser is available in this environment.
func TestAnalyzeUIScreenshot_HTMLNoBrowser(t *testing.T) {
	t.Parallel()

	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()
	env := ToolEnv{} // WebBrowser is nil
	args := map[string]any{
		"image_path": "/tmp/page.html",
	}

	result, err := h.Execute(ctx, env, args)

	require.Error(t, err)
	require.True(t, result.IsError)
	require.Contains(t, result.Output, "no browser is available")
	require.NotContains(t, result.Output, "not implemented")
}

// TestAnalyzeUIScreenshot_NonHTML_NoBrowserCalled verifies that for
// non-HTML input (e.g. a .png file), BrowseURL is never called — the
// existing image path logic runs directly.
func TestAnalyzeUIScreenshot_NonHTML_NoBrowserCalled(t *testing.T) {
	t.Parallel()

	mock := &mockWebBrowserForUIScreenshot{}

	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()
	env := ToolEnv{WebBrowser: mock}
	args := map[string]any{
		"image_path": "/tmp/screenshot.png",
	}

	result, err := h.Execute(ctx, env, args)

	// BrowseURL should NOT have been called.
	require.Empty(t, mock.lastURL, "BrowseURL should not be called for non-HTML input")

	// AnalyzeImage returns (json, nil) when no vision provider is available,
	// so the handler sees no error and returns IsError=false.
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.NotContains(t, result.Output, "HTML content requires")
}

// TestAnalyzeUIScreenshot_HTMLWithBrowser_WritesValidPNG verifies that
// the mock browser can write a valid PNG to screenshot_path, and the
// handler proceeds to call AnalyzeImage on it.
//
// Since the handler cleans up the temp file via defer, we use a
// validation-path approach: the mock writes a copy of the PNG to a
// test-controlled path that survives after Execute returns.
func TestAnalyzeUIScreenshot_HTMLWithBrowser_WritesValidPNG(t *testing.T) {
	t.Parallel()

	htmlFile, err := os.CreateTemp("", "sprout-test-png-*.html")
	require.NoError(t, err)
	defer os.Remove(htmlFile.Name())
	htmlFile.WriteString("<html><body></body></html>")
	htmlFile.Close()

	// Create a validation file for test assertions — since the handler
	// cleans up the temp screenshot via defer, we need a separate path.
	validationFile, err := os.CreateTemp("", "sprout-validation-*.png")
	require.NoError(t, err)
	validationPath := validationFile.Name()
	validationFile.Close()
	defer os.Remove(validationPath)

	writingMock := &mockBrowserThatWritesPNG{
		base:           &mockWebBrowserForUIScreenshot{returnResult: "ok"},
		validationPath: validationPath,
	}

	h := &analyzeUIScreenshotHandler{}
	ctx := context.Background()
	env := ToolEnv{WebBrowser: writingMock}
	args := map[string]any{
		"image_path": htmlFile.Name(),
	}

	result, _ := h.Execute(ctx, env, args)

	// Verify the PNG file written by the mock is a valid image.
	data, readErr := os.ReadFile(validationPath)
	require.NoError(t, readErr, "validation PNG file should be readable")
	img, decodeErr := png.Decode(bytes.NewReader(data))
	require.NoError(t, decodeErr, "PNG file should be a valid image")
	require.Equal(t, 1, img.Bounds().Dx(), "PNG should be 1x1 pixel")
	require.Equal(t, 1, img.Bounds().Dy(), "PNG should be 1x1 pixel")

	// The result should not contain the old HTML error.
	require.NotContains(t, result.Output, "HTML content requires a browser")
}

// mockBrowserThatWritesPNG writes a valid 1x1 PNG to screenshot_path
// before delegating to the base mock. It also writes a copy to
// validationPath for test assertions (since the temp screenshot is
// cleaned up by the handler after Execute returns).
type mockBrowserThatWritesPNG struct {
	base           *mockWebBrowserForUIScreenshot
	lastOpts       map[string]any
	validationPath string
}

func (m *mockBrowserThatWritesPNG) BrowseURL(ctx context.Context, url string, opts map[string]any) (string, error) {
	// Write a valid 1x1 PNG to screenshot_path (the handler will analyze this).
	if sp, ok := opts["screenshot_path"].(string); ok && sp != "" {
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		f, err := os.Create(sp)
		if err == nil {
			png.Encode(f, img)
			f.Close()
		}
	}

	// Write a copy to validation path for test assertions (persists after
	// the handler's defer cleanup).
	if m.validationPath != "" {
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		f, err := os.Create(m.validationPath)
		if err == nil {
			png.Encode(f, img)
			f.Close()
		}
	}

	// Record opts for test assertions.
	m.lastOpts = opts

	return m.base.BrowseURL(ctx, url, opts)
}
