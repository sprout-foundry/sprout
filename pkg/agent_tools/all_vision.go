//go:build !js

package tools

// registerVisionTools registers the vision analysis tools. In native
// (desktop/daemon) builds these are advertised to the model and backed by
// the VisionProcessor pipeline (pkg/agent_tools/vision_*.go).
//
// Image and PDF reading is read_file's job (it attaches pixels inline for a
// vision-capable primary and extracts text otherwise), so there is no
// separate image-analysis tool.
//
// Excluded from WASM builds via all_vision_js.go, which returns nil.
// Mirrors the all_codegraph_wasm.go pattern.
//
// SP-112-6.
func registerVisionTools() []ToolHandler {
	return []ToolHandler{
		&analyzeUIScreenshotHandler{},
	}
}
