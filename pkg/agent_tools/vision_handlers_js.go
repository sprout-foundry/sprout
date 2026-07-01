//go:build js

package tools

import "context"

// analyzeImageContentHandler — WASM stub.
type analyzeImageContentHandler struct{}

func (h *analyzeImageContentHandler) Name() string { return "analyze_image_content" }

func (h *analyzeImageContentHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "analyze_image_content",
		Description: "Analyze images/PDFs for text/code extraction or general insights. Supports local file paths and remote HTTP(S) URLs.",
		Required:    []string{"image_path"},
		Parameters: []ParameterDef{
			{Name: "image_path", Type: "string", Required: true, Description: "Path or URL to an image or PDF to analyze (local path or HTTP(S) URL)"},
			{Name: "analysis_prompt", Type: "string", Required: false, Description: "Optional custom vision prompt"},
			{Name: "analysis_mode", Type: "string", Required: false, Description: "Optional analysis mode override"},
		},
	}
}

func (h *analyzeImageContentHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "image_path")
	return err
}

func (h *analyzeImageContentHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	return ToolResult{Output: "vision analysis is not available in WASM mode", IsError: true}, nil
}

// analyzeUIScreenshotHandler — WASM stub.
type analyzeUIScreenshotHandler struct{}

func (h *analyzeUIScreenshotHandler) Name() string { return "analyze_ui_screenshot" }

func (h *analyzeUIScreenshotHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "analyze_ui_screenshot",
		Description: "Analyze UI screenshots, mockups, or live HTML pages for implementation feedback. Accepts image files (PNG/JPG/WebP), remote image URLs, and local HTML files which are automatically rendered via a headless browser before analysis. Ideal for quick visual testing of dev builds and design reviews.",
		Required:    []string{"image_path"},
		Parameters: []ParameterDef{
			{Name: "image_path", Type: "string", Required: true, Description: "Path or URL to the UI screenshot or HTML file"},
			{Name: "analysis_prompt", Type: "string", Required: false, Description: "Optional custom vision prompt for analysis"},
			{Name: "viewport_width", Type: "integer", Required: false, Description: "Browser viewport width in pixels for HTML files (default: 1280)"},
			{Name: "viewport_height", Type: "integer", Required: false, Description: "Browser viewport height in pixels for HTML files (default: 720)"},
		},
	}
}

func (h *analyzeUIScreenshotHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "image_path")
	return err
}

func (h *analyzeUIScreenshotHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	return ToolResult{Output: "UI screenshot analysis is not available in WASM mode", IsError: true}, nil
}
