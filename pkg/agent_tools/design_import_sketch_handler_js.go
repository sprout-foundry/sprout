//go:build js

package tools

// registerDesignImportSketchTools is a WASM stub. design_import_sketch depends
// on the vision tier to turn the attached sketch image into extraction text,
// and the vision pipeline needs CGo/SQLite/native HTTP clients the browser
// sandbox cannot provide (see all_vision_js.go). The tool is therefore not
// advertised to the model in WASM builds.
//
// design_assets and design_validate are pure Go and keep WASM variants; the
// browser/vision-dependent design tools (design_render, design_import_sketch,
// design_critique) do not (SP-140 invariant 7, SP-140-2 §2c).
//
// Mirrors design_render_handler_js.go / all_vision_js.go.
func registerDesignImportSketchTools() []ToolHandler {
	return nil
}
