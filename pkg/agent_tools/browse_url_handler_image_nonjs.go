//go:build !js

package tools

import (
	"context"
	"os"
)

// loadScreenshotForMultimodal reads a screenshot file and returns it as an
// ImageData ready for inline multimodal attachment.
func loadScreenshotForMultimodal(ctx context.Context, env ToolEnv, path string) (ImageData, error) {
	base64Data, mimeType, err := env.VisionProcessor.GetImageData(ctx, path)
	if err != nil {
		return ImageData{}, err
	}
	return ImageData{Base64: base64Data, MIMEType: mimeType}, nil
}

// attachScreenshotIfRequested is the nonjs path: when the user asked for a
// screenshot and the file exists, try to attach it as inline multimodal
// content so the model sees it directly without a separate
// analyze_image_content call. Best-effort: never fails the tool.
func attachScreenshotIfRequested(ctx context.Context, env ToolEnv, args map[string]any, result *ToolResult) {
	action, ok := args["action"].(string)
	if !ok || action != "screenshot" {
		return
	}
	sp, ok := args["screenshot_path"].(string)
	if !ok || sp == "" {
		return
	}
	if _, statErr := os.Stat(sp); statErr != nil {
		return
	}
	if env.VisionProcessor == nil {
		return
	}
	img, err := loadScreenshotForMultimodal(ctx, env, sp)
	if err != nil {
		return
	}
	result.Images = []ImageData{img}
}
