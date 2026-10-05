//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// The /wasm/* and standalone-page routes exist so those paths never reach
// the SPA catch-all: handleIndex answers every unmatched path with
// index.html as text/html, which for sprout.wasm means the shell fetch
// passes its ok check and dies inside WebAssembly.instantiate with an
// "invalid magic number" — the worst possible error message for the
// actual problem (asset not served).

func newWebServerForRoutes(t *testing.T) *ReactWebServer {
	t.Helper()
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return server
}

// wasmFixturesAvailable reports whether wasm asset fixtures exist (they
// ship via the Vite build; a fresh clone without dist falls back to
// webui/dist on disk, which requires a prior build).
func wasmFixturesAvailable(t *testing.T) {
	t.Helper()
	if _, err := readStaticFile("wasm/wasm_exec.js"); err != nil {
		t.Skipf("wasm assets not available (build artifact): %v", err)
	}
}

func TestHandleWasmAssetsServesRuntimeFiles(t *testing.T) {
	server := newWebServerForRoutes(t)
	wasmFixturesAvailable(t)

	tests := []struct {
		name        string
		path        string
		contentType string
	}{
		{
			name:        "wasm binary",
			path:        "/wasm/sprout.wasm",
			contentType: "application/wasm",
		},
		{
			name:        "wasm exec runtime",
			path:        "/wasm/wasm_exec.js",
			contentType: "text/javascript",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := readStaticFile("wasm/" + strings.TrimPrefix(tc.path, "/wasm/")); err != nil {
				t.Skipf("wasm asset %s not available (build artifact): %v", tc.path, err)
			}

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			server.handleWasmAssets(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rec.Code)
			}
			if rec.Body.Len() == 0 {
				t.Fatalf("expected non-empty body for %s", tc.path)
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, tc.contentType) {
				t.Fatalf("expected content type prefix %q, got %q", tc.contentType, got)
			}
		})
	}
}

func TestHandleWasmAssetsRejectsTraversal(t *testing.T) {
	server := newWebServerForRoutes(t)

	for _, path := range []string{
		"/wasm/../handlers.go",
		"/wasm/..%2fhandlers.go",
		"/wasm/%2e%2e/static_loader.go",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		server.handleWasmAssets(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("expected non-200 for traversal path %s, got 200", path)
		}
	}
}

func TestHandleStandalonePagesServeComponentShells(t *testing.T) {
	server := newWebServerForRoutes(t)

	for _, page := range []string{"editor.html", "terminal.html"} {
		t.Run(page, func(t *testing.T) {
			if _, err := readStaticFile(page); err != nil {
				t.Skipf("%s not available (build artifact): %v", page, err)
			}

			req := httptest.NewRequest(http.MethodGet, "/"+page, nil)
			rec := httptest.NewRecorder()
			server.handleStandalonePage(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "<!doctype html>") && !strings.Contains(body, "<!DOCTYPE html>") {
				t.Fatalf("expected an HTML document for %s", page)
			}
			// The component shells are NOT the React app — their titles
			// distinguish them from an index.html fallback.
			if !strings.Contains(body, "Sprout Editor") && !strings.Contains(body, "Sprout Terminal") {
				t.Fatalf("expected the standalone component shell for %s, got a different document", page)
			}
		})
	}
}

func TestHandleStandalonePageRejectsOtherNames(t *testing.T) {
	server := newWebServerForRoutes(t)

	// The handler is registered for exactly two paths; anything else that
	// reaches it (shouldn't via the mux, but defense in depth) is a 404.
	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	rec := httptest.NewRecorder()
	server.handleStandalonePage(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for index.html via the standalone handler, got %d", rec.Code)
	}
}
