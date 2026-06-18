//go:build !js

// browser_rod_content.go provides page navigation and HTML/DOM content extraction
// for the go-rod based BrowserRenderer implementation.

package webcontent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

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
