//go:build js

package tools

// registerCheckAppTool is a WASM stub: check_app needs to spawn the
// project's build and server and drive a host-side browser, none of which the
// browser shell can do, so the tool is not advertised there (as with
// browse_url).
func registerCheckAppTool() []ToolHandler {
	return nil
}
