//go:build !js

package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestPreviewOrigin starts a preview origin rooted at root and returns it
// with a cleanup that stops the listener.
func newTestPreviewOrigin(t *testing.T, root string) *previewOrigin {
	t.Helper()
	p := newPreviewOrigin()
	if err := p.start(root); err != nil {
		t.Fatalf("starting preview origin: %v", err)
	}
	t.Cleanup(p.stop)
	return p
}

// getPreview fetches url and returns the response with its body read.
func getPreview(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx,gosec // G107: loopback test listener
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return resp, string(body)
}

func TestPreviewOriginServesWorkspaceFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "page.html"), []byte("<h1>hello</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := newTestPreviewOrigin(t, root)
	token, err := p.mintToken()
	if err != nil {
		t.Fatalf("minting token: %v", err)
	}

	resp, body := getPreview(t, p.previewURLFor(token, "page.html"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if body != "<h1>hello</h1>" {
		t.Errorf("body = %q, want the file content", body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

func TestPreviewOriginServesRelativeAssets(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app.js"), []byte("console.log(1)"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "style.css"), []byte("body{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := newTestPreviewOrigin(t, root)
	token, err := p.mintToken()
	if err != nil {
		t.Fatal(err)
	}

	jsResp, _ := getPreview(t, p.previewURLFor(token, "assets/app.js"))
	if ct := jsResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Errorf("js Content-Type = %q, want application/javascript", ct)
	}

	cssResp, _ := getPreview(t, p.previewURLFor(token, "assets/style.css"))
	if ct := cssResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("css Content-Type = %q, want text/css", ct)
	}
}

func TestPreviewOriginRejectsUnknownToken(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "page.html"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := newTestPreviewOrigin(t, root)
	url := "http://" + p.address() + previewPathPref + "deadbeef/page.html"

	resp, _ := getPreview(t, url)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for an unknown token", resp.StatusCode)
	}
}

func TestPreviewOriginRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside-secret.html")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })

	p := newTestPreviewOrigin(t, root)
	token, err := p.mintToken()
	if err != nil {
		t.Fatal(err)
	}

	// The token is valid; only the path is hostile. It must not escape root.
	url := "http://" + p.address() + previewPathPref + token + "/../outside-secret.html"
	resp, body := getPreview(t, url)
	if resp.StatusCode == http.StatusOK {
		t.Errorf("status = 200 for a traversal path; body = %q", body)
	}
	if strings.Contains(body, "secret") {
		t.Errorf("served content outside the workspace: %q", body)
	}
}

func TestPreviewOriginRejectsDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}

	p := newTestPreviewOrigin(t, root)
	token, err := p.mintToken()
	if err != nil {
		t.Fatal(err)
	}

	resp, _ := getPreview(t, p.previewURLFor(token, "sub"))
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a directory", resp.StatusCode)
	}
}

func TestPreviewOriginRejectsNonGet(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "page.html"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := newTestPreviewOrigin(t, root)
	token, err := p.mintToken()
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Post(p.previewURLFor(token, "page.html"), "text/plain", strings.NewReader("x")) //nolint:noctx // test helper
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestPreviewOriginSetsIsolatingCSP(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "page.html"), []byte("<p>x</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := newTestPreviewOrigin(t, root)
	token, err := p.mintToken()
	if err != nil {
		t.Fatal(err)
	}

	resp, _ := getPreview(t, p.previewURLFor(token, "page.html"))
	csp := resp.Header.Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("no CSP on the preview response")
	}
	// The app embeds the preview from a DIFFERENT origin, so frame-ancestors
	// must not be 'self'/'none' — either would render the preview blank.
	if strings.Contains(csp, "frame-ancestors 'self'") || strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP = %q blocks the app origin from framing the preview", csp)
	}
	// The preview origin must not be reachable from the app origin's
	// credentials: no wildcard connect-src.
	if strings.Contains(csp, "connect-src *") {
		t.Errorf("CSP = %q must not allow connect-src *", csp)
	}
	// Isolation rests on the origin differing, so connect-src must not name
	// the app origin's API surface.
	if strings.Contains(csp, "connect-src 'self' *") {
		t.Errorf("CSP = %q must not widen connect-src", csp)
	}
}

func TestPreviewOriginExpiredTokenRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "page.html"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := newTestPreviewOrigin(t, root)
	token, err := p.mintToken()
	if err != nil {
		t.Fatal(err)
	}

	// Expire the grant directly rather than sleeping.
	p.mu.Lock()
	p.tokens[token] = previewToken{expires: time.Now().Add(-time.Minute)}
	p.mu.Unlock()

	resp, _ := getPreview(t, p.previewURLFor(token, "page.html"))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for an expired token", resp.StatusCode)
	}
}

func TestPreviewOriginMintDropsExpiredTokens(t *testing.T) {
	p := newPreviewOrigin()
	expired, err := p.mintToken()
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.tokens[expired] = previewToken{expires: time.Now().Add(-time.Minute)}
	p.mu.Unlock()

	if _, err := p.mintToken(); err != nil {
		t.Fatal(err)
	}
	if p.validToken(expired) {
		t.Error("expired token still valid after a mint")
	}
	p.mu.RLock()
	count := len(p.tokens)
	p.mu.RUnlock()
	if count != 1 {
		t.Errorf("token map size = %d, want 1 (expired grant dropped)", count)
	}
}

func TestPreviewURLForEmptyAddress(t *testing.T) {
	p := newPreviewOrigin()
	if url := p.previewURLFor("tok", "a.html"); url != "" {
		t.Errorf("previewURLFor on a stopped origin = %q, want empty", url)
	}
}

// TestPreviewOriginIsDistinctFromAppOrigin asserts the whole point of the
// design: the preview listener is a different origin than the app server, so
// the previewed page cannot read the app's sessionStorage or cookies.
func TestPreviewOriginIsDistinctFromAppOrigin(t *testing.T) {
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(app.Close)

	root := t.TempDir()
	p := newTestPreviewOrigin(t, root)

	appURL := strings.TrimPrefix(app.URL, "http://")
	if p.address() == appURL {
		t.Fatalf("preview origin %q equals the app origin", p.address())
	}
}
