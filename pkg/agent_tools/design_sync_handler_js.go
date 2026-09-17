//go:build js

package tools

// registerDesignSyncTools is a WASM stub. design_sync is pure Go and would build
// on js, but SP-140 invariant 7 keeps only design_assets and design_validate on
// the WASM roster — the Phase 4/5 design tools (design_export_tokens and
// design_sync among them) are native-only. The tool is therefore not advertised
// to the model in WASM builds.
//
// Mirrors design_export_handler_js.go and design_render_handler_js.go.
func registerDesignSyncTools() []ToolHandler {
	return nil
}
