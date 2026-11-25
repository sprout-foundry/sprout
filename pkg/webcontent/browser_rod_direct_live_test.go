//go:build !js

package webcontent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestDirectBrowserReachesLoopbackThroughAConfiguredProxy is the behaviour the
// app check depends on: with a proxy configured for outbound traffic, its own
// loopback server is still reached. Pinning the launcher flags is not enough —
// only a real page load shows Chromium honoured them. Skipped when no browser
// can run here.
func TestDirectBrowserReachesLoopbackThroughAConfiguredProxy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<h1 id=ok>home</h1>"))
	}))
	t.Cleanup(srv.Close)

	// A proxy that answers everything would turn this into a proxy error if
	// the browser used it.
	var proxied int
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied++
		http.Error(w, "proxy should not answer for loopback", http.StatusForbidden)
	}))
	t.Cleanup(proxy.Close)
	t.Setenv("SPROUT_HTTP_PROXY", proxy.URL)
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")

	renderer := NewDirectBrowserRenderer()
	t.Cleanup(renderer.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	html, err := renderer.CaptureDOM(ctx, srv.URL, 1280, 900, "")
	if err != nil {
		if errors.Is(err, ErrBrowserUnavailable) {
			t.Skipf("no browser available: %v", err)
		}
		t.Fatalf("CaptureDOM: %v", err)
	}
	if proxied != 0 {
		t.Errorf("the proxy answered %d request(s); loopback must stay direct", proxied)
	}
	if !strings.Contains(html, "home") {
		t.Errorf("page did not render through the direct browser: %q", html)
	}
}
