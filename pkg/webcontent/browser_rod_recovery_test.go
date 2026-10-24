//go:build !js

package webcontent

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A cached browser handle is not proof of a live browser. Chromium can crash
// or be OOM-killed while the process keeps running; before the liveness check
// every later call failed with "Failed to open a new tab" until the whole
// process restarted.
func TestCaptureDOMRecoversFromADeadBrowser(t *testing.T) {
	r := NewBrowserRenderer()
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if _, err := r.CaptureDOM(ctx, "data:text/html,<h1>one</h1>", 0, 0, ""); err != nil {
		t.Skipf("no browser available: %v", err)
	}

	// Kill the browser behind the cached handle, leaving the renderer open —
	// the same state a Chromium crash leaves behind.
	rr := r.(*rodRenderer)
	rr.mu.Lock()
	cached := rr.browser
	rr.mu.Unlock()
	if cached == nil {
		t.Skip("no cached browser to kill")
	}
	if err := cached.Close(); err != nil {
		t.Fatalf("closing the cached browser: %v", err)
	}

	html, err := r.CaptureDOM(ctx, "data:text/html,<h1>two</h1>", 0, 0, "")
	if err != nil {
		t.Fatalf("capture did not recover from a dead browser: %v", err)
	}
	if !strings.Contains(html, "two") {
		t.Errorf("recovered capture returned the wrong content: %q", html)
	}
}

// A live browser must not be relaunched on every call.
func TestConnectReusesALiveBrowser(t *testing.T) {
	r := NewBrowserRenderer()
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if _, err := r.CaptureDOM(ctx, "data:text/html,<h1>one</h1>", 0, 0, ""); err != nil {
		t.Skipf("no browser available: %v", err)
	}

	rr := r.(*rodRenderer)
	rr.mu.Lock()
	first := rr.browser
	rr.mu.Unlock()

	if _, err := r.CaptureDOM(ctx, "data:text/html,<h1>two</h1>", 0, 0, ""); err != nil {
		t.Fatalf("second capture: %v", err)
	}

	rr.mu.Lock()
	second := rr.browser
	rr.mu.Unlock()

	if first != second {
		t.Error("a live browser was replaced; connect must reuse a healthy handle")
	}
}

func TestBrowserAliveRejectsNil(t *testing.T) {
	if browserAlive(nil) {
		t.Error("a nil handle reported alive")
	}
}
