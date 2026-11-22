//go:build !js

package appcheck

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestSiteServerServesRootFromIndexHTML pins the site lookup: a request for
// the site root serves the root index.html, and a miss falls back to the
// host's not-found handling. The Node script the check ports joined paths as
// strings, so "/" reached index.html; a plain filepath.Join collapses "/" to
// the directory, which is never a file, and the app root would 404.
func TestSiteServerServesRootFromIndexHTML(t *testing.T) {
	site := t.TempDir()
	writeSiteFile(t, filepath.Join(site, "index.html"), "<h1>home</h1>")
	writeSiteFile(t, filepath.Join(site, "about.html"), "<h1>about</h1>")
	writeSiteFile(t, filepath.Join(site, "sub", "index.html"), "<h1>sub</h1>")

	srv := httptest.NewServer(siteServer{site: site, notFound: filepath.Join(site, "404.html")})
	t.Cleanup(srv.Close)

	cases := []struct {
		path string
		code int
		body string
	}{
		{"/", http.StatusOK, "<h1>home</h1>"},
		{"/index.html", http.StatusOK, "<h1>home</h1>"},
		{"/about", http.StatusOK, "<h1>about</h1>"},
		{"/sub", http.StatusOK, "<h1>sub</h1>"},
		{"/sub/", http.StatusOK, "<h1>sub</h1>"},
		{"/missing", http.StatusNotFound, ""},
	}
	for _, tc := range cases {
		resp, err := http.Get(srv.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != tc.code {
			t.Errorf("GET %s: status %d, want %d", tc.path, resp.StatusCode, tc.code)
		}
		if tc.body != "" && string(body) != tc.body {
			t.Errorf("GET %s: body %q, want %q", tc.path, body, tc.body)
		}
	}
}

func writeSiteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
