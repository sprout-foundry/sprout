package tools

import (
	"context"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type analyzeImageContentHandler struct{}

func (h *analyzeImageContentHandler) Name() string { return "analyze_image_content" }

func (h *analyzeImageContentHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "analyze_image_content",
		Description: "Analyze images/PDFs for text/code extraction or general insights. Supports local file paths and remote HTTP(S) URLs.",
		Required:    []string{"image_path"},
		Parameters: []ParameterDef{
			{Name: "image_path", Type: "string", Required: true, Description: "Path or URL to an image or PDF to analyze (local path or HTTP(S) URL)"},
			{Name: "analysis_prompt", Type: "string", Description: "Optional custom vision prompt"},
			{Name: "analysis_mode", Type: "string", Description: "Optional analysis mode override"},
		},
	}
}

func (h *analyzeImageContentHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "image_path")
	return err
}

func (h *analyzeImageContentHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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

	analysisMode := ""
	if v, ok := args["analysis_mode"].(string); ok {
		analysisMode = v
	}

	result, err := AnalyzeImage(ctx, imagePath, analysisPrompt, analysisMode)
	if err != nil {
		return ToolResult{Output: result, IsError: true}, err
	}

	succeeded = true
	return ToolResult{Output: result}, nil
}
