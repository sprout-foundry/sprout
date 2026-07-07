//go:build js

package tools

import "context"

// attachScreenshotIfRequested is a WASM stub. The vision pipeline isn't
// available under GOOS=js/wasm (GetImageData lives in vision_image.go behind
// //go:build !js), so multimodal screenshot attachment is a no-op here. The
// model still receives the screenshot path and can fall back to
// analyze_image_content if needed.
func attachScreenshotIfRequested(_ context.Context, _ ToolEnv, _ map[string]any, _ *ToolResult) {
}
