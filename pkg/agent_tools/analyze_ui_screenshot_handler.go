//go:build !js

package tools

import (
	"context"
	"fmt"
	"time"
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

	// Detect renderable content — render via the shared browser helper,
	// then analyze the resulting screenshot.
	//
	// RenderModeAuto keeps this tool's historical semantics: only HTML
	// (http(s) URLs serving text/html, local .html/.htm) renders. SVG does
	// NOT auto-render here — it falls through to AnalyzeImage's raw
	// image/svg+xml branch, which is the desired behavior for this tool.
	// Callers that know their input is a renderable source (design_render)
	// pass RenderModeBrowser explicitly.
	renderOpts := RenderInputOptions{
		Mode:                     RenderModeAuto,
		ViewportWidth:            viewportDim(args, "viewport_width", defaultViewportWidth),
		ViewportHeight:           viewportDim(args, "viewport_height", defaultViewportHeight),
		ReportBrowserUnavailable: true,
	}

	result, rendered, err := renderInputToString(ctx, env, "analyze_ui_screenshot", imagePath, renderOpts,
		func(analyzeCtx context.Context, pngPath string) (string, error) {
			return AnalyzeImage(analyzeCtx, pngPath, analysisPrompt, visionModeFrontend)
		})
	if rendered {
		if err != nil {
			return ToolResult{Output: result, IsError: true}, err
		}
		return ToolResult{Output: result}, nil
	}

	result, err = AnalyzeImage(ctx, imagePath, analysisPrompt, visionModeFrontend)
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
