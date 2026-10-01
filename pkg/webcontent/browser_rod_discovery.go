//go:build !js

// browser_rod_discovery.go — GPU probe + system browser discovery, split
// from browser_rod_launch.go. The GPU-probe cache (gpuProbeDone/Failed,
// markGPUProbe, resetGPUProbe, probeGPUSupport) and the candidate-browser
// search (systemBrowserPaths, findPlaywrightChromium) are the "where do I
// find a working browser / does the GPU work" concerns; the renderer
// lifecycle (connect, sessions, Close) stays in browser_rod_launch.go.
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
// Playwright cache paths are checked first because they are always native
// binaries that work in containers, snaps, and restricted environments.
func systemBrowserPaths() []string {
	homeDir := os.Getenv("HOME")
	playwrightCache := filepath.Join(homeDir, ".cache", "ms-playwright")

	// Discover Playwright chromium versions (prefer newest first)
	playwrightBins := findPlaywrightChromium(playwrightCache)

	return append(playwrightBins,
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/snap/bin/chromium",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
		"C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe",
	)
}

// findPlaywrightChromium scans the Playwright cache directory for chromium
// binaries and returns them sorted newest-first. Returns empty slice if none found.
func findPlaywrightChromium(cacheDir string) []string {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil
	}

	type version struct {
		bin string
		num int
	}
	var versions []version

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "chromium-") {
			continue
		}
		// Parse version number from "chromium-1217" etc.
		numStr := strings.TrimPrefix(name, "chromium-")
		var num int
		fmt.Sscanf(numStr, "%d", &num)
		bin := filepath.Join(cacheDir, name, "chrome-linux", "chrome")
		if _, err := os.Stat(bin); err == nil {
			versions = append(versions, version{bin: bin, num: num})
		}
	}

	// Sort newest first
	for i := 0; i < len(versions); i++ {
		for j := i + 1; j < len(versions); j++ {
			if versions[j].num > versions[i].num {
				versions[i], versions[j] = versions[j], versions[i]
			}
		}
	}

	var bins []string
	for _, v := range versions {
		bins = append(bins, v.bin)
	}
	return bins
}
