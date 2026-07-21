//go:build js

package tools

// registerRunAutomateTool is a WASM stub. run_automate requires an
// agent integration (tools.RunAutomateFunc is set by pkg/agent at
// startup), and WASM builds don't run pkg/agent — they don't have
// an *Agent instance to capture. The tool is not advertised to the
// model in WASM mode rather than being advertised as a tool that
// always returns "not available".
//
// SP-112-8.
func registerRunAutomateTool() []ToolHandler {
	return nil
}
