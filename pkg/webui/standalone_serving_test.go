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

func TestHandleWasmAssetsNegotiatesPrecompressed(t *testing.T) {
	server := newWebServerForRoutes(t)

	// The precompressed siblings only exist after scripts/build-wasm.sh
	// ran with compression enabled; skip when this checkout has none.
	if _, err := readStaticFile("wasm/sprout.wasm.br"); err != nil {
		t.Skipf("precompressed wasm variants not available (build artifact): %v", err)
	}

	tests := []struct {
		name      string
		acceptEnc string
		wantEnc   string
	}{
		{name: "brotli preferred", acceptEnc: "gzip, deflate, br", wantEnc: "br"},
		{name: "gzip only", acceptEnc: "gzip, deflate", wantEnc: "gzip"},
		{name: "identity wins when nothing advertised", acceptEnc: "identity", wantEnc: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/wasm/sprout.wasm", nil)
			req.Header.Set("Accept-Encoding", tc.acceptEnc)
			rec := httptest.NewRecorder()
			server.handleWasmAssets(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}
			gotEnc := rec.Header().Get("Content-Encoding")
			if gotEnc != tc.wantEnc {
				t.Fatalf("Content-Encoding = %q, want %q", gotEnc, tc.wantEnc)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/wasm") {
				t.Fatalf("inner content type must still be application/wasm, got %q", ct)
			}
			if tc.wantEnc != "" {
				if vary := rec.Header().Get("Vary"); !strings.Contains(vary, "Accept-Encoding") {
					t.Fatalf("expected Vary: Accept-Encoding on encoded responses, got %q", vary)
				}
			}
		})
	}
}

func TestHandleWasmAssetsDoesNotCompressWasmExec(t *testing.T) {
	server := newWebServerForRoutes(t)
	wasmFixturesAvailable(t)

	// wasm_exec.js is tiny and compresses poorly as a precomputed artifact;
	// it is always served raw regardless of Accept-Encoding.
	req := httptest.NewRequest(http.MethodGet, "/wasm/wasm_exec.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	rec := httptest.NewRecorder()
	server.handleWasmAssets(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("wasm_exec.js must not carry Content-Encoding, got %q", enc)
	}
}

func TestNegotiatePrecompressedWasmQValueRejection(t *testing.T) {
	server := newWebServerForRoutes(t)

	// A client that explicitly rejects an encoding (q=0) cannot decompress
	// that body — serving one would hard-break the load.
	tests := []struct {
		name      string
		acceptEnc string
		wantEnc   string
	}{
		{name: "gzip explicitly rejected, br accepted", acceptEnc: "gzip;q=0, br", wantEnc: "br"},
		{name: "br explicitly rejected, gzip accepted", acceptEnc: "br;q=0, gzip", wantEnc: "gzip"},
		{name: "gzip q=0.000 variant counts as rejection", acceptEnc: "gzip;q=0.000, br", wantEnc: "br"},
		{name: "both rejected → identity", acceptEnc: "gzip;q=0, br;q=0", wantEnc: ""},
		{name: "plain tokens unaffected", acceptEnc: "gzip, deflate, br", wantEnc: "br"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := readStaticFile("wasm/sprout.wasm.br"); err != nil {
				t.Skipf("precompressed wasm variants not available: %v", err)
			}
			req := httptest.NewRequest(http.MethodGet, "/wasm/sprout.wasm", nil)
			req.Header.Set("Accept-Encoding", tc.acceptEnc)
			rec := httptest.NewRecorder()
			server.handleWasmAssets(rec, req)

			if got := rec.Header().Get("Content-Encoding"); got != tc.wantEnc {
				t.Fatalf("Content-Encoding = %q, want %q", got, tc.wantEnc)
			}
		})
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

func TestRejectsEncoding(t *testing.T) {
	tests := []struct {
		ae    string
		token string
		want  bool
	}{
		{"gzip;q=0, br", "gzip", true},
		{"gzip;q=0, br", "br", false},
		{"br;q=0.000, gzip", "br", true},
		{"gzip, deflate, br", "gzip", false},
		{"gzip", "gzip", false},
		{"", "gzip", false},
		{"deflate", "gzip", false},
		{"gzip;q=0.5", "gzip", false}, // nonzero q is not a rejection
	}
	for _, tc := range tests {
		if got := rejectsEncoding(tc.ae, tc.token); got != tc.want {
			t.Errorf("rejectsEncoding(%q, %q) = %v; want %v", tc.ae, tc.token, got, tc.want)
		}
	}
}
