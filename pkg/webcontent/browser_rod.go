//go:build browser

package webcontent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// renderTimeout is the maximum time allowed for a single RenderPage call
// (navigation + JS execution + HTML extraction).
const renderTimeout = 30 * time.Second

// stableDuration is how long WaitStable watches for network/DOM quiescence.
const stableDuration = 500 * time.Millisecond

// rodRenderer implements BrowserRenderer using go-rod and Chromium.
type rodRenderer struct {
	browser *rod.Browser

	mu     sync.Mutex
	closed bool
}

// Compile-time interface satisfaction check.
var _ BrowserRenderer = (*rodRenderer)(nil)

// NewBrowserRenderer returns a BrowserRenderer backed by go-rod.
// The browser is launched lazily on the first call to RenderPage.
func NewBrowserRenderer() BrowserRenderer {
	return &rodRenderer{}
}

// connect launches Chromium (if not already connected) and returns the browser.
// It is safe to call from multiple goroutines.
func (r *rodRenderer) connect(ctx context.Context) (*rod.Browser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, fmt.Errorf("browser renderer has been closed")
	}
	if r.browser != nil {
		return r.browser, nil
	}

	u, err := launcher.New().Headless(true).NoSandbox(true).Context(ctx).Launch()
	if err != nil {
		return nil, fmt.Errorf("browser launch: %w", err)
	}
	r.browser = rod.New().ControlURL(u)
	if err := r.browser.Connect(); err != nil {
		return nil, fmt.Errorf("browser connect: %w", err)
	}
	return r.browser, nil
}

// RenderPage navigates to url, waits for JS to execute, and returns the rendered HTML.
func (r *rodRenderer) RenderPage(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	browser, err := r.connect(ctx)
	if err != nil {
		return "", fmt.Errorf("browser connect: %w", err)
	}

	// Use an incognito context so cookies/state don't leak between renders.
	incognito, err := browser.Incognito()
	if err != nil {
		return "", fmt.Errorf("incognito context: %w", err)
	}

	page, err := incognito.Page(proto.TargetCreateTarget{})
	if err != nil {
		return "", fmt.Errorf("open page: %w", err)
	}

	// Always close the page when done.
	defer func() {
		_ = page.Close()
		_ = incognito.Close()
	}()

	if err := page.Navigate(url); err != nil {
		return "", fmt.Errorf("navigate to %s: %w", url, err)
	}

	// WaitStable waits for load, request idle, and DOM stability.
	if err := page.WaitStable(stableDuration); err != nil {
		return "", fmt.Errorf("wait stable: %w", err)
	}

	html, err := page.HTML()
	if err != nil {
		return "", fmt.Errorf("get HTML: %w", err)
	}

	return html, nil
}

// Close shuts down the browser process.
func (r *rodRenderer) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}
	r.closed = true

	if r.browser != nil {
		_ = r.browser.Close()
	}
}
