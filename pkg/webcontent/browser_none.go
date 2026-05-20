//go:build js

package webcontent

// NewBrowserRenderer returns a no-op renderer for WASM builds where a headless
// browser is not available. The returned renderer always returns an error.
func NewBrowserRenderer() BrowserRenderer {
	return nop
}
