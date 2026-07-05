//go:build !js

package tools

import (
	"context"
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

	analysisPrompt := ""
	if v, ok := args["analysis_prompt"].(string); ok {
		analysisPrompt = v
	}

	// TODO(SP-079-3): When browser support lands in ToolEnv, use viewportWidth/viewportHeight
	// to render HTML content before analysis. For now, only direct image paths are supported.

	// Detect HTML content — requires a browser to render, which is not yet
	// wired into ToolEnv (separate SP task).
	if IsHTMLInput(imagePath) {
		return ToolResult{
			Output:  "HTML content requires a browser for rendering. Please provide a screenshot image file instead.",
			IsError: true,
		}, agenterrors.NewTool("analyze_ui_screenshot", "html content requires browser rendering", nil)
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
