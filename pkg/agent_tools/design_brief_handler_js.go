//go:build js

package tools

// registerDesignBriefTools is a WASM stub. design_brief is pure Go and would
// build on js, but SP-140 invariant 7 keeps only design_assets and design_validate
// on the WASM roster — the Phase 4/5 design tools (design_export_tokens,
// design_sync, and design_brief among them) are native-only. The tool is
// therefore not advertised to the model in WASM builds.
//
// Mirrors design_sync_handler_js.go and design_export_handler_js.go.
func registerDesignBriefTools() []ToolHandler {
	return nil
}
