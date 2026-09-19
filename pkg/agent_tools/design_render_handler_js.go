//go:build js

package tools

// registerDesignRenderTools is a WASM stub. design_render depends on the
// host browser tier (rod/Chromium rasterization) and, for critique, the
// vision tier — neither exists in the browser sandbox. The tool is therefore
// not advertised to the model in WASM builds.
//
// design_assets and design_validate are pure Go and keep WASM variants; the
// browser/vision-dependent design tools (design_render, design_import_sketch,
// design_critique) do not (SP-140 invariant 7, SP-140-2 §2c).
//
// Mirrors all_vision_js.go.
func registerDesignRenderTools() []ToolHandler {
	return nil
}
