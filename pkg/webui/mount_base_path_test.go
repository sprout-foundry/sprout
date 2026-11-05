//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeBasePath(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"/":               "",
		"agent":           "/agent",
		"/agent":          "/agent",
		"/agent/":         "/agent",
		" /tools/agent/ ": "/tools/agent",
	}
	for in, want := range cases {
		if got := normalizeBasePath(in); got != want {
			t.Errorf("normalizeBasePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripBasePath(t *testing.T) {
	cases := []struct {
		path     string
		want     string
		stripped bool
	}{
		{"/agent", "/", true},
		{"/agent/", "/", true},
		{"/agent/assets/main.js", "/assets/main.js", true},
		{"/agent/api/bootstrap", "/api/bootstrap", true},
		{"/assets/main.js", "/assets/main.js", false},
		// A sibling path that merely shares the prefix must not be rewritten.
		{"/agentic/assets/main.js", "/agentic/assets/main.js", false},
	}
	for _, c := range cases {
		got, ok := stripBasePath("/agent", c.path)
		if got != c.want || ok != c.stripped {
			t.Errorf("stripBasePath(%q) = (%q, %v), want (%q, %v)", c.path, got, ok, c.want, c.stripped)
		}
	}
}

func TestBasePathMiddlewareRewritesPrefixedRequests(t *testing.T) {
	ws := &ReactWebServer{}
	ws.SetBasePath("/agent")

	var seen string
	handler := ws.basePathMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Path
	}))

	for _, path := range []string{"/agent/assets/main.js", "/assets/main.js"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		if seen != "/assets/main.js" {
			t.Errorf("request %q routed as %q, want /assets/main.js", path, seen)
		}
	}
}

func TestBasePathMiddlewareIsInertAtTheRoot(t *testing.T) {
	ws := &ReactWebServer{}
	ws.SetBasePath("")

	var seen string
	handler := ws.basePathMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Path
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/agent/assets/main.js", nil))
	if seen != "/agent/assets/main.js" {
		t.Errorf("without a base path the request path must pass through, got %q", seen)
	}
}

func TestRewriteIndexForBasePath(t *testing.T) {
	index := []byte(`<!doctype html><html><head>
<link rel="icon" href="/logo-mark.svg" />
<link rel="manifest" href="/manifest.json" />
<script type="module" crossorigin src="/assets/main-abc.js"></script>
<link rel="modulepreload" href="/assets/chunk-def.js">
</head><body></body></html>`)

	out := string(rewriteIndexForBasePath(index, "/agent"))

	for _, want := range []string{
		`href="/agent/logo-mark.svg"`,
		`href="/agent/manifest.json"`,
		`src="/agent/assets/main-abc.js"`,
		`href="/agent/assets/chunk-def.js"`,
		`window.SPROUT_PROXY_BASE="/agent"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rewritten index is missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, `src="/assets/`) {
		t.Errorf("an origin-root asset reference survived the rewrite:\n%s", out)
	}
	// The proxy base must be published before the module script runs.
	if strings.Index(out, "SPROUT_PROXY_BASE") > strings.Index(out, `src="/agent/assets/`) {
		t.Errorf("SPROUT_PROXY_BASE must be set before the module script:\n%s", out)
	}
}

func TestRewriteIndexForBasePathIsANoOpAtTheRoot(t *testing.T) {
	index := []byte(`<script src="/assets/main.js"></script>`)
	if out := string(rewriteIndexForBasePath(index, "")); out != string(index) {
		t.Errorf("without a base path the index must be served unchanged, got %q", out)
	}
}

func TestHandleIndexAppliesBasePath(t *testing.T) {
	ws := &ReactWebServer{}
	ws.SetBasePath("/agent")

	rec := httptest.NewRecorder()
	ws.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/agent/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `window.SPROUT_PROXY_BASE="/agent"`) {
		t.Errorf("served index does not publish the mount path:\n%s", body)
	}
	if strings.Contains(body, `src="/assets/`) {
		t.Errorf("served index still references assets from the origin root:\n%s", body)
	}
}
