//go:build browser

package webcontent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

const defaultWaitTimeout = 10 * time.Second

// localhostTimeout is the navigation timeout for localhost URLs (fast fail).
const localhostTimeout = 10 * time.Second

// remoteTimeout is the navigation timeout for all other URLs.
const remoteTimeout = 30 * time.Second

// getNavigationTimeout returns the appropriate navigation timeout for the given URL.
// Localhost URLs get a shorter timeout (10s) to fail fast when nothing is listening.
// Remote URLs get a longer timeout (30s) to accommodate slower network conditions.
func getNavigationTimeout(url string) time.Duration {
	lowerURL := strings.ToLower(url)
	// Match localhost-style URLs: with optional port, path, query, fragment.
	if strings.HasPrefix(lowerURL, "http://localhost") ||
		strings.HasPrefix(lowerURL, "http://127.0.0.1") ||
		strings.HasPrefix(lowerURL, "http://[::1]") ||
		strings.HasPrefix(lowerURL, "https://localhost") ||
		strings.HasPrefix(lowerURL, "https://127.0.0.1") ||
		strings.HasPrefix(lowerURL, "https://[::1]") {
		return localhostTimeout
	}
	return remoteTimeout
}

// rodRenderer implements BrowserRenderer using go-rod and Chromium.
type rodRenderer struct {
	browser *rod.Browser

	mu       sync.Mutex
	closed   bool
	sessions map[string]*browserSession
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

	// Attempt launch with retry: first try with GPU enabled; if the GPU probe
	// (a tiny screenshot) hangs, relaunch with --disable-gpu. This way machines
	// with a real GPU keep acceleration, while environments like WSL2 or
	// Docker containers without GPU support fall back gracefully.
	disableGPU := gpuProbeFailed()
	for range 2 {
		browser, needsFallback, err := r.launchAndProbe(ctx, disableGPU)
		if err != nil {
			return nil, err
		}
		if !needsFallback {
			r.browser = browser
			return r.browser, nil
		}
		// GPU probe hung — close browser and retry with --disable-gpu.
		_ = browser.Close()
		markGPUProbe(false)
		disableGPU = true
	}

	return nil, fmt.Errorf("browser launch failed after GPU fallback")
}

// launchAndProbe launches a browser instance, optionally with --disable-gpu,
// and probes whether screenshot capture works. Returns the browser, whether
// a fallback to --disable-gpu is needed, and any error.
func (r *rodRenderer) launchAndProbe(ctx context.Context, disableGPU bool) (*rod.Browser, bool, error) {
	l := launcher.New().Headless(true).NoSandbox(true).Context(ctx)
	if disableGPU {
		l.Set("disable-gpu")
	}

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
		return nil, false, fmt.Errorf("browser launch: %w", err)
	}
	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		return nil, false, fmt.Errorf("browser connect: %w", err)
	}

	// If GPU support hasn't been probed yet, test whether screenshots work.
	// In environments without a functional GPU (WSL2, some Docker containers),
	// PageCaptureScreenshot hangs indefinitely. A quick probe lets us detect
	// this and fall back to --disable-gpu.
	if !disableGPU && !gpuProbeDone() {
		if probeGPUSupport(ctx, browser) {
			markGPUProbe(true)
		} else {
			return browser, true, nil // needs fallback
		}
	}

	return browser, false, nil
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
		return "", fmt.Errorf("failed to open incognito page: %w", err)
	}
	defer func() {
		_ = page.Close()
		_ = incognito.Close()
	}()

	if err := page.Timeout(getNavigationTimeout(url)).Navigate(url); err != nil {
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
		return fmt.Errorf("screenshot: open incognito page: %w", err)
	}
	defer func() {
		_ = page.Close()
		_ = incognito.Close()
	}()

	if err := applyViewportAndUA(page, viewportWidth, viewportHeight, userAgent); err != nil {
		return fmt.Errorf("screenshot: apply viewport and UA: %w", err)
	}

	if err := page.Timeout(getNavigationTimeout(url)).Navigate(url); err != nil {
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
		return "", fmt.Errorf("failed to open incognito page: %w", err)
	}
	defer func() {
		_ = page.Close()
		_ = incognito.Close()
	}()

	if err := applyViewportAndUA(page, viewportWidth, viewportHeight, userAgent); err != nil {
		return "", fmt.Errorf("failed to apply viewport and user agent: %w", err)
	}

	if err := page.Timeout(getNavigationTimeout(url)).Navigate(url); err != nil {
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

// Run executes a full browser interaction session with optional steps, captures, and diagnostics.
func (r *rodRenderer) Run(ctx context.Context, url string, opts BrowseOptions) (*BrowseResult, error) {
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	persistentSession := opts.PersistSession || strings.TrimSpace(opts.SessionID) != "" || opts.CloseSession
	var (
		page      *rod.Page
		sessionID string
		cleanup   func()
	)
	if persistentSession {
		session, err := r.acquireSession(ctx, opts.SessionID)
		if err != nil {
			return nil, fmt.Errorf("acquire browser session: %w", err)
		}
		page = session.page
		sessionID = session.id
		cleanup = func() {
			session.lastUsed = time.Now()
			session.mu.Unlock()
			if opts.CloseSession {
				_ = r.closeSessionByID(session.id)
			}
		}
	} else {
		incognito, tempPage, err := r.openIncognitoPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("open browser page: %w", err)
		}
		page = tempPage
		cleanup = func() {
			_ = page.Close()
			_ = incognito.Close()
		}
	}
	defer cleanup()

	if err := applyViewportAndUA(page, opts.ViewportWidth, opts.ViewportHeight, opts.UserAgent); err != nil {
		return nil, fmt.Errorf("apply viewport settings: %w", err)
	}

	var removeInstrumentation func() error
	if opts.IncludeConsole || opts.CaptureNetwork {
		hook, err := page.EvalOnNewDocument(browserInstrumentationScript)
		if err != nil {
			return nil, fmt.Errorf("install browser instrumentation: %w", err)
		}
		removeInstrumentation = hook
		defer func() {
			if removeInstrumentation != nil {
				_ = removeInstrumentation()
			}
		}()
	}

	if err := page.Timeout(getNavigationTimeout(url)).Navigate(url); err != nil {
		return nil, fmt.Errorf("navigate to %s: %w", url, err)
	}
	if err := page.WaitStable(stableDuration); err != nil {
		return nil, fmt.Errorf("wait stable: %w", err)
	}

	if err := waitForSelectorIfNeeded(page, opts.WaitForSelector, opts.WaitTimeoutMs); err != nil {
		return nil, fmt.Errorf("wait for selector: %w", err)
	}

	result := &BrowseResult{SessionID: sessionID}

	for i, step := range opts.Steps {
		if err := executeBrowseStep(page, step, opts.WaitTimeoutMs, result); err != nil {
			return nil, fmt.Errorf("step[%d] %s: %w", i, step.Action, err)
		}
	}

	info, err := page.Info()
	if err != nil {
		return nil, fmt.Errorf("page info: %w", err)
	}
	result.FinalURL = info.URL
	result.Title = info.Title
	if readyState, err := evalToJSONString(page, `() => document.readyState`); err == nil {
		result.ReadyState = strings.Trim(readyState, "\"")
	}

	if opts.ScreenshotPath != "" {
		if err := r.captureCurrentPageScreenshot(page, opts.ScreenshotPath); err != nil {
			return nil, fmt.Errorf("capture screenshot: %w", err)
		}
		result.ScreenshotPath = opts.ScreenshotPath
	}

	if len(opts.CaptureSelectors) > 0 {
		captures, err := captureSelectors(page, opts.CaptureSelectors, opts.ResponseMaxChars)
		if err != nil {
			return nil, fmt.Errorf("capture selectors: %w", err)
		}
		result.SelectorCaptures = captures
	}

	if opts.CaptureDOM {
		html, err := page.HTML()
		if err != nil {
			return nil, fmt.Errorf("get HTML: %w", err)
		}
		result.DOM = truncateForBrowseResult(html, domLimit(opts.ResponseMaxChars))
	}

	if opts.CaptureText {
		html, err := page.HTML()
		if err != nil {
			return nil, fmt.Errorf("get HTML for text extraction: %w", err)
		}
		result.VisibleText = truncateForBrowseResult(HTMLToText(html), textLimit(opts.ResponseMaxChars))
	}

	if opts.IncludeConsole || opts.CaptureNetwork {
		consoleMessages, pageErrors, networkRequests, err := captureBrowserDiagnostics(page)
		if err != nil {
			return nil, fmt.Errorf("capture browser diagnostics: %w", err)
		}
		if opts.IncludeConsole {
			result.ConsoleMessages = truncateStringSlice(consoleMessages, 40, textLimit(opts.ResponseMaxChars))
			result.PageErrors = truncateStringSlice(pageErrors, 40, textLimit(opts.ResponseMaxChars))
		}
		if opts.CaptureNetwork {
			result.NetworkRequests = truncateNetworkRequests(markCORSBlockedRequests(networkRequests), 50, textLimit(opts.ResponseMaxChars))
		}
		result.CORSIssues = truncateStringSlice(detectCORSIssues(consoleMessages, pageErrors, networkRequests), 20, textLimit(opts.ResponseMaxChars))
	}
	if opts.CaptureCookies {
		cookies, err := captureStorageMap(page, `() => {
			const value = document.cookie || '';
			const out = {};
			for (const pair of value.split(';')) {
				if (!pair.trim()) continue;
				const idx = pair.indexOf('=');
				const key = idx >= 0 ? pair.slice(0, idx).trim() : pair.trim();
				const val = idx >= 0 ? pair.slice(idx + 1).trim() : '';
				out[key] = val;
			}
			return out;
		}`)
		if err != nil {
			return nil, fmt.Errorf("capture cookies: %w", err)
		}
		result.Cookies = cookies
	}
	if opts.CaptureStorage {
		localStorage, err := captureStorageMap(page, `() => Object.fromEntries(Object.entries(localStorage))`)
		if err != nil {
			return nil, fmt.Errorf("capture localStorage: %w", err)
		}
		sessionStorage, err := captureStorageMap(page, `() => Object.fromEntries(Object.entries(sessionStorage))`)
		if err != nil {
			return nil, fmt.Errorf("capture sessionStorage: %w", err)
		}
		result.LocalStorage = localStorage
		result.SessionStorage = sessionStorage
	}

	return result, nil
}

// Close shuts down the browser process.
func (r *rodRenderer) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}
	r.closed = true

	for id, session := range r.sessions {
		session.mu.Lock()
		if session.page != nil {
			_ = session.page.Close()
		}
		if session.incognito != nil {
			_ = session.incognito.Close()
		}
		session.mu.Unlock()
		delete(r.sessions, id)
	}

	if r.browser != nil {
		_ = r.browser.Close()
	}
}
