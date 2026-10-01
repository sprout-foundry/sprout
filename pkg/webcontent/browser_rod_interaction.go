//go:build !js

// browser_rod_interaction.go provides the Run() method and all step execution
// helpers for the go-rod based BrowserRenderer implementation.

package webcontent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

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

	if err := injectCookiesAndHeaders(page, url, opts); err != nil {
		return nil, fmt.Errorf("inject cookies/headers: %w", err)
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
		return browseStepWaitFor(page, step, timeout, record, result)
	case "click":
		return browseStepClick(page, step, timeout, record, result)
	case "hover":
		return browseStepHover(page, step, timeout, record, result)
	case "type":
		return browseStepType(page, step, timeout, record, result)
	case "fill":
		return browseStepFill(page, step, timeout, record, result)
	case "press":
		return browseStepPress(page, step, timeout, record, result)
	case "sleep":
		return browseStepSleep(page, step, timeout, record, result)
	case "scroll_to":
		return browseStepScrollTo(page, step, timeout, record, result)
	case "navigate":
		return browseStepNavigate(page, step, timeout, record, result)
	case "reload":
		return browseStepReload(page, step, timeout, record, result)
	case "back":
		return browseStepBack(page, step, timeout, record, result)
	case "forward":
		return browseStepForward(page, step, timeout, record, result)
	case "assert_selector":
		return browseStepAssertSelector(page, step, timeout, record, result)
	case "assert_text":
		return browseStepAssertText(page, step, timeout, record, result)
	case "assert_title":
		return browseStepAssertTitle(page, step, timeout, record, result)
	case "assert_url":
		return browseStepAssertURL(page, step, timeout, record, result)
	case "wait_for_text":
		return browseStepWaitForText(page, step, timeout, record, result)
	case "eval":
		return browseStepEval(page, step, timeout, record, result)
	case "wait_for_function":
		return browseStepWaitForFunction(page, step, timeout, record, result)
	case "screenshot_selector":
		return browseStepScreenshotSelector(page, step, timeout, record, result)
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
		return fmt.Errorf("lookup input key: %w", err)
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
			if state, err := first.Eval(`() => {
				const rect = this.getBoundingClientRect();
				const style = window.getComputedStyle(this);
				return {
					visible: !!(rect.width || rect.height || this.getClientRects().length) && style.visibility !== 'hidden' && style.display !== 'none',
					enabled: !this.disabled,
					box: { x: rect.x, y: rect.y, width: rect.width, height: rect.height }
				};
			}`); err == nil && state != nil {
				var parsed struct {
					Visible bool       `json:"visible"`
					Enabled bool       `json:"enabled"`
					Box     ElementBox `json:"box"`
				}
				if err := json.Unmarshal([]byte(state.Value.JSON("", "")), &parsed); err == nil {
					capture.Visible = parsed.Visible
					capture.Enabled = parsed.Enabled
					capture.BoundingBox = &parsed.Box
				}
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

func injectCookiesAndHeaders(page *rod.Page, targetURL string, opts BrowseOptions) error {
	if len(opts.Headers) > 0 {
		dict := make([]string, 0, len(opts.Headers)*2)
		for k, v := range opts.Headers {
			dict = append(dict, k, v)
		}
		_, err := page.SetExtraHeaders(dict)
		if err != nil {
			return fmt.Errorf("set extra headers: %w", err)
		}
	}

	if len(opts.Cookies) == 0 {
		return nil
	}

	parsed, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("parse target URL for cookies: %w", err)
	}
	domain := parsed.Hostname()
	if domain == "" {
		domain = "localhost"
	}

	cookies := make([]*proto.NetworkCookieParam, 0, len(opts.Cookies))
	for name, value := range opts.Cookies {
		cookies = append(cookies, &proto.NetworkCookieParam{
			Name:   name,
			Value:  value,
			Domain: domain,
			Path:   "/",
		})
	}
	if err := page.SetCookies(cookies); err != nil {
		return fmt.Errorf("set cookies: %w", err)
	}

	return nil
}
