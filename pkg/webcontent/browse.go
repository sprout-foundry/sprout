package webcontent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// effectiveCtx returns opts.Ctx if non-nil, otherwise context.Background().
func effectiveCtx(opts BrowseOptions) context.Context {
	if opts.Ctx != nil {
		return opts.Ctx
	}
	return context.Background()
}

// BrowseURL performs browser-based actions on a URL (screenshots, DOM capture, text extraction).
// It requires the browser build tag and a headless browser (Chromium).
func BrowseURL(url string, opts BrowseOptions) (string, error) {
	if url == "" {
		return "", fmt.Errorf("URL cannot be empty")
	}
	lower := strings.ToLower(url)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return "", fmt.Errorf("URL must start with http:// or https://, got: %s", url)
	}

	ctx := effectiveCtx(opts)
	browser := GetGlobalBrowser()

	switch opts.Action {
	case "screenshot":
		if opts.ScreenshotPath == "" {
			return "", fmt.Errorf("screenshot_path is required for action=screenshot")
		}
		if hasAdvancedBrowseOptions(opts) {
			_, err := browser.Run(ctx, url, opts)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Screenshot saved to: %s", opts.ScreenshotPath), nil
		}
		err := browser.Screenshot(ctx, url, opts.ScreenshotPath, opts.ViewportWidth, opts.ViewportHeight, opts.UserAgent)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Screenshot saved to: %s", opts.ScreenshotPath), nil

	case "dom":
		if hasAdvancedBrowseOptions(opts) {
			result, err := browser.Run(ctx, url, mergeInspectDefaults(opts, false, true))
			if err != nil {
				return "", err
			}
			return result.DOM, nil
		}
		return browser.CaptureDOM(ctx, url, opts.ViewportWidth, opts.ViewportHeight, opts.UserAgent)

	case "text", "":
		if hasAdvancedBrowseOptions(opts) {
			result, err := browser.Run(ctx, url, mergeInspectDefaults(opts, true, false))
			if err != nil {
				return "", err
			}
			return result.VisibleText, nil
		}
		html, err := browser.CaptureDOM(ctx, url, opts.ViewportWidth, opts.ViewportHeight, opts.UserAgent)
		if err != nil {
			return "", err
		}
		return HTMLToText(html), nil

	case "inspect", "interact":
		result, err := browser.Run(ctx, url, mergeInspectDefaults(opts, true, false))
		if err != nil {
			return "", err
		}
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal browse result: %w", err)
		}
		return string(encoded), nil

	default:
		return "", fmt.Errorf("unknown action: %s (valid: screenshot, dom, text, inspect)", opts.Action)
	}
}

func hasAdvancedBrowseOptions(opts BrowseOptions) bool {
	return strings.TrimSpace(opts.WaitForSelector) != "" ||
		strings.TrimSpace(opts.SessionID) != "" ||
		opts.PersistSession ||
		opts.CloseSession ||
		len(opts.Steps) > 0 ||
		len(opts.CaptureSelectors) > 0 ||
		opts.CaptureDOM ||
		opts.CaptureText ||
		opts.IncludeConsole ||
		opts.CaptureNetwork ||
		opts.CaptureStorage ||
		opts.CaptureCookies ||
		opts.ResponseMaxChars > 0
}

func mergeInspectDefaults(opts BrowseOptions, captureText bool, captureDOM bool) BrowseOptions {
	out := opts
	if captureText {
		out.CaptureText = true
	}
	if captureDOM {
		out.CaptureDOM = true
	}
	if !out.CaptureText && !out.CaptureDOM && len(out.CaptureSelectors) == 0 {
		out.CaptureText = true
	}
	if opts.Action == "inspect" || opts.Action == "interact" {
		out.IncludeConsole = true
		out.CaptureNetwork = true
	}
	if opts.Action == "inspect" {
		out.CaptureStorage = true
		out.CaptureCookies = true
	}
	return out
}
