package tools

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type analyzeUIScreenshotHandler struct{}

func (h *analyzeUIScreenshotHandler) Name() string { return "analyze_ui_screenshot" }

func (h *analyzeUIScreenshotHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "analyze_ui_screenshot",
		Description: "Analyze UI screenshots, mockups, or live HTML pages for implementation feedback. Accepts image files (PNG/JPG/WebP), remote image URLs, and local HTML files which are automatically rendered via a headless browser before analysis. Ideal for quick visual testing of dev builds and design reviews.",
		Required: []string{"image_path"},
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
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": true,
			})
		}()
	}

	// TODO: Full implementation requires *Agent access for GetVisionProcessor()
	// and vision model integration for screenshot analysis. This is a thin wrapper stub.
	return ToolResult{
		Output:  "analyze_ui_screenshot requires full *Agent refactoring for complete functionality. This handler cannot analyze UI screenshots without access to the Agent's vision processor. Please use the legacy interface or complete the migration.",
		IsError: true,
	}, fmt.Errorf("analyze_ui_screenshot requires full *Agent refactoring")
}
