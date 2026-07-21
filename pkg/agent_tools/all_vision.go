//go:build !js

package tools

// registerVisionTools registers the vision analysis tools (image content
// analysis and UI screenshot analysis). In native (desktop/daemon) builds
// these are advertised to the model and backed by the VisionProcessor
// pipeline (pkg/agent_tools/vision_*.go).
//
// Excluded from WASM builds via all_vision_js.go, which returns nil.
// Mirrors the all_codegraph_wasm.go pattern.
//
// SP-112-6.
func registerVisionTools() []ToolHandler {
	return []ToolHandler{
		&analyzeImageContentHandler{},
		&analyzeUIScreenshotHandler{},
	}
}
