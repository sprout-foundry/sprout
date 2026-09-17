//go:build js

package tools

// registerDesignExportTools is a WASM stub. design_export_tokens is pure Go and
// would build on js, but SP-140 invariant 7 keeps only design_assets and
// design_validate on the WASM roster — the Phase 4/5 design writers
// (design_export_tokens among them) are native-only. The tool is therefore not
// advertised to the model in WASM builds.
//
// Mirrors design_render_handler_js.go and all_vision_js.go.
func registerDesignExportTools() []ToolHandler {
	return nil
}
