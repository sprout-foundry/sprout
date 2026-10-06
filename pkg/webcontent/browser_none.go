//go:build js

package webcontent

// NewBrowserRenderer returns the WASM renderer: workspace-file screenshots go
// to the host page (see browser_page_js.go); everything else returns an error.
func NewBrowserRenderer() BrowserRenderer {
	return &pageRenderer{}
}
