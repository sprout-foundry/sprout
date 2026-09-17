//go:build !js

package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// render_input.go — Shared browser-render helper.
//
// Extracted from analyze_ui_screenshot_handler.go (SP-140-2 §2c) so the
// design_render tool can rasterize SVG/HTML/mermaid sources to PNG through
// the same code path instead of duplicating it. The helper is the single
// place that:
//
//  1. decides whether an input should be rendered (RenderMode + detection),
//  2. converts a local path to a well-formed file:// URL,
//  3. renders via ToolEnv.WebBrowser with allow_file_url: true,
//  4. owns the temp screenshot file's lifetime.
//
// Image analysis on the resulting PNG stays with the caller — this helper
// only produces the artifact.

// errNoBrowser is the underlying cause returned when rendering is requested
// but no browser backend is wired into ToolEnv.
var errNoBrowser = errors.New("browser not available in this environment")

// renderInputToString runs the browser pipeline for inputPath and calls
// analyze on the written PNG, returning analyze's (result, error).
//
// It is the analyze_ui_screenshot use of the shared helper: "render, then
// analyze the artifact with the vision tier".
//
// Behaviour by mode:
//   - RenderModeAuto: renders when IsRenderableInput(inputPath) is true
//     (http(s) HTML URLs, local .html/.htm/.svg); otherwise returns
//     rendered=false and the caller handles the input directly.
//   - RenderModeBrowser: always renders (URL browsed as-is, local path
//     converted to file://).
//   - RenderModeImage: never renders.
//
// On a render decision, rendered is true. When rendering or analysis fails
// the returned error is the same typed tool error the caller should surface;
// analyze's partial text (if any) is returned in result. The temp PNG is
// always removed before returning.
func renderInputToString(
	ctx context.Context,
	env ToolEnv,
	toolName string,
	inputPath string,
	opts RenderInputOptions,
	analyze func(ctx context.Context, pngPath string) (string, error),
) (result string, rendered bool, err error) {
	if !shouldRenderInput(inputPath, opts) {
		return "", false, nil
	}

	pngPath, cleanup, renderErr := renderInputToPNG(ctx, env, toolName, inputPath, opts)
	if renderErr != nil {
		return renderFailureText(renderErr), true, renderErr
	}
	defer cleanup()

	if analyze == nil {
		msg := "render requested without an analyze callback"
		return msg, true, agenterrors.NewTool(toolName, msg, nil)
	}
	text, analyzeErr := analyze(ctx, pngPath)
	return text, true, analyzeErr
}

// renderFailureText extracts the human-readable message from a typed render
// error so callers can surface it in ToolResult.Output (the handler contract
// is that the same text appears in both Output and the returned Go error).
// The TypedError.String() form is "[CODE] message", so we unwrap to the
// underlying message where possible.
func renderFailureText(err error) string {
	if err == nil {
		return ""
	}
	var te *agenterrors.TypedError
	if errors.As(err, &te) && te.Message != "" {
		return te.Message
	}
	return err.Error()
}

// RenderInputOptions parameterizes the shared render helper.
type RenderInputOptions struct {
	// Mode selects the render decision. Zero value (RenderModeAuto)
	// preserves analyze_ui_screenshot's historical auto-detection.
	Mode RenderMode
	// Forced render for RenderModeBrowser without a browser backend:
	// when true, a missing browser is a fatal error. Auto mode always
	// treats a missing browser as fatal once it has decided to render.
	ViewportWidth  float64
	ViewportHeight float64

	// ReportBrowserUnavailable controls the error when WebBrowser is nil.
	// When true (default for analyze_ui_screenshot) the caller gets the
	// user-facing "no browser is available" guidance message. When false
	// (design_render, which is browser-tier by construction) the error is
	// the terser "browser not available in this environment".
	ReportBrowserUnavailable bool
}

// shouldRenderInput applies the render-mode decision.
func shouldRenderInput(inputPath string, opts RenderInputOptions) bool {
	switch opts.Mode {
	case RenderModeImage:
		return false
	case RenderModeBrowser:
		return true
	default: // RenderModeAuto
		return IsRenderableInput(inputPath)
	}
}

// defaultViewportWidth/Height match the analyze_ui_screenshot defaults.
const (
	defaultViewportWidth  = 1280
	defaultViewportHeight = 720
)

// renderInputToPNG renders inputPath with the browser backend and returns
// the path of the written PNG plus a cleanup func that removes it.
//
// Local paths are converted to file:// URLs and browsed with
// allow_file_url: true (see browser_adapter.go's buildBrowseOptions);
// http(s) URLs are browsed as-is. The caller must invoke cleanup when done
// with the artifact, even when a later step fails.
//
// The returned path is written by the browser process, so it necessarily
// lives on the local filesystem — callers must not hand it to os.ReadFile
// or VFS helpers in remote/SSH workspace modes (SP-126 VFS boundary).
func renderInputToPNG(
	ctx context.Context,
	env ToolEnv,
	toolName string,
	inputPath string,
	opts RenderInputOptions,
) (string, func(), error) {
	noop := func() {}

	if env.WebBrowser == nil {
		if opts.ReportBrowserUnavailable {
			return "", noop, agenterrors.NewTool(toolName,
				"html content requires browser rendering but no browser is available", errNoBrowser)
		}
		return "", noop, agenterrors.NewTool(toolName, "browser not available in this environment", errNoBrowser)
	}

	// Build the URL to browse.
	var fileURL string
	if isHTTPURL(inputPath) {
		fileURL = inputPath
	} else {
		absPath, absErr := filepath.Abs(inputPath)
		if absErr != nil {
			msg := fmt.Sprintf("failed to resolve render source path: %v", absErr)
			return "", noop, agenterrors.NewTool(toolName, msg, absErr)
		}
		// filepath.ToSlash normalizes Windows backslashes so url.URL
		// produces a well-formed file:// URL (forward slashes, no %5C
		// escapes). On POSIX this is a no-op.
		fileURL = (&url.URL{Scheme: "file", Path: filepath.ToSlash(absPath)}).String()
	}

	viewportWidth := opts.ViewportWidth
	if viewportWidth <= 0 {
		viewportWidth = defaultViewportWidth
	}
	viewportHeight := opts.ViewportHeight
	if viewportHeight <= 0 {
		viewportHeight = defaultViewportHeight
	}

	// Create a temporary file for the screenshot, then close it so
	// the browser can write to it.
	tmpFile, tmpErr := os.CreateTemp("", "sprout-render-*.png")
	if tmpErr != nil {
		msg := fmt.Sprintf("failed to create temp screenshot file: %v", tmpErr)
		return "", noop, agenterrors.NewTool(toolName, msg, tmpErr)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	cleanup := func() { os.Remove(tmpPath) } // best-effort

	browseOpts := map[string]any{
		"action":          "screenshot",
		"screenshot_path": tmpPath,
		"viewport_width":  viewportWidth,
		"viewport_height": viewportHeight,
		"allow_file_url":  true,
	}
	if _, browseErr := env.WebBrowser.BrowseURL(ctx, fileURL, browseOpts); browseErr != nil {
		cleanup()
		msg := fmt.Sprintf("browser rendering failed: %v", browseErr)
		return "", noop, agenterrors.NewTool(toolName, msg, browseErr)
	}

	return tmpPath, cleanup, nil
}

// renderSourceFileURL converts inputPath to the URL the browser should
// browse. Exposed for tests and for callers that need the same file://
// normalization without rendering (e.g. diagnostics).
func renderSourceFileURL(inputPath string) (string, error) {
	if isHTTPURL(inputPath) {
		return inputPath, nil
	}
	absPath, err := filepath.Abs(inputPath)
	if err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absPath)}).String(), nil
}

// hasRenderableExtension reports whether a local path's extension is one the
// browser tier can rasterize. Kept internal; public detection goes through
// IsRenderableInput.
func hasRenderableExtension(path string) bool {
	if strings.HasPrefix(strings.ToLower(path), "http://") ||
		strings.HasPrefix(strings.ToLower(path), "https://") {
		return false
	}
	switch GetFileExtension(path) {
	case ".html", ".htm", ".svg":
		return true
	default:
		return false
	}
}
