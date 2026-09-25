//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// The WebUI file API serves files whose bytes change under an unchanged
// URL (design/generated/*.css is regenerated in place on token re-export),
// so the response must deny heuristic caching: no-cache forces the browser
// to revalidate against Last-Modified, and a matching If-Modified-Since
// gets a 304 with no body.
func TestHandleFileRead_CacheHeaders(t *testing.T) {
	dir := t.TempDir()
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	server.workspaceRoot = dir
	server.getOrCreateClientContext(defaultWebClientID).WorkspaceRoot = dir

	file := filepath.Join(dir, "design", "generated", "tokens.css")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(":root { --accent: red; }"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/file?path="+filepath.Join("design", "generated", "tokens.css"), nil)
	rec := httptest.NewRecorder()
	server.handleAPIFile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	cc := rec.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want \"no-cache\"", cc)
	}

	lastMod := rec.Header().Get("Last-Modified")
	if lastMod == "" {
		t.Fatal("Last-Modified header missing")
	}

	// The 304 revalidation path: echoing Last-Modified back as
	// If-Modified-Since must yield 304 with no body, and the no-cache
	// directive must still be present.
	cond := httptest.NewRequest(http.MethodGet, "/api/file?path="+filepath.Join("design", "generated", "tokens.css"), nil)
	cond.Header.Set("If-Modified-Since", lastMod)
	rec304 := httptest.NewRecorder()
	server.handleAPIFile(rec304, cond)
	if rec304.Code != http.StatusNotModified {
		t.Fatalf("conditional GET: expected 304, got %d body=%s", rec304.Code, rec304.Body.String())
	}
	if rec304.Body.Len() != 0 {
		t.Errorf("304 response has a body (%d bytes); want none", rec304.Body.Len())
	}
	if got := rec304.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("304 Cache-Control = %q, want \"no-cache\"", got)
	}

	// A fresh copy after the mtime must still be served (200, full body).
	if err := os.Chtimes(file, time.Now().Add(2*time.Second), time.Now().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	stale := httptest.NewRequest(http.MethodGet, "/api/file?path="+filepath.Join("design", "generated", "tokens.css"), nil)
	stale.Header.Set("If-Modified-Since", lastMod)
	rec200 := httptest.NewRecorder()
	server.handleAPIFile(rec200, stale)
	if rec200.Code != http.StatusOK {
		t.Fatalf("GET with old If-Modified-Since: expected 200, got %d", rec200.Code)
	}
	if got := rec200.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("200 Cache-Control = %q, want \"no-cache\"", got)
	}
}
