//go:build js

package tools

// registerDesignCritiqueTools is a WASM stub. design_critique depends on the
// host browser tier (rod/Chromium rasterization of the rendered target) and on
// the vision tier (the critique itself) — neither exists in the browser
// sandbox. The tool is therefore not advertised to the model in WASM builds.
//
// design_assets and design_validate are pure Go and keep WASM variants; the
// browser/vision-dependent design tools (design_render, design_import_sketch,
// design_critique) do not (SP-140 invariant 7, SP-140-4 §4a).
//
// Mirrors design_render_handler_js.go / design_import_sketch_handler_js.go.
func registerDesignCritiqueTools() []ToolHandler {
	return nil
}
