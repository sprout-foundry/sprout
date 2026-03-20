//go:build browser

package webcontent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipWithoutBrowser skips tests unless LEDIT_TEST_BROWSER=1 is set.
func skipWithoutBrowser(t *testing.T) {
	t.Helper()
	if os.Getenv("LEDIT_TEST_BROWSER") == "" {
		t.Skip("skipping: set LEDIT_TEST_BROWSER=1 to run browser tests")
	}
}

// ---------------------------------------------------------------------------
// Screenshot
// ---------------------------------------------------------------------------

func TestRodRenderer_Screenshot_ExampleDotCom(t *testing.T) {
	skipWithoutBrowser(t)

	r := NewBrowserRenderer()
	defer r.Close()

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "screenshot.png")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := r.Screenshot(ctx, "https://example.com", outPath, 0, 0, "")
	require.NoError(t, err)

	info, err := os.Stat(outPath)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(1000), "screenshot file should be non-trivial sized")
}

func TestRodRenderer_Screenshot_CustomViewport(t *testing.T) {
	skipWithoutBrowser(t)

	r := NewBrowserRenderer()
	defer r.Close()

	tmpDir := t.TempDir()
	outMobile := filepath.Join(tmpDir, "mobile.png")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Render at mobile dimensions — viewport should be applied.
	err := r.Screenshot(ctx, "https://example.com", outMobile, 375, 812, "")
	require.NoError(t, err)

	info, err := os.Stat(outMobile)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(500))
}

func TestRodRenderer_Screenshot_CustomUserAgent(t *testing.T) {
	skipWithoutBrowser(t)

	r := NewBrowserRenderer()
	defer r.Close()

	// Create a server that echoes the User-Agent.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ua := req.Header.Get("User-Agent")
		fmt.Fprintf(w, `<!DOCTYPE html><html><body><h1>UA: %s</h1></body></html>`, ua)
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Capture DOM to inspect the rendered content.
	html, err := r.CaptureDOM(ctx, ts.URL, 0, 0, "TestBot/1.0")
	require.NoError(t, err)
	assert.Contains(t, html, "TestBot/1.0", "user-agent should be applied")
}

func TestRodRenderer_Screenshot_CreatesParentDirs(t *testing.T) {
	skipWithoutBrowser(t)

	r := NewBrowserRenderer()
	defer r.Close()

	outPath := filepath.Join(t.TempDir(), "nested", "dirs", "shot.png")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := r.Screenshot(ctx, "https://example.com", outPath, 0, 0, "")
	require.NoError(t, err)

	_, err = os.Stat(outPath)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// CaptureDOM
// ---------------------------------------------------------------------------

func TestRodRenderer_CaptureDOM_ExampleDotCom(t *testing.T) {
	skipWithoutBrowser(t)

	r := NewBrowserRenderer()
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	html, err := r.CaptureDOM(ctx, "https://example.com", 0, 0, "")
	require.NoError(t, err)
	assert.NotEmpty(t, html)
	assert.Contains(t, html, "Example Domain")
	assert.Contains(t, html, "<html", "CaptureDOM should return raw HTML")
}

func TestRodRenderer_CaptureDOM_WithViewport(t *testing.T) {
	skipWithoutBrowser(t)

	r := NewBrowserRenderer()
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	html, err := r.CaptureDOM(ctx, "https://example.com", 1920, 1080, "")
	require.NoError(t, err)
	assert.NotEmpty(t, html)
	assert.Contains(t, html, "Example Domain")
}

// ---------------------------------------------------------------------------
// BrowseURL E2E (uses real browser via globalBrowser singleton)
// ---------------------------------------------------------------------------

func TestBrowseURL_TextAction_E2E(t *testing.T) {
	skipWithoutBrowser(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Test Page</title></head><body><h1>Hello World</h1><p>This is a test.</p></body></html>`))
	}))
	defer ts.Close()

	text, err := BrowseURL(ts.URL, BrowseOptions{Action: "text"})
	require.NoError(t, err)
	assert.Contains(t, text, "Hello World")
	assert.Contains(t, text, "Test Page")
	// Should NOT contain HTML tags — text mode strips them.
	assert.NotContains(t, text, "<html>")
	assert.NotContains(t, text, "<h1>")
}

func TestBrowseURL_DOMAction_E2E(t *testing.T) {
	skipWithoutBrowser(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>DOM Test</title></head><body><div id="app"><h1>Rendered</h1></div></body></html>`))
	}))
	defer ts.Close()

	dom, err := BrowseURL(ts.URL, BrowseOptions{Action: "dom"})
	require.NoError(t, err)
	assert.Contains(t, dom, "<html")
	assert.Contains(t, dom, "Rendered")
}

func TestBrowseURL_ScreenshotAction_E2E(t *testing.T) {
	skipWithoutBrowser(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body style="background:#0f0"><h1>Screenshot Test</h1></body></html>`))
	}))
	defer ts.Close()

	outPath := filepath.Join(t.TempDir(), "e2e_screenshot.png")
	result, err := BrowseURL(ts.URL, BrowseOptions{Action: "screenshot", ScreenshotPath: outPath})
	require.NoError(t, err)
	assert.Contains(t, result, outPath)

	info, err := os.Stat(outPath)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(500), "screenshot should have content")
}

// ---------------------------------------------------------------------------
// Jina empty-content retry (simulated via fetchDirectURL path)
// ---------------------------------------------------------------------------

func TestBrowseURL_DefaultActionIsText(t *testing.T) {
	skipWithoutBrowser(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><h1>Default Action</h1></body></html>`))
	}))
	defer ts.Close()

	// Empty action should default to "text"
	text, err := BrowseURL(ts.URL, BrowseOptions{})
	require.NoError(t, err)
	assert.Contains(t, text, "Default Action")
	assert.NotContains(t, text, "<html>")
}

// ---------------------------------------------------------------------------
// localhost browser-first: fetchDirectURL tries browser for localhost HTML
// ---------------------------------------------------------------------------

func TestFetchDirectURL_LocalhostHTML_BrowserRendering(t *testing.T) {
	skipWithoutBrowser(t)

	// We need a server bound to "localhost" specifically (not 127.0.0.1).
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Local Dev Server</title></head><body><div id="root"></div><script>document.getElementById('root').innerText = 'JS Rendered!';</script></body></html>`))
	}))
	defer ts.Close()

	// Build a localhost URL pointing to the same port.
	localURL := fmt.Sprintf("http://localhost:%s/", ts.URL[len("http://127.0.0.1:"):])

	fetcher := NewWebContentFetcher()
	content, err := fetcher.fetchDirectURL(localURL)

	require.NoError(t, err)
	// The browser should have run the JS and rendered "JS Rendered!" into the div.
	assert.Contains(t, content, "JS Rendered!", "localhost HTML should be browser-rendered")
	assert.Contains(t, content, "Local Dev Server")
}

func TestFetchDirectURL_NonLocalhost_SPAStillDetected(t *testing.T) {
	skipWithoutBrowser(t)

	// Serve an SPA shell on a non-localhost address — SPA detection should still work.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>SPA App</title></head><body><div id="root"></div><script src="/bundle.js"></script></body></html>`))
	}))
	defer ts.Close()

	fetcher := NewWebContentFetcher()
	content, err := fetcher.fetchDirectURL(ts.URL)

	require.NoError(t, err)
	// The SPA shell detection should trigger browser rendering.
	// After rendering, we should at least see the title extracted.
	assert.Contains(t, content, "SPA App")
}

// ---------------------------------------------------------------------------
// Viewport/UA combinations
// ---------------------------------------------------------------------------

func TestCaptureDOM_ViewportAndUA_Together(t *testing.T) {
	skipWithoutBrowser(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, `<!DOCTYPE html><html><body>
			<p>UA: %s</p>
			<script>document.write("<p>Width: " + window.innerWidth + "</p>");</script>
		</body></html>`, req.Header.Get("User-Agent"))
	}))
	defer ts.Close()

	r := NewBrowserRenderer()
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	html, err := r.CaptureDOM(ctx, ts.URL, 768, 1024, "MyTestAgent/2.0")
	require.NoError(t, err)
	assert.Contains(t, html, "MyTestAgent/2.0")
	assert.Contains(t, html, "Width: 768", "viewport width should be applied via emulation")
}

// ---------------------------------------------------------------------------
// Error handling
// ---------------------------------------------------------------------------

func TestRodRenderer_Screenshot_InvalidURL(t *testing.T) {
	skipWithoutBrowser(t)

	r := NewBrowserRenderer()
	defer r.Close()

	tmpDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := r.Screenshot(ctx, "http://0.0.0.0:1", filepath.Join(tmpDir, "fail.png"), 0, 0, "")
	assert.Error(t, err)
}

func TestRodRenderer_CaptureDOM_InvalidURL(t *testing.T) {
	skipWithoutBrowser(t)

	r := NewBrowserRenderer()
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := r.CaptureDOM(ctx, "http://0.0.0.0:1", 0, 0, "")
	assert.Error(t, err)
}

func TestRodRenderer_CloseIdempotent(t *testing.T) {
	skipWithoutBrowser(t)

	r := NewBrowserRenderer()
	r.Close()
	r.Close() // second close should not panic
}
