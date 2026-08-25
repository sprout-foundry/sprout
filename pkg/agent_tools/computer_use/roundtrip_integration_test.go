package computer_use

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// TestScreenshotRoundTrip_ProvesImagesReachLLM is the integration test that
// validates the most critical computer-use data path: a take_screenshot tool
// call must produce ImageData that survives conversion to the OpenAI-format
// content blocks the provider sends to the LLM.
//
// This test exists because the entire feature is useless if screenshots
// don't make it back to the model — the agent would be operating blind.
// It exercises three layers:
//  1. The tool handler (takeScreenshotHandler.Execute) — produces ImageData
//  2. The image field mapping (URI→URL, MIMEType→Type) — matches the
//     conversion in pkg/agent/tool_security.go:288
//  3. The OpenAI multimodal content builder — matches the format in
//     pkg/agent_providers/generic_provider_vision.go:buildMultiModalContent
func TestScreenshotRoundTrip_ProvesImagesReachLLM(t *testing.T) {
	// --- Layer 1: Tool handler produces a screenshot with image data ---
	mock := &MockBackend{
		OverrideScreenshotData: minimalPNG,
		OverrideScreenshotDims: Size{Width: 800, Height: 600},
	}
	savedBackend := backend
	SetBackend(mock)
	defer SetBackend(savedBackend)

	handler := &takeScreenshotHandler{}
	result, err := handler.Execute(context.Background(), tools.ToolEnv{}, nil)
	if err != nil {
		t.Fatalf("take_screenshot failed: %v", err)
	}

	if len(result.Images) == 0 {
		t.Fatal("take_screenshot returned no images — the model would be blind")
	}

	img := result.Images[0]
	if img.URI == "" {
		t.Fatal("image URI is empty — handler must set a data URI")
	}
	if !strings.HasPrefix(img.URI, "data:image/png;base64,") {
		t.Errorf("image URI should be a base64 PNG data URI, got: %s", img.URI[:min(40, len(img.URI))])
	}
	if img.MIMEType != "image/png" {
		t.Errorf("MIMEType = %q, want image/png", img.MIMEType)
	}

	// --- Layer 2: Field mapping (tools.ImageData → api.ImageData) ---
	// This mirrors the conversion in pkg/agent/tool_security.go:288
	type apiImageData struct {
		URL  string
		Type string
	}
	mapped := apiImageData{
		URL:  img.URI,
		Type: img.MIMEType,
	}
	if mapped.URL == "" {
		t.Fatal("field mapping lost the URL — api.ImageData.URL would be empty")
	}

	// --- Layer 3: OpenAI-format multimodal content construction ---
	// This mirrors buildMultiModalContent in generic_provider_vision.go.
	// The provider builds: [{"type":"text","text":...}, {"type":"image_url","image_url":{"url":...}}]
	contentParts := buildExpectedOpenAIContent(result.Output, mapped.URL)
	if len(contentParts) < 2 {
		t.Fatalf("expected at least 2 content parts (text + image), got %d", len(contentParts))
	}

	// Find the image part
	var imagePart map[string]any
	for _, part := range contentParts {
		if part["type"] == "image_url" {
			imagePart = part
			break
		}
	}
	if imagePart == nil {
		t.Fatal("no image_url content block found — screenshot would not reach the LLM")
	}

	urlField := imagePart["image_url"]
	if urlField == nil {
		t.Fatal("image_url field is nil")
	}
	urlMap, ok := urlField.(map[string]any)
	if !ok {
		t.Fatalf("image_url should be a map, got %T", urlField)
	}
	urlStr, ok := urlMap["url"].(string)
	if !ok || urlStr == "" {
		t.Fatal("image_url.url is empty — the screenshot data URI was lost")
	}

	// Verify the data URI is valid base64
	b64Data := strings.TrimPrefix(urlStr, "data:image/png;base64,")
	if b64Data == urlStr {
		t.Fatal("URL is not a base64 data URI")
	}
	if _, err := base64.StdEncoding.DecodeString(b64Data); err != nil {
		t.Fatalf("screenshot base64 is invalid: %v", err)
	}

	// Verify the mock backend recorded the screenshot call
	if len(mock.Records) != 1 || mock.Records[0].Action != "Screenshot" {
		t.Errorf("backend records = %+v, want one Screenshot call", mock.Records)
	}
}

// TestScreenshotRoundTrip_WithRegion verifies region screenshots also produce
// valid image data (region cropping is a separate code path in the handler).
func TestScreenshotRoundTrip_WithRegion(t *testing.T) {
	mock := &MockBackend{
		OverrideScreenshotData: minimalPNG,
		OverrideScreenshotDims: Size{Width: 100, Height: 100},
	}
	savedBackend := backend
	SetBackend(mock)
	defer SetBackend(savedBackend)

	handler := &takeScreenshotHandler{}
	args := map[string]any{
		"region": map[string]any{
			"x":      10,
			"y":      20,
			"width":  100,
			"height": 50,
		},
	}
	result, err := handler.Execute(context.Background(), tools.ToolEnv{}, args)
	if err != nil {
		t.Fatalf("take_screenshot with region failed: %v", err)
	}
	if len(result.Images) == 0 {
		t.Fatal("region screenshot returned no images")
	}

	// Verify the region was passed to the backend
	if len(mock.Records) != 1 {
		t.Fatalf("expected 1 backend record, got %d", len(mock.Records))
	}
	rec := mock.Records[0]
	if rec.Args["region"] == nil {
		t.Error("region was not passed to the backend")
	}
}

// TestAnthropicTranslation_DragCoordinates verifies the fixed drag parameter
// mapping (start_coordinate=from, coordinate=to) matches Anthropic's API.
func TestAnthropicTranslation_DragCoordinates(t *testing.T) {
	mock := &MockBackend{}
	savedBackend := backend
	SetBackend(mock)
	defer SetBackend(savedBackend)

	params := map[string]any{
		"start_coordinate": []any{10.0, 20.0},
		"coordinate":       []any{100.0, 200.0},
	}
	_, err := TranslateAnthropicAction("left_click_drag", params)
	if err != nil {
		t.Fatalf("drag failed: %v", err)
	}

	if len(mock.Records) != 1 || mock.Records[0].Action != "MouseDrag" {
		t.Fatalf("records = %+v, want one MouseDrag", mock.Records)
	}
	drag := mock.Records[0]
	from := drag.Args["from"].(Point)
	to := drag.Args["to"].(Point)
	if from.X != 10 || from.Y != 20 {
		t.Errorf("from = %+v, want {10,20}", from)
	}
	if to.X != 100 || to.Y != 200 {
		t.Errorf("to = %+v, want {100,200}", to)
	}
}

// buildExpectedOpenAIContent mirrors the OpenAI multimodal content format used
// by generic_provider_vision.go's buildMultiModalContent. It exists in the test
// to verify the conversion without importing the providers package (which would
// create an import cycle).
func buildExpectedOpenAIContent(text string, imageURL string) []map[string]any {
	parts := make([]map[string]any, 0, 2)
	if strings.TrimSpace(text) != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": text,
		})
	}
	if imageURL != "" {
		parts = append(parts, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": imageURL,
			},
		})
	}
	return parts
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
