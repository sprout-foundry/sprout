//go:build !js

package webcontent

// browser_rod_steps.go — per-action handlers for the browse-step
// engine: each browseStepX function performs one action dispatched by
// executeBrowseStep (browser_rod_interaction.go).
import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// browseStepWaitFor performs the wait_for browse step.
func browseStepWaitFor(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
	if strings.TrimSpace(step.Selector) == "" {
		return fmt.Errorf("browse step wait_for requires selector")
	}
	if _, err := page.Timeout(timeout).Element(step.Selector); err != nil {
		return fmt.Errorf("wait_for %q: %w", step.Selector, err)
	}
	record(fmt.Sprintf("wait_for %s", step.Selector))
	return nil
}

// browseStepClick performs the click browse step.
func browseStepClick(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
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
}

// browseStepHover performs the hover browse step.
func browseStepHover(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
	el, err := requireElement(page, step.Selector, timeout)
	if err != nil {
		return fmt.Errorf("requireElement for hover: %w", err)
	}
	if err := el.Hover(); err != nil {
		return fmt.Errorf("hover %q: %w", step.Selector, err)
	}
	record(fmt.Sprintf("hover %s", step.Selector))
	return nil
}

// browseStepType performs the type browse step.
func browseStepType(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
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
}

// browseStepFill performs the fill browse step.
func browseStepFill(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
	el, err := requireElement(page, step.Selector, timeout)
	if err != nil {
		return fmt.Errorf("requireElement for fill: %w", err)
	}
	if _, err := el.Eval(`value => {
		this.focus();
		const nativeInputValueProperty = Object.getOwnPropertyDescriptor(
			HTMLInputElement.prototype, 'value'
		).set;
		if (nativeInputValueProperty) {
			nativeInputValueProperty.call(this, value);
		} else {
			this.value = value;
		}
		this.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText', data: value }));
		this.dispatchEvent(new Event('change', { bubbles: true }));
		return true;
	}`, step.Value); err != nil {
		return fmt.Errorf("fill %q: %w", step.Selector, err)
	}
	_ = page.WaitStable(stableDuration)
	record(fmt.Sprintf("fill %s", step.Selector))
	return nil
}

// browseStepPress performs the press browse step.
func browseStepPress(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
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
}

// browseStepSleep performs the sleep browse step.
func browseStepSleep(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
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
}

// browseStepScrollTo performs the scroll_to browse step.
func browseStepScrollTo(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
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
}

// browseStepNavigate performs the navigate browse step.
func browseStepNavigate(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
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
}

// browseStepReload performs the reload browse step.
func browseStepReload(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
	if err := page.Reload(); err != nil {
		return fmt.Errorf("reload page: %w", err)
	}
	if err := page.WaitStable(stableDuration); err != nil {
		return fmt.Errorf("wait stable after reload: %w", err)
	}
	record("reload")
	return nil
}

// browseStepBack performs the back browse step.
func browseStepBack(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
	if err := page.NavigateBack(); err != nil {
		return fmt.Errorf("navigate back: %w", err)
	}
	if err := page.WaitStable(stableDuration); err != nil {
		return fmt.Errorf("wait stable after back: %w", err)
	}
	record("back")
	return nil
}

// browseStepForward performs the forward browse step.
func browseStepForward(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
	if err := page.NavigateForward(); err != nil {
		return fmt.Errorf("navigate forward: %w", err)
	}
	if err := page.WaitStable(stableDuration); err != nil {
		return fmt.Errorf("wait stable after forward: %w", err)
	}
	record("forward")
	return nil
}

// browseStepAssertSelector performs the assert_selector browse step.
func browseStepAssertSelector(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
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
}

// browseStepAssertText performs the assert_text browse step.
func browseStepAssertText(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
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
}

// browseStepAssertTitle performs the assert_title browse step.
func browseStepAssertTitle(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
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
}

// browseStepAssertURL performs the assert_url browse step.
func browseStepAssertURL(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
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
}

// browseStepWaitForText performs the wait_for_text browse step.
func browseStepWaitForText(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
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
}

// browseStepEval performs the eval browse step.
func browseStepEval(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
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
}

// browseStepWaitForFunction performs the wait_for_function browse step.
func browseStepWaitForFunction(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
	if strings.TrimSpace(step.Script) == "" {
		return fmt.Errorf("browse step wait_for_function requires script")
	}
	if err := page.Timeout(timeout).Wait(rod.Eval(step.Script)); err != nil {
		return fmt.Errorf("wait_for_function: %w", err)
	}
	record(fmt.Sprintf("wait_for_function %s", step.Script))
	return nil
}

// browseStepScreenshotSelector performs the screenshot_selector browse step.
func browseStepScreenshotSelector(page *rod.Page, step BrowseStep, timeout time.Duration, record func(string), result *BrowseResult) error {
	if strings.TrimSpace(step.Selector) == "" {
		return fmt.Errorf("browse step screenshot_selector requires selector")
	}
	if strings.TrimSpace(step.ScreenshotPath) == "" {
		return fmt.Errorf("browse step screenshot_selector requires screenshot_path")
	}
	el, err := requireElement(page, step.Selector, timeout)
	if err != nil {
		return fmt.Errorf("requireElement for screenshot_selector: %w", err)
	}
	data, err := el.Screenshot(proto.PageCaptureScreenshotFormatPng, 100)
	if err != nil {
		return fmt.Errorf("screenshot_selector %q: %w", step.Selector, err)
	}
	if err := os.WriteFile(step.ScreenshotPath, data, 0644); err != nil {
		return fmt.Errorf("write screenshot %q: %w", step.ScreenshotPath, err)
	}
	record(fmt.Sprintf("screenshot_selector %s -> %s", step.Selector, step.ScreenshotPath))
	return nil
}
