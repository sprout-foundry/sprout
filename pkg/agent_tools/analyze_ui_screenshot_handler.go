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
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// visionModeFrontend is the analysis mode used by the analyze_ui_screenshot tool.
const visionModeFrontend = "frontend"

type analyzeUIScreenshotHandler struct{}

func (h *analyzeUIScreenshotHandler) Name() string { return "analyze_ui_screenshot" }

func (h *analyzeUIScreenshotHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "analyze_ui_screenshot",
		Description: "Analyze UI screenshots, mockups, or live HTML pages for implementation feedback. Accepts images, URLs, and local HTML files (auto-rendered via browser).",
		Required:    []string{"image_path"},
		Parameters: []ParameterDef{
			{Name: "image_path", Type: "string", Required: true, Description: "Path or URL to screenshot or HTML file"},
			{Name: "analysis_prompt", Type: "string", Description: "Custom vision prompt for analysis"},
			{Name: "viewport_width", Type: "integer", Description: "Browser width in px for HTML files (default 1280)"},
			{Name: "viewport_height", Type: "integer", Description: "Browser height in px for HTML files (default 720)"},
		},
	}
}

func (h *analyzeUIScreenshotHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "image_path")
	return err
}

func (h *analyzeUIScreenshotHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	imagePath, err := extractString(args, "image_path")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// Gate 1 precheck — local filesystem paths only. http(s) URLs are
	// fetched over the network and skip the classifier. Local paths are
	// gated on the raw path here, before any file:// URL conversion.
	if !isHTTPURL(imagePath) {
		resolvedPath, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "analyze_ui_screenshot", imagePath)
		if decision == "deny" {
			return ToolResult{Output: fmt.Sprintf("read blocked: %s is not accessible from this session", imagePath), IsError: true},
				fmt.Errorf("read blocked: %s is not accessible", imagePath)
		}
		if decision == "prompt" && env.FileAccessPrompter != nil {
			if ctx2, approved := promptForOffWorkspacePath(ctx, env, "analyze_ui_screenshot", imagePath, resolvedPath, "read"); approved {
				ctx = ctx2
			} else {
				return ToolResult{Output: fmt.Sprintf("read blocked: off-workspace access to %s was not approved", imagePath), IsError: true},
					fmt.Errorf("read blocked: off-workspace access to %s was not approved", imagePath)
			}
		}
	}

	analysisPrompt := ""
	if v, ok := args["analysis_prompt"].(string); ok {
		analysisPrompt = v
	}

	// Detect HTML content — render via browser then analyze the screenshot.
	if IsHTMLInput(imagePath) {
		if env.WebBrowser == nil {
			return ToolResult{
				Output:  "HTML content requires a browser for rendering, but no browser is available in this environment. Please provide a screenshot image file instead.",
				IsError: true,
			}, agenterrors.NewTool("analyze_ui_screenshot", "html content requires browser rendering but no browser is available", errNoBrowser)
		}

		// Build the URL to browse.
		var fileURL string
		if strings.HasPrefix(imagePath, "http://") || strings.HasPrefix(imagePath, "https://") {
			fileURL = imagePath
		} else {
			absPath, absErr := filepath.Abs(imagePath)
			if absErr != nil {
				msg := fmt.Sprintf("failed to resolve HTML path: %v", absErr)
				return ToolResult{Output: msg, IsError: true},
					agenterrors.NewTool("analyze_ui_screenshot", msg, absErr)
			}
			// filepath.ToSlash normalizes Windows backslashes so url.URL
			// produces a well-formed file:// URL (forward slashes, no %5C
			// escapes). On POSIX this is a no-op.
			fileURL = (&url.URL{Scheme: "file", Path: filepath.ToSlash(absPath)}).String()
		}

		// Read viewport dimensions (default 1280x720).
		// The tool framework may pass these as int (direct call) or float64
		// (JSON deserialization) — handle both.
		viewportWidth := viewportDim(args, "viewport_width", 1280)
		viewportHeight := viewportDim(args, "viewport_height", 720)

		// Create a temporary file for the screenshot, then close it so
		// the browser can write to it.
		tmpFile, tmpErr := os.CreateTemp("", "sprout-html-render-*.png")
		if tmpErr != nil {
			msg := fmt.Sprintf("failed to create temp screenshot file: %v", tmpErr)
			return ToolResult{Output: msg, IsError: true},
				agenterrors.NewTool("analyze_ui_screenshot", msg, tmpErr)
		}
		tmpPath := tmpFile.Name()
		tmpFile.Close()

		// Render the HTML page and capture a screenshot.
		opts := map[string]any{
			"action":          "screenshot",
			"screenshot_path": tmpPath,
			"viewport_width":  viewportWidth,
			"viewport_height": viewportHeight,
			"allow_file_url":  true,
		}
		_, browseErr := env.WebBrowser.BrowseURL(ctx, fileURL, opts)
		if browseErr != nil {
			os.Remove(tmpPath) // best-effort cleanup
			msg := fmt.Sprintf("browser rendering failed: %v", browseErr)
			return ToolResult{Output: msg, IsError: true},
				agenterrors.NewTool("analyze_ui_screenshot", msg, browseErr)
		}

		defer os.Remove(tmpPath)

		result, analyzeErr := AnalyzeImage(ctx, tmpPath, analysisPrompt, visionModeFrontend)
		if analyzeErr != nil {
			return ToolResult{Output: result, IsError: true}, analyzeErr
		}

		return ToolResult{Output: result}, nil
	}

	result, err := AnalyzeImage(ctx, imagePath, analysisPrompt, visionModeFrontend)
	if err != nil {
		return ToolResult{Output: result, IsError: true}, err
	}

	return ToolResult{Output: result}, nil
}

func (h *analyzeUIScreenshotHandler) Aliases() []string      { return nil }
func (h *analyzeUIScreenshotHandler) Timeout() time.Duration { return 0 }
func (h *analyzeUIScreenshotHandler) MaxResultSize() int     { return 0 }
func (h *analyzeUIScreenshotHandler) SafeForParallel() bool  { return false }
func (h *analyzeUIScreenshotHandler) Interactive() bool      { return false }

// errNoBrowser is the underlying cause returned when HTML input is received
// but no browser backend is wired into ToolEnv.
var errNoBrowser = errors.New("browser not available in this environment")

// viewportDim extracts an integer viewport dimension from tool args, handling
// both int (direct calls) and float64 (JSON deserialization) representations.
// Falls back to def when the key is missing or non-positive.
func viewportDim(args map[string]any, key string, def float64) float64 {
	switch v := args[key].(type) {
	case int:
		if v > 0 {
			return float64(v)
		}
	case float64:
		if v > 0 {
			return v
		}
	}
	return def
}
