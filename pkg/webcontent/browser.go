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

	// Screenshot captures a screenshot of the given URL and writes it to outputPath.
	// viewportWidth and viewportHeight set the browser viewport dimensions (0 = use defaults 1280x720).
	// userAgent overrides the browser user-agent string ("" = use default).
	Screenshot(ctx context.Context, url string, outputPath string, viewportWidth, viewportHeight int, userAgent string) error

	// CaptureDOM returns the rendered HTML of the page (similar to RenderPage but specifically
	// for capturing the DOM state after JS execution). Use this when you need the full HTML
	// rather than text-extracted content.
	CaptureDOM(ctx context.Context, url string, viewportWidth, viewportHeight int, userAgent string) (string, error)

	// Close releases any resources held by the renderer (browsers, pages, etc.)
	Close()
}

// BrowseOptions configures browser-based URL browsing.
type BrowseOptions struct {
	// Ctx carries a context for cancellation/deadlines; if nil, context.Background() is used.
	Ctx context.Context
	// ViewportWidth sets the browser viewport width in pixels (0 = default 1280)
	ViewportWidth int
	// ViewportHeight sets the browser viewport height in pixels (0 = default 720)
	ViewportHeight int
	// UserAgent overrides the browser user-agent string
	UserAgent string
	// Action determines what to do: "screenshot", "dom", or "text" (default: "text")
	Action string
	// ScreenshotPath is the file path for screenshot output (required for action="screenshot")
	ScreenshotPath string
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

func (n *nopRenderer) Screenshot(_ context.Context, _ string, _ string, _, _ int, _ string) error {
	return fmt.Errorf("browser rendering not available")
}

func (n *nopRenderer) CaptureDOM(_ context.Context, _ string, _, _ int, _ string) (string, error) {
	return "", fmt.Errorf("browser rendering not available")
}

func (n *nopRenderer) Close() {}
