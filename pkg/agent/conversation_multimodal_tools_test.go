package agent

import (
	"context"
	"sync"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestGetOptimizedToolDefinitions_KeepsVisionToolsForDirectMultimodalImages(t *testing.T) {
	// Vision models keep access to analyze_image_content and analyze_ui_screenshot tools
	// even when direct multimodal images are present. This allows the agent to use tools
	// for additional context such as URLs, file paths, or specialized analysis modes.
	agent := &Agent{
		client: &visionSupportingClient{supportsVision: true},
		messages: []api.Message{
			{
				Role:    "user",
				Content: "[image: pasted.png]\nWhat is in this menu? Also check the screenshot at /tmp/screenshot.png",
				Images: []api.ImageData{
					{Base64: "ZmFrZQ==", Type: "image/png"},
				},
			},
		},
	}

	tools := agent.getOptimizedToolDefinitions(agent.messages)

	// Verify that vision tools are still available even with direct multimodal images
	foundAnalyzeImageContent := false
	foundAnalyzeUIScreenshot := false
	for _, tool := range tools {
		if tool.Function.Name == "analyze_image_content" {
			foundAnalyzeImageContent = true
		}
		if tool.Function.Name == "analyze_ui_screenshot" {
			foundAnalyzeUIScreenshot = true
		}
	}

	if !foundAnalyzeImageContent {
		t.Fatal("expected analyze_image_content to remain available even with direct multimodal images")
	}
	if !foundAnalyzeUIScreenshot {
		t.Fatal("expected analyze_ui_screenshot to remain available even with direct multimodal images")
	}
}

func TestGetOptimizedToolDefinitions_KeepsVisionToolsWithoutAttachedImages(t *testing.T) {
	agent := &Agent{
		client: &visionSupportingClient{supportsVision: true},
		messages: []api.Message{
			{
				Role:    "user",
				Content: "Analyze https://example.com/menu.png",
			},
		},
	}

	tools := agent.getOptimizedToolDefinitions(agent.messages)
	foundAnalyzeImageContent := false
	for _, tool := range tools {
		if tool.Function.Name == "analyze_image_content" {
			foundAnalyzeImageContent = true
			break
		}
	}
	if !foundAnalyzeImageContent {
		t.Fatal("expected analyze_image_content to remain available when there are no attached images")
	}
}

func TestShouldRequireOCRBeforeCompletion_FalseForDirectMultimodalImages(t *testing.T) {
	agent := &Agent{
		client:           &visionSupportingClient{supportsVision: true},
		streamingEnabled: true,
		interruptCtx:     context.Background(),
		outputMutex:      &sync.Mutex{},
		messages: []api.Message{
			{
				Role:    "user",
				Content: "OCR Trigger Policy (MANDATORY): use analyze_image_content for menu images/PDFs.",
				Images: []api.ImageData{
					{Base64: "ZmFrZQ==", Type: "image/png"},
				},
			},
			{
				Role: "assistant",
				ToolCalls: []api.ToolCall{
					{
						ID:   "fetch_1",
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      "fetch_url",
							Arguments: `{"url":"https://example.com/menu"}`,
						},
					},
				},
			},
			{Role: "tool", ToolCallId: "fetch_1", Content: "Menu page includes Image 4: https://cdn.example.com/menu.jpg"},
		},
	}
	ch := NewConversationHandler(agent)

	if ch.shouldRequireOCRBeforeCompletion() {
		t.Fatal("expected OCR requirement to be skipped for direct multimodal image reasoning")
	}
}
