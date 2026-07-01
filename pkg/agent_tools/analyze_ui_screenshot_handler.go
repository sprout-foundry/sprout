//go:build !js

package tools

import (
	"context"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// visionModeFrontend is the analysis mode used by the analyze_ui_screenshot tool.
const visionModeFrontend = "frontend"

type analyzeUIScreenshotHandler struct{}

func (h *analyzeUIScreenshotHandler) Name() string { return "analyze_ui_screenshot" }

func (h *analyzeUIScreenshotHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "analyze_ui_screenshot",
		Description: "Analyze UI screenshots, mockups, or live HTML pages for implementation feedback. Accepts image files (PNG/JPG/WebP), remote image URLs, and local HTML files which are automatically rendered via a headless browser before analysis. Ideal for quick visual testing of dev builds and design reviews.",
		Required:    []string{"image_path"},
		Parameters: []ParameterDef{
			{Name: "image_path", Type: "string", Required: true, Description: "Path or URL to the UI screenshot or HTML file"},
			{Name: "analysis_prompt", Type: "string", Description: "Optional custom vision prompt for analysis"},
			{Name: "viewport_width", Type: "integer", Description: "Browser viewport width in pixels for HTML files (default: 1280)"},
			{Name: "viewport_height", Type: "integer", Description: "Browser viewport height in pixels for HTML files (default: 720)"},
		},
	}
}

func (h *analyzeUIScreenshotHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "image_path")
	return err
}

func (h *analyzeUIScreenshotHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	var succeeded bool
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": !succeeded,
			})
		}()
	}

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

	succeeded = true
	return ToolResult{Output: result}, nil
}
