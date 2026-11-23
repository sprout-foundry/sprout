//go:build !js

package webcontent

import "sync"

// The app check drives a server on this machine, so its browser must keep
// loopback off any configured proxy (a proxy answering for 127.0.0.1 turns
// every page into a proxy error). It gets a renderer of its own rather than
// the shared global one, held for the process lifetime like the global
// renderer: constructing one per check would start a fresh Chromium each run
// and leak it, since nothing closes a renderer a caller was handed.
var (
	directRenderingOnce sync.Once
	directRendering     BrowserRenderer
)

// GetDirectBrowser returns the process-lifetime renderer that never uses a
// proxy. Chromium launches on the first session, so getting it is cheap.
func GetDirectBrowser() BrowserRenderer {
	directRenderingOnce.Do(func() {
		directRendering = NewDirectBrowserRenderer()
	})
	return directRendering
}

// CloseDirectBrowser closes the proxy-free renderer, releasing Chromium
// resources. It is for graceful shutdown, as with the global browser: a closed
// renderer stays closed, so a later GetDirectBrowser returns a renderer that
// refuses every session. It resets the singleton anyway, so a caller that
// closes early gets a working renderer rather than a poisoned one.
func CloseDirectBrowser() {
	if directRendering == nil {
		return
	}
	directRendering.Close()
	directRendering = nil
	directRenderingOnce = sync.Once{}
}
