//go:build browser

package webcontent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// defaultViewportWidth is the fallback viewport width when 0 is passed.
const defaultViewportWidth = 1280

// defaultViewportHeight is the fallback viewport height when 0 is passed.
const defaultViewportHeight = 720

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

	l := launcher.New().Headless(true).NoSandbox(true).Context(ctx)

	// Try common system browser paths before auto-download.
	// This allows running on systems with pre-installed Chromium/Chrome.
	for _, bin := range systemBrowserPaths() {
		if _, err := os.Stat(bin); err == nil {
			l = l.Bin(bin)
			break
		}
	}

	u, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("browser launch: %w", err)
	}
	r.browser = rod.New().ControlURL(u)
	if err := r.browser.Connect(); err != nil {
		return nil, fmt.Errorf("browser connect: %w", err)
	}
	return r.browser, nil
}

// systemBrowserPaths returns candidate paths for system-installed browsers.
func systemBrowserPaths() []string {
	return []string{
		"/usr/bin/chromium-browser",
		"/usr/bin/chromium",
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/snap/bin/chromium",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
		"C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe",
	}
}

// openIncognitoPage creates an incognito browser context and opens a new page.
// The caller MUST defer close on both the incognito browser and the page.
func (r *rodRenderer) openIncognitoPage(ctx context.Context) (*rod.Browser, *rod.Page, error) {
	browser, err := r.connect(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("browser connect: %w", err)
	}

	incognito, err := browser.Incognito()
	if err != nil {
		return nil, nil, fmt.Errorf("incognito context: %w", err)
	}

	page, err := incognito.Page(proto.TargetCreateTarget{})
	if err != nil {
		_ = incognito.Close()
		return nil, nil, fmt.Errorf("open page: %w", err)
	}

	return incognito, page, nil
}

// applyViewportAndUA sets the viewport dimensions and user-agent on a page,
// falling back to defaults when zero values are provided.
func applyViewportAndUA(page *rod.Page, vw, vh int, ua string) error {
	if vw == 0 {
		vw = defaultViewportWidth
	}
	if vh == 0 {
		vh = defaultViewportHeight
	}

	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:  vw,
		Height: vh,
	}); err != nil {
		return fmt.Errorf("set viewport: %w", err)
	}

	if ua != "" {
		if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: ua}); err != nil {
			return fmt.Errorf("set user-agent: %w", err)
		}
	}

	return nil
}

// RenderPage navigates to url, waits for JS to execute, and returns the rendered HTML.
func (r *rodRenderer) RenderPage(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	incognito, page, err := r.openIncognitoPage(ctx)
	if err != nil {
		return "", err
	}
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

// Screenshot captures a screenshot of the given URL and writes it to outputPath.
// viewportWidth and viewportHeight set the browser viewport dimensions (0 = use defaults 1280x720).
// userAgent overrides the browser user-agent string ("" = use default).
func (r *rodRenderer) Screenshot(ctx context.Context, url string, outputPath string, viewportWidth, viewportHeight int, userAgent string) error {
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	incognito, page, err := r.openIncognitoPage(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = page.Close()
		_ = incognito.Close()
	}()

	if err := applyViewportAndUA(page, viewportWidth, viewportHeight, userAgent); err != nil {
		return err
	}

	if err := page.Navigate(url); err != nil {
		return fmt.Errorf("navigate to %s: %w", url, err)
	}

	if err := page.WaitStable(stableDuration); err != nil {
		return fmt.Errorf("wait stable: %w", err)
	}

	// Capture a full-page screenshot as PNG.
	imgData, err := page.Screenshot(true, &proto.PageCaptureScreenshot{
		Format: proto.PageCaptureScreenshotFormatPng,
	})
	if err != nil {
		return fmt.Errorf("screenshot %s: %w", url, err)
	}

	// Ensure the parent directory exists.
	if dir := filepath.Dir(outputPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create screenshot directory %s: %w", dir, err)
		}
	}

	if err := os.WriteFile(outputPath, imgData, 0o644); err != nil {
		return fmt.Errorf("write screenshot to %s: %w", outputPath, err)
	}

	return nil
}

// CaptureDOM returns the rendered HTML of the page after JavaScript execution.
// viewportWidth and viewportHeight set the browser viewport dimensions (0 = use defaults 1280x720).
// userAgent overrides the browser user-agent string ("" = use default).
func (r *rodRenderer) CaptureDOM(ctx context.Context, url string, viewportWidth, viewportHeight int, userAgent string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	incognito, page, err := r.openIncognitoPage(ctx)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = page.Close()
		_ = incognito.Close()
	}()

	if err := applyViewportAndUA(page, viewportWidth, viewportHeight, userAgent); err != nil {
		return "", err
	}

	if err := page.Navigate(url); err != nil {
		return "", fmt.Errorf("navigate to %s: %w", url, err)
	}

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
