//go:build js

package tools

// registerBrowseURLTool is a WASM stub. browse_url requires a host-side
// headless browser (rod/Chromium) that is not available in the browser
// environment — the WASM shell cannot spawn Chromium and the host's
// browser_none.go returns a nopRenderer that always errors. Rather than
// advertise a tool that can never succeed, browse_url is not registered in
// WASM builds. (See SP-112 Tier 2.)
func registerBrowseURLTool() []ToolHandler {
	return nil
}
