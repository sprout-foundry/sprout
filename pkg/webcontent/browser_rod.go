//go:build browser

package webcontent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
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

const browserInstrumentationScript = `
(() => {
  if (window.__leditBrowserCaptureInstalled) return;
  window.__leditBrowserCaptureInstalled = true;
  window.__leditBrowserCapture = { console: [], errors: [] };
  const limitPush = (list, value) => {
    list.push(value);
    if (list.length > 100) list.shift();
  };
  const stringify = (value) => {
    try {
      if (typeof value === 'string') return value;
      return JSON.stringify(value);
    } catch (_err) {
      return String(value);
    }
  };
  for (const level of ['log', 'info', 'warn', 'error']) {
    const original = console[level];
    console[level] = function (...args) {
      try {
        limitPush(window.__leditBrowserCapture.console, '[' + level + '] ' + args.map(stringify).join(' '));
      } catch (_err) {}
      return original.apply(this, args);
    };
  }
  window.addEventListener('error', (event) => {
    try {
      const location = event.filename ? ' @ ' + event.filename + ':' + event.lineno + ':' + event.colno : '';
      limitPush(window.__leditBrowserCapture.errors, String(event.message || 'error') + location);
    } catch (_err) {}
  });
  window.addEventListener('unhandledrejection', (event) => {
    try {
      limitPush(window.__leditBrowserCapture.errors, 'Unhandled rejection: ' + stringify(event.reason));
    } catch (_err) {}
  });
})();
`

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

func (r *rodRenderer) Run(ctx context.Context, url string, opts BrowseOptions) (*BrowseResult, error) {
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	incognito, page, err := r.openIncognitoPage(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = page.Close()
		_ = incognito.Close()
	}()

	if err := applyViewportAndUA(page, opts.ViewportWidth, opts.ViewportHeight, opts.UserAgent); err != nil {
		return nil, err
	}

	var removeInstrumentation func() error
	if opts.IncludeConsole {
		removeInstrumentation, err = page.EvalOnNewDocument(browserInstrumentationScript)
		if err != nil {
			return nil, fmt.Errorf("install browser instrumentation: %w", err)
		}
		defer func() {
			if removeInstrumentation != nil {
				_ = removeInstrumentation()
			}
		}()
	}

	if err := page.Navigate(url); err != nil {
		return nil, fmt.Errorf("navigate to %s: %w", url, err)
	}
	if err := page.WaitStable(stableDuration); err != nil {
		return nil, fmt.Errorf("wait stable: %w", err)
	}

	if err := waitForSelectorIfNeeded(page, opts.WaitForSelector, opts.WaitTimeoutMs); err != nil {
		return nil, err
	}

	result := &BrowseResult{}

	for _, step := range opts.Steps {
		if err := executeBrowseStep(page, step, opts.WaitTimeoutMs, result); err != nil {
			return nil, err
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
			return nil, err
		}
		result.ScreenshotPath = opts.ScreenshotPath
	}

	if len(opts.CaptureSelectors) > 0 {
		captures, err := captureSelectors(page, opts.CaptureSelectors, opts.ResponseMaxChars)
		if err != nil {
			return nil, err
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

	if opts.IncludeConsole {
		consoleMessages, pageErrors, err := captureBrowserDiagnostics(page)
		if err != nil {
			return nil, err
		}
		result.ConsoleMessages = truncateStringSlice(consoleMessages, 40, textLimit(opts.ResponseMaxChars))
		result.PageErrors = truncateStringSlice(pageErrors, 40, textLimit(opts.ResponseMaxChars))
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
			return nil, err
		}
		result.Cookies = cookies
	}
	if opts.CaptureStorage {
		localStorage, err := captureStorageMap(page, `() => Object.fromEntries(Object.entries(localStorage))`)
		if err != nil {
			return nil, err
		}
		sessionStorage, err := captureStorageMap(page, `() => Object.fromEntries(Object.entries(sessionStorage))`)
		if err != nil {
			return nil, err
		}
		result.LocalStorage = localStorage
		result.SessionStorage = sessionStorage
	}

	return result, nil
}

func waitForSelectorIfNeeded(page *rod.Page, selector string, timeoutMs int) error {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return nil
	}
	timeout := defaultWaitTimeout
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs) * time.Millisecond
	}
	if _, err := page.Timeout(timeout).Element(selector); err != nil {
		return fmt.Errorf("wait for selector %q: %w", selector, err)
	}
	return nil
}

func executeBrowseStep(page *rod.Page, step BrowseStep, timeoutMs int, result *BrowseResult) error {
	action := strings.ToLower(strings.TrimSpace(step.Action))
	timeout := defaultWaitTimeout
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs) * time.Millisecond
	}

	record := func(description string) {
		if result != nil {
			result.Actions = append(result.Actions, description)
		}
	}

	switch action {
	case "wait_for":
		if strings.TrimSpace(step.Selector) == "" {
			return fmt.Errorf("browse step wait_for requires selector")
		}
		if _, err := page.Timeout(timeout).Element(step.Selector); err != nil {
			return fmt.Errorf("wait_for %q: %w", step.Selector, err)
		}
		record(fmt.Sprintf("wait_for %s", step.Selector))
		return nil
	case "click":
		el, err := requireElement(page, step.Selector, timeout)
		if err != nil {
			return err
		}
		if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return fmt.Errorf("click %q: %w", step.Selector, err)
		}
		_ = page.WaitStable(stableDuration)
		record(fmt.Sprintf("click %s", step.Selector))
		return nil
	case "hover":
		el, err := requireElement(page, step.Selector, timeout)
		if err != nil {
			return err
		}
		if err := el.Hover(); err != nil {
			return fmt.Errorf("hover %q: %w", step.Selector, err)
		}
		record(fmt.Sprintf("hover %s", step.Selector))
		return nil
	case "type":
		el, err := requireElement(page, step.Selector, timeout)
		if err != nil {
			return err
		}
		if err := el.Input(step.Value); err != nil {
			return fmt.Errorf("type into %q: %w", step.Selector, err)
		}
		_ = page.WaitStable(stableDuration)
		record(fmt.Sprintf("type %s", step.Selector))
		return nil
	case "fill":
		el, err := requireElement(page, step.Selector, timeout)
		if err != nil {
			return err
		}
		if _, err := el.Eval(`value => {
			this.focus();
			this.value = value;
			this.dispatchEvent(new Event('input', { bubbles: true }));
			this.dispatchEvent(new Event('change', { bubbles: true }));
			return true;
		}`, step.Value); err != nil {
			return fmt.Errorf("fill %q: %w", step.Selector, err)
		}
		_ = page.WaitStable(stableDuration)
		record(fmt.Sprintf("fill %s", step.Selector))
		return nil
	case "press":
		if strings.TrimSpace(step.Key) == "" {
			return fmt.Errorf("browse step press requires key")
		}
		if strings.TrimSpace(step.Selector) != "" {
			el, err := requireElement(page, step.Selector, timeout)
			if err != nil {
				return err
			}
			if _, err := el.Eval(`() => { this.focus(); return true; }`); err != nil {
				return fmt.Errorf("focus %q before keypress: %w", step.Selector, err)
			}
		}
		if err := pressPageKey(page, step.Key); err != nil {
			return err
		}
		_ = page.WaitStable(stableDuration)
		record(fmt.Sprintf("press %s", step.Key))
		return nil
	case "sleep":
		delay := step.Millis
		if delay <= 0 {
			delay = 250
		}
		select {
		case <-time.After(time.Duration(delay) * time.Millisecond):
			record(fmt.Sprintf("sleep %dms", delay))
			return nil
		case <-page.GetContext().Done():
			return page.GetContext().Err()
		}
	case "scroll_to":
		if strings.TrimSpace(step.Selector) != "" {
			el, err := requireElement(page, step.Selector, timeout)
			if err != nil {
				return err
			}
			if _, err := el.Eval(`() => { this.scrollIntoView({ block: 'center', inline: 'nearest' }); return true; }`); err != nil {
				return fmt.Errorf("scroll_to %q: %w", step.Selector, err)
			}
			record(fmt.Sprintf("scroll_to %s", step.Selector))
			return nil
		}
		if _, err := page.Eval(`y => { window.scrollTo({ top: y, behavior: 'instant' }); return true; }`, step.Millis); err != nil {
			return fmt.Errorf("scroll_to y=%d: %w", step.Millis, err)
		}
		record(fmt.Sprintf("scroll_to %d", step.Millis))
		return nil
	case "navigate":
		target := strings.TrimSpace(step.Value)
		if target == "" {
			return fmt.Errorf("browse step navigate requires value URL")
		}
		if err := page.Navigate(target); err != nil {
			return fmt.Errorf("navigate to %q: %w", target, err)
		}
		if err := page.WaitStable(stableDuration); err != nil {
			return fmt.Errorf("wait stable after navigate to %q: %w", target, err)
		}
		record(fmt.Sprintf("navigate %s", target))
		return nil
	case "reload":
		if err := page.Reload(); err != nil {
			return fmt.Errorf("reload page: %w", err)
		}
		if err := page.WaitStable(stableDuration); err != nil {
			return fmt.Errorf("wait stable after reload: %w", err)
		}
		record("reload")
		return nil
	case "back":
		if err := page.NavigateBack(); err != nil {
			return fmt.Errorf("navigate back: %w", err)
		}
		if err := page.WaitStable(stableDuration); err != nil {
			return fmt.Errorf("wait stable after back: %w", err)
		}
		record("back")
		return nil
	case "forward":
		if err := page.NavigateForward(); err != nil {
			return fmt.Errorf("navigate forward: %w", err)
		}
		if err := page.WaitStable(stableDuration); err != nil {
			return fmt.Errorf("wait stable after forward: %w", err)
		}
		record("forward")
		return nil
	case "assert_selector":
		el, err := requireElement(page, step.Selector, timeout)
		if err != nil {
			return err
		}
		if expect := strings.TrimSpace(step.Expect); expect != "" {
			text, _ := el.Text()
			html, _ := el.HTML()
			if !strings.Contains(text, expect) && !strings.Contains(html, expect) {
				return fmt.Errorf("assert_selector %q missing expected text %q", step.Selector, expect)
			}
		}
		record(fmt.Sprintf("assert_selector %s", step.Selector))
		return nil
	case "wait_for_text":
		expected := strings.TrimSpace(step.Expect)
		if expected == "" {
			expected = strings.TrimSpace(step.Value)
		}
		if expected == "" {
			return fmt.Errorf("browse step wait_for_text requires expect or value")
		}
		if strings.TrimSpace(step.Selector) != "" {
			el, err := requireElement(page, step.Selector, timeout)
			if err != nil {
				return err
			}
			if err := el.Wait(rod.Eval(`expected => (this.innerText || this.textContent || '').includes(expected)`, expected)); err != nil {
				return fmt.Errorf("wait_for_text on %q expecting %q: %w", step.Selector, expected, err)
			}
		} else {
			if err := page.Timeout(timeout).Wait(rod.Eval(`expected => (document.body && document.body.innerText || '').includes(expected)`, expected)); err != nil {
				return fmt.Errorf("wait_for_text expecting %q: %w", expected, err)
			}
		}
		record(fmt.Sprintf("wait_for_text %s", expected))
		return nil
	case "eval":
		if strings.TrimSpace(step.Script) == "" {
			return fmt.Errorf("browse step eval requires script")
		}
		value, err := evalToJSONString(page, step.Script)
		evalResult := EvalResult{Script: step.Script}
		if err != nil {
			evalResult.Error = err.Error()
		} else {
			evalResult.Value = value
		}
		if result != nil {
			result.EvalResults = append(result.EvalResults, evalResult)
		}
		if err != nil {
			return fmt.Errorf("eval step failed: %w", err)
		}
		record("eval")
		return nil
	default:
		return fmt.Errorf("unknown browse step action: %s", step.Action)
	}
}

func requireElement(page *rod.Page, selector string, timeout time.Duration) (*rod.Element, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return nil, fmt.Errorf("selector is required")
	}
	el, err := page.Timeout(timeout).Element(selector)
	if err != nil {
		return nil, fmt.Errorf("find selector %q: %w", selector, err)
	}
	return el, nil
}

func pressPageKey(page *rod.Page, raw string) error {
	key, err := lookupInputKey(raw)
	if err != nil {
		return err
	}
	if err := page.Keyboard.Press(key); err != nil {
		return fmt.Errorf("press key %q: %w", raw, err)
	}
	if err := page.Keyboard.Release(key); err != nil {
		return fmt.Errorf("release key %q: %w", raw, err)
	}
	return nil
}

func lookupInputKey(raw string) (input.Key, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "enter", "return":
		return input.Enter, nil
	case "escape", "esc":
		return input.Escape, nil
	case "tab":
		return input.Tab, nil
	case "space", "spacebar":
		return input.Space, nil
	case "arrowleft", "left":
		return input.ArrowLeft, nil
	case "arrowright", "right":
		return input.ArrowRight, nil
	case "arrowup", "up":
		return input.ArrowUp, nil
	case "arrowdown", "down":
		return input.ArrowDown, nil
	case "backspace":
		return input.Backspace, nil
	case "delete":
		return input.Delete, nil
	}
	if len(raw) == 1 {
		return input.Key([]rune(raw)[0]), nil
	}
	return 0, fmt.Errorf("unsupported key %q", raw)
}

func captureSelectors(page *rod.Page, selectors []string, responseMaxChars int) ([]SelectorCapture, error) {
	captures := make([]SelectorCapture, 0, len(selectors))
	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		if selector == "" {
			continue
		}
		elements, err := page.Elements(selector)
		if err != nil {
			return nil, fmt.Errorf("capture selector %q: %w", selector, err)
		}
		capture := SelectorCapture{
			Selector: selector,
			Found:    len(elements) > 0,
			Count:    len(elements),
		}
		if len(elements) > 0 {
			first := elements[0]
			text, _ := first.Text()
			html, _ := first.HTML()
			value, _ := first.Attribute("value")
			capture.Text = truncateForBrowseResult(text, textLimit(responseMaxChars))
			capture.HTML = truncateForBrowseResult(html, domLimit(responseMaxChars))
			if value != nil {
				capture.Value = truncateForBrowseResult(*value, textLimit(responseMaxChars))
			}
			capture.Attributes = make(map[string]string)
			for _, attr := range []string{"id", "class", "name", "role", "href", "aria-label"} {
				v, _ := first.Attribute(attr)
				if v != nil && *v != "" {
					capture.Attributes[attr] = truncateForBrowseResult(*v, 256)
				}
			}
			if len(capture.Attributes) == 0 {
				capture.Attributes = nil
			}
		}
		captures = append(captures, capture)
	}
	return captures, nil
}

func captureStorageMap(page *rod.Page, script string) (map[string]string, error) {
	res, err := page.Eval(script)
	if err != nil {
		return nil, fmt.Errorf("capture storage map: %w", err)
	}
	if res == nil || res.Value.Nil() {
		return nil, nil
	}
	raw := []byte(res.Value.JSON("", ""))
	var parsed map[string]string
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode storage map: %w", err)
	}
	if len(parsed) == 0 {
		return nil, nil
	}
	return parsed, nil
}

func captureBrowserDiagnostics(page *rod.Page) ([]string, []string, error) {
	res, err := page.Eval(`() => window.__leditBrowserCapture || { console: [], errors: [] }`)
	if err != nil {
		return nil, nil, fmt.Errorf("capture browser diagnostics: %w", err)
	}
	payload := struct {
		Console []string `json:"console"`
		Errors  []string `json:"errors"`
	}{}
	if err := json.Unmarshal([]byte(res.Value.JSON("", "")), &payload); err != nil {
		return nil, nil, fmt.Errorf("decode browser diagnostics: %w", err)
	}
	return payload.Console, payload.Errors, nil
}

func evalToJSONString(page *rod.Page, script string) (string, error) {
	res, err := page.Eval(script)
	if err != nil {
		return "", err
	}
	return res.Value.JSON("", "  "), nil
}

func truncateForBrowseResult(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(value[:limit]) + "... (truncated)"
}

func truncateStringSlice(values []string, maxItems int, itemLimit int) []string {
	if len(values) > maxItems {
		values = values[len(values)-maxItems:]
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, truncateForBrowseResult(value, itemLimit))
	}
	return out
}

func textLimit(responseMaxChars int) int {
	if responseMaxChars > 0 {
		return responseMaxChars
	}
	return 4000
}

func domLimit(responseMaxChars int) int {
	if responseMaxChars > 0 {
		return responseMaxChars
	}
	return 12000
}

func (r *rodRenderer) captureCurrentPageScreenshot(page *rod.Page, outputPath string) error {
	imgData, err := page.Screenshot(true, &proto.PageCaptureScreenshot{
		Format: proto.PageCaptureScreenshotFormatPng,
	})
	if err != nil {
		return fmt.Errorf("screenshot current page: %w", err)
	}
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
