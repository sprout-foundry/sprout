//go:build !js

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

// requireCaptureBrowser mirrors the pkg/capture tests: the fleet machine has no
// configured proxy; a configured one would blank the fixture page.
func requireCaptureBrowser(t *testing.T) {
	t.Helper()
	t.Setenv("SPROUT_HTTP_PROXY", "")
	s, err := webcontent.GetGlobalBrowser().OpenRodSession(t.Context())
	if err != nil {
		t.Skipf("no browser available: %v", err)
	}
	s.Close()
}

func TestRunCapture_OutFiles(t *testing.T) {
	requireCaptureBrowser(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>T</title></head><body><p>hello</p></body></html>`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	var out strings.Builder
	cmd := &cobra.Command{}
	code, err := RunCapture(cmd, captureFlags{viewport: "laptop", waitMs: 100, maxBytes: 0, out: dir}, srv.URL, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	b, err := os.ReadFile(filepath.Join(dir, "snapshot.html"))
	if err != nil {
		t.Fatalf("read snapshot.html: %v", err)
	}
	if !strings.Contains(string(b), "hello") {
		t.Fatalf("snapshot missing content:\n%s", b)
	}

	pngBytes, err := os.ReadFile(filepath.Join(dir, "screenshot.png"))
	if err != nil {
		t.Fatalf("read screenshot.png: %v", err)
	}
	if len(pngBytes) < 100 {
		t.Fatalf("screenshot looks too small: %d bytes", len(pngBytes))
	}
	// screenshot PNG magic
	if !strings.HasPrefix(string(pngBytes), "\x89PNG") {
		t.Fatalf("screenshot.png is not a PNG")
	}

	var meta map[string]any
	b, err = os.ReadFile(filepath.Join(dir, "capture.json"))
	if err != nil {
		t.Fatalf("read capture.json: %v", err)
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatalf("capture.json is not JSON: %v", err)
	}
	if meta["url"] == "" || meta["title"] == "" {
		t.Fatalf("capture.json missing url/title: %v", meta)
	}
}

func TestRunCapture_ErrorPath(t *testing.T) {
	dir := t.TempDir()
	flags := captureFlags{out: ""}
	var out strings.Builder
	cmd := &cobra.Command{}
	code, err := RunCapture(cmd, flags, "http://127.0.0.1:1/nope", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out.String()), &parsed); err != nil {
		t.Fatalf("error output is not JSON: %v\n%s", err, out.String())
	}
	if parsed["error"] == nil || parsed["error"] == "" {
		t.Fatalf("expected error reason in JSON, got %v", parsed)
	}
	if _, exists := parsed["html"]; exists {
		t.Fatalf("error output should not contain html: %v", parsed)
	}
	// --out with a failure must not create the snapshot files
	code, err = RunCapture(cmd, captureFlags{out: dir}, "http://127.0.0.1:1/nope", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "snapshot.html")); !os.IsNotExist(err) {
		t.Fatalf("snapshot.html should not exist on failure")
	}
}
