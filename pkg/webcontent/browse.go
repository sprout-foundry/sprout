package webcontent

import (
	"context"
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
		err := browser.Screenshot(ctx, url, opts.ScreenshotPath, opts.ViewportWidth, opts.ViewportHeight, opts.UserAgent)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Screenshot saved to: %s", opts.ScreenshotPath), nil

	case "dom":
		return browser.CaptureDOM(ctx, url, opts.ViewportWidth, opts.ViewportHeight, opts.UserAgent)

	case "text", "":
		html, err := browser.CaptureDOM(ctx, url, opts.ViewportWidth, opts.ViewportHeight, opts.UserAgent)
		if err != nil {
			return "", err
		}
		return HTMLToText(html), nil

	default:
		return "", fmt.Errorf("unknown action: %s (valid: screenshot, dom, text)", opts.Action)
	}
}
