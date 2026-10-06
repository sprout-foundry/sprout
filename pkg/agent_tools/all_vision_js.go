//go:build js

package tools

// registerVisionTools registers the browser-build vision tools. There is no
// VisionProcessor pipeline here (it needs native image tooling), so
// analyze_image_content attaches the image for the primary model instead.
// analyze_ui_screenshot stays native-only until HTML can be rasterized in
// the page (SP-158 §158c).
func registerVisionTools() []ToolHandler {
	return []ToolHandler{&analyzeImageContentHandler{}}
}
