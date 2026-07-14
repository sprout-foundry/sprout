//go:build !js

package tools

// registerBrowseURLTool registers the browse_url tool, which requires a
// host-side headless browser (rod/Chromium) to render pages and capture
// screenshots/DOM. In native (desktop/daemon) builds the browser renderer
// is available, so the tool is advertised to the model.
func registerBrowseURLTool() []ToolHandler {
	return []ToolHandler{
		&browseURLHandler{},
	}
}
