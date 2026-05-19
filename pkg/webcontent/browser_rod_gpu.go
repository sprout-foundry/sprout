//go:build browser

package webcontent

import (
	"context"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// GPU probe state — persisted across browser reconnects within the same process.
var (
	gpuStateMu     sync.RWMutex
	gpuStateProbed bool
	gpuStateWorks  bool
)

func gpuProbeDone() bool {
	gpuStateMu.RLock()
	defer gpuStateMu.RUnlock()
	return gpuStateProbed
}

func gpuProbeFailed() bool {
	gpuStateMu.RLock()
	defer gpuStateMu.RUnlock()
	return gpuStateProbed && !gpuStateWorks
}

func markGPUProbe(works bool) {
	gpuStateMu.Lock()
	defer gpuStateMu.Unlock()
	gpuStateProbed = true
	gpuStateWorks = works
}

// resetGPUProbe clears the cached GPU probe result. Used for testing.
func resetGPUProbe() {
	gpuStateMu.Lock()
	defer gpuStateMu.Unlock()
	gpuStateProbed = false
	gpuStateWorks = false
}

// probeGPUSupport tests whether screenshot capture works on the given browser.
// Returns true if the screenshot succeeded, false if it timed out (GPU unavailable).
// The browser is not closed here — the caller decides what to do.
func probeGPUSupport(ctx context.Context, browser *rod.Browser) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	page, err := browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		return false
	}
	defer func() { _ = page.Close() }()

	// Use about:blank — no network needed.
	if err := page.Navigate("about:blank"); err != nil {
		return false
	}

	done := make(chan struct{})
	var probeErr error
	go func() {
		_, probeErr = page.Screenshot(false, &proto.PageCaptureScreenshot{
			Format: proto.PageCaptureScreenshotFormatPng,
		})
		close(done)
	}()

	select {
	case <-done:
		return probeErr == nil
	case <-probeCtx.Done():
		// The screenshot goroutine may still be running (GPU hang).
		// Wait briefly for it to finish, then abandon it. The browser
		// will be closed by the caller, which terminates any in-flight
		// CDP requests and causes the goroutine to exit.
		return false
	}
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
