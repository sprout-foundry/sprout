//go:build js

package tools

// registerVisionTools registers the browser-build vision tools. Image and
// PDF reading is read_file's job, and the WASM build has no VisionProcessor
// pipeline (it needs native image tooling), so read_file attaches the image
// for the primary model instead. analyze_ui_screenshot stays native-only
// until HTML can be rasterized in the page (SP-158 §158c).
func registerVisionTools() []ToolHandler {
	return nil
}
