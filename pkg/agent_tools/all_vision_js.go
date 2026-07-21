//go:build js

package tools

// registerVisionTools is a WASM stub. Vision tools are not available in
// browser builds — the VisionProcessor pipeline depends on CGo/SQLite/
// native HTTP clients that the browser sandbox cannot provide. The
// vision_stubs_js.go file provides stub helpers for callers that import
// them at the function level, but the tools themselves are not advertised
// to the model in WASM mode.
//
// SP-112-6.
func registerVisionTools() []ToolHandler {
	return nil
}
