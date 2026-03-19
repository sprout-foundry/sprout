package webcontent

import (
	"context"
	"fmt"
)

// BrowserRenderer renders HTML pages using a headless browser.
// Implementations may require external dependencies (e.g., rod/Chromium)
// and are loaded via build tags.
type BrowserRenderer interface {
	// RenderPage navigates to the given URL using a headless browser,
	// waits for JavaScript to execute, and returns the fully rendered HTML.
	RenderPage(ctx context.Context, url string) (string, error)

	// Close releases any resources held by the renderer (browsers, pages, etc.)
	Close()
}

// nopRenderer is a no-op implementation that always returns an error,
// used when no headless browser is available (i.e., without the browser build tag).
type nopRenderer struct{}

// Compile-time interface satisfaction check.
var _ BrowserRenderer = (*nopRenderer)(nil)

var nop = &nopRenderer{}

func (n *nopRenderer) RenderPage(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("browser rendering not available")
}

func (n *nopRenderer) Close() {}
