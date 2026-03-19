//go:build !browser

package webcontent

// NewBrowserRenderer returns a no-op renderer when the browser build tag
// is not set. The returned renderer always returns an error from RenderPage.
func NewBrowserRenderer() BrowserRenderer {
	return nop
}
