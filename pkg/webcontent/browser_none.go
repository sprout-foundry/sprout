//go:build js

package webcontent

// NewBrowserRenderer returns the WASM renderer. The browser build has no
// headless browser of its own and no longer has a host-page rasterizer, so
// workspace-file screenshots are unavailable here.
func NewBrowserRenderer() BrowserRenderer {
	return &nopRenderer{}
}
