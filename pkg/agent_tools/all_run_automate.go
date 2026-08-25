//go:build !js

package tools

// registerRunAutomateTool registers the run_automate tool, which starts
// an automated workflow from the project's automate/ directory as a
// background process. The handler is a function-pointer bridge — pkg/agent
// sets tools.RunAutomateFunc at startup, capturing the *Agent reference
// in a closure so the handler doesn't need direct *Agent access.
//
// Excluded from WASM builds via all_run_automate_js.go, which returns
// nil. WASM has no agent integration; the function pointer is never
// set, and the tool's Execute() would always return "not available".
// Mirrors the all_vision_js.go pattern (SP-112-6) and all_browse_url_wasm.go.
//
// SP-112-8.
func registerRunAutomateTool() []ToolHandler {
	return []ToolHandler{
		&runAutomateHandler{},
	}
}
