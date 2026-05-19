//go:build browser

package webcontent

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

const browserInstrumentationScript = `
(() => {
  if (window.__sproutBrowserCaptureInstalled) return;
  window.__sproutBrowserCaptureInstalled = true;
  window.__sproutBrowserCapture = { console: [], errors: [], network: [] };
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
        limitPush(window.__sproutBrowserCapture.console, '[' + level + '] ' + args.map(stringify).join(' '));
      } catch (_err) {}
      return original.apply(this, args);
    };
  }
  window.addEventListener('error', (event) => {
    try {
      const location = event.filename ? ' @ ' + event.filename + ':' + event.lineno + ':' + event.colno : '';
      limitPush(window.__sproutBrowserCapture.errors, String(event.message || 'error') + location);
    } catch (_err) {}
  });
  window.addEventListener('unhandledrejection', (event) => {
    try {
      limitPush(window.__sproutBrowserCapture.errors, 'Unhandled rejection: ' + stringify(event.reason));
    } catch (_err) {}
  });
  const recordNetwork = (value) => {
    try {
      limitPush(window.__sproutBrowserCapture.network, value);
    } catch (_err) {}
  };
  if (typeof window.fetch === 'function') {
    const originalFetch = window.fetch.bind(window);
    window.fetch = async function(input, init) {
      const method = (init && init.method) || (input && input.method) || 'GET';
      const url = typeof input === 'string' ? input : ((input && input.url) || '');
      try {
        const response = await originalFetch(input, init);
        recordNetwork({ type: 'fetch', method, url, status: response.status, ok: !!response.ok, initiator: 'fetch' });
        return response;
      } catch (err) {
        recordNetwork({ type: 'fetch', method, url, error: String(err), initiator: 'fetch' });
        throw err;
      }
    };
  }
  if (typeof window.XMLHttpRequest === 'function') {
    const OriginalXHR = window.XMLHttpRequest;
    function WrappedXHR() {
      const xhr = new OriginalXHR();
      let method = 'GET';
      let url = '';
      const originalOpen = xhr.open;
      xhr.open = function(m, u) {
        method = m || 'GET';
        url = u || '';
        return originalOpen.apply(xhr, arguments);
      };
      xhr.addEventListener('loadend', function() {
        recordNetwork({ type: 'xhr', method, url, status: xhr.status || 0, ok: xhr.status >= 200 && xhr.status < 400, initiator: 'xhr' });
      });
      xhr.addEventListener('error', function() {
        recordNetwork({ type: 'xhr', method, url, error: 'network error', initiator: 'xhr' });
      });
      return xhr;
    }
    WrappedXHR.prototype = OriginalXHR.prototype;
    window.XMLHttpRequest = WrappedXHR;
  }
})();
`

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
			return fmt.Errorf("requireElement for click: %w", err)
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
			return fmt.Errorf("requireElement for hover: %w", err)
		}
		if err := el.Hover(); err != nil {
			return fmt.Errorf("hover %q: %w", step.Selector, err)
		}
		record(fmt.Sprintf("hover %s", step.Selector))
		return nil
	case "type":
		el, err := requireElement(page, step.Selector, timeout)
		if err != nil {
			return fmt.Errorf("requireElement for type: %w", err)
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
			return fmt.Errorf("requireElement for fill: %w", err)
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
				return fmt.Errorf("requireElement for press focus: %w", err)
			}
			if _, err := el.Eval(`() => { this.focus(); return true; }`); err != nil {
				return fmt.Errorf("focus %q before keypress: %w", step.Selector, err)
			}
		}
		if err := pressPageKey(page, step.Key); err != nil {
			return fmt.Errorf("pressPageKey: %w", err)
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
				return fmt.Errorf("requireElement for scroll_to: %w", err)
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
		if err := page.Timeout(getNavigationTimeout(target)).Navigate(target); err != nil {
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
			return fmt.Errorf("requireElement for assert_selector: %w", err)
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
	case "assert_text":
		expected := strings.TrimSpace(step.Expect)
		if expected == "" {
			expected = strings.TrimSpace(step.Value)
		}
		if expected == "" {
			return fmt.Errorf("browse step assert_text requires expect or value")
		}
		bodyText, err := evalToJSONString(page, `() => (document.body && (document.body.innerText || document.body.textContent)) || ''`)
		if err != nil {
			return fmt.Errorf("assert_text: %w", err)
		}
		if !strings.Contains(strings.Trim(bodyText, `"`), expected) {
			return fmt.Errorf("assert_text missing expected text %q", expected)
		}
		record(fmt.Sprintf("assert_text %s", expected))
		return nil
	case "assert_title":
		expected := strings.TrimSpace(step.Expect)
		if expected == "" {
			expected = strings.TrimSpace(step.Value)
		}
		if expected == "" {
			return fmt.Errorf("browse step assert_title requires expect or value")
		}
		info, err := page.Info()
		if err != nil {
			return fmt.Errorf("assert_title page info: %w", err)
		}
		if !strings.Contains(info.Title, expected) {
			return fmt.Errorf("assert_title missing expected text %q in %q", expected, info.Title)
		}
		record(fmt.Sprintf("assert_title %s", expected))
		return nil
	case "assert_url":
		expected := strings.TrimSpace(step.Expect)
		if expected == "" {
			expected = strings.TrimSpace(step.Value)
		}
		if expected == "" {
			return fmt.Errorf("browse step assert_url requires expect or value")
		}
		info, err := page.Info()
		if err != nil {
			return fmt.Errorf("assert_url page info: %w", err)
		}
		if !strings.Contains(info.URL, expected) {
			return fmt.Errorf("assert_url missing expected text %q in %q", expected, info.URL)
		}
		record(fmt.Sprintf("assert_url %s", expected))
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
				return fmt.Errorf("requireElement for wait_for_text: %w", err)
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
