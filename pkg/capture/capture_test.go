//go:build !js

package capture

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

// --- fixtures ────────────────────────────────────────────────────────────

func fixtureImagePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(200 - x%40), G: uint8(120 + y%60), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture png: %v", err)
	}
	return buf.Bytes()
}

func fixtureFontPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	path := filepath.Join(
		filepath.Dir(filepath.Dir(filepath.Dir(thisFile))),
		"mockup", "kit", "fonts", "archivo-black.woff2",
	)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture font not available: %v", err)
	}
	return path
}

// appFixture serves a small app: external stylesheet, a cross-origin
// stylesheet (second server with CORS), an image, a font and a script that
// adds content to the DOM.
func appFixture(t *testing.T) (pageURL string, cleanup func()) {
	t.Helper()

	imgPNG := fixtureImagePNG(t, 8, 6)
	fontPath := fixtureFontPath(t)
	fontBytes, err := os.ReadFile(fontPath)
	if err != nil {
		t.Fatalf("read fixture font: %v", err)
	}

	css := `
@font-face {
  font-family: "Fixture Font";
  src: url("/fixture.woff2") format("woff2");
  font-weight: 400;
  font-style: normal;
}
body { font-family: "Fixture Font", sans-serif; margin: 0; background: #fff; color: #111; }
.card { background: #e8f5e9; padding: 1rem; border-radius: 8px; }
.injected-card { margin-top: 1rem; }
`

	crossOriginCSS := `body { letter-spacing: 0.02em; }
.badge { color: #b00020; font-weight: 700; }
`

	crossSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte(crossOriginCSS))
	}))

	pageHTML := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Fixture App</title>
<link rel="stylesheet" href="/style.css">
<link rel="stylesheet" href="%s/style.css">
</head>
<body>
<main id="main">
  <h1 class="title">Fixture App</h1>
  <div class="card injected-card">
    <p class="badge">Injected badge</p>
    <img src="/fixture.png" alt="fixture" width="64" height="48">
  </div>
</main>
<footer>Outside the main</footer>
<script>
  const el = document.createElement("div");
  el.className = "script-added";
  el.textContent = "script-added-marker";
  document.body.appendChild(el);
</script>
</body>
</html>`, crossSrv.URL)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(pageHTML))
		case "/style.css":
			w.Header().Set("Content-Type", "text/css")
			_, _ = w.Write([]byte(css))
		case "/fixture.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imgPNG)
		case "/fixture.woff2":
			w.Header().Set("Content-Type", "font/woff2")
			_, _ = w.Write(fontBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	return srv.URL, func() { srv.Close(); crossSrv.Close() }
}

// --- browser helpers ─────────────────────────────────────────────────────

func requireBrowser(t *testing.T) {
	t.Helper()
	// The fleet machine the platform runs this on has no configured proxy
	// (browse_url and the app check render through the managed renderer).
	// A configured proxy is not visible to the fixture: blank pages would
	// render, so drop it the same way an unproxied fleet machine has it.
	t.Setenv("SPROUT_HTTP_PROXY", "")
	s, err := webcontent.GetGlobalBrowser().OpenRodSession(context.Background())
	if err != nil {
		t.Skipf("no browser available: %v", err)
	}
	s.Close()
}

func openPage(t *testing.T) (*webcontent.RodSession, context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	r := webcontent.GetGlobalBrowser()
	s, err := r.OpenRodSession(ctx)
	if err != nil {
		cancel()
		t.Skipf("no browser available: %v", err)
	}
	t.Cleanup(func() { cancel(); s.Close() })
	return s, ctx, cancel
}

// openSnapshot loads a snapshot into the browser and screenshots it.
func openSnapshot(t *testing.T, htmlText string) []byte {
	t.Helper()
	dir := t.TempDir()
	snapPath := filepath.Join(dir, "snapshot.html")
	if err := os.WriteFile(snapPath, []byte(htmlText), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	s, _, _ := openPage(t)
	page := s.Page
	if err := page.Navigate("file://" + snapPath); err != nil {
		t.Fatalf("navigate to snapshot: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Fatalf("wait load: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	shot, err := page.Screenshot(false, nil)
	if err != nil {
		t.Fatalf("screenshot snapshot: %v", err)
	}
	return shot
}

// --- pixel comparison ────────────────────────────────────────────────────

func diffScreenshots(t *testing.T, a, b []byte) {
	t.Helper()
	imgA, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatalf("decode original screenshot: %v", err)
	}
	imgB, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode snapshot screenshot: %v", err)
	}
	boundsA := imgA.Bounds()
	boundsB := imgB.Bounds()
	if boundsA.Dx() != boundsB.Dx() || boundsA.Dy() != boundsB.Dy() {
		t.Fatalf("screenshot size mismatch: %v vs %v", boundsA, boundsB)
	}
	var diffSum uint64
	var diffPixels int
	totalPixels := 0
	for y := boundsA.Min.Y; y < boundsA.Max.Y; y++ {
		for x := boundsA.Min.X; x < boundsA.Max.X; x++ {
			paR, paG, paB, _ := imgA.At(x, y).RGBA()
			pbR, pbG, pbB, _ := imgB.At(x, y).RGBA()
			// per-channel abs diff (0..65535), summed across r,g,b
			d := abs(int(paR>>8)-int(pbR>>8)) + abs(int(paG>>8)-int(pbG>>8)) + abs(int(paB>>8)-int(pbB>>8))
			diffSum += uint64(d)
			if d > 24 {
				diffPixels++
			}
			totalPixels++
		}
	}
	if totalPixels == 0 {
		t.Fatal("screenshot has zero pixels")
	}
	meanDiff := float64(diffSum) / float64(totalPixels)
	diffFraction := float64(diffPixels) / float64(totalPixels)
	t.Logf("mean diff %.2f, fraction %.2f%%", meanDiff, diffFraction*100)
	if meanDiff > 12 || diffFraction > 0.03 {
		t.Fatalf("snapshot diverges from original: mean diff %.2f, fraction %.2f%%", meanDiff, diffFraction*100)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// --- tests ───────────────────────────────────────────────────────────────

func TestCapture_StandaloneSnapshot(t *testing.T) {
	requireBrowser(t)
	pageURL, cleanup := appFixture(t)
	defer cleanup()

	res, err := Run(context.Background(), Options{URL: pageURL, Viewport: "laptop", WaitMs: 100, MaxBytes: 400000})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if res.HTML == "" {
		t.Fatal("snapshot is empty")
	}
	// script-added content present, script absent
	if !strings.Contains(res.HTML, "script-added-marker") {
		t.Error("snapshot missing script-added content")
	}
	if strings.Contains(res.HTML, "<script") {
		t.Error("snapshot contains <script>")
	}
	// styles inlined
	if !strings.Contains(res.HTML, ".card") {
		t.Error("snapshot missing external stylesheet rule (.card)")
	}
	if !strings.Contains(res.HTML, ".badge") {
		t.Error("snapshot missing cross-origin stylesheet rule (.badge)")
	}
	// image inlined as data URL
	if !strings.Contains(res.HTML, "data:image/png;base64") {
		t.Error("snapshot missing image data URL")
	}
	// font inlined as data URL
	if !strings.Contains(res.HTML, "data:font/woff2") {
		t.Error("snapshot missing font data URL")
	}
	if res.Title != "Fixture App" {
		t.Errorf("title = %q, want Fixture App", res.Title)
	}
	if res.HTMLBytes <= 0 {
		t.Error("html_bytes should be > 0")
	}
	if res.Trimmed {
		t.Error("trimmed should be false for a small fixture")
	}
	// Standalone: load snapshot, compare screenshots.
	originalShot := mustOriginalScreenshot(t, pageURL)
	snapshotShot := openSnapshot(t, res.HTML)
	diffScreenshots(t, originalShot, snapshotShot)
}

func mustOriginalScreenshot(t *testing.T, pageURL string) []byte {
	t.Helper()
	s, _, _ := openPage(t)
	page := s.Page
	if err := page.Navigate(pageURL); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Fatalf("wait load: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	shot, err := page.Screenshot(false, nil)
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	return shot
}

func TestCapture_SelectorLimitsContent(t *testing.T) {
	requireBrowser(t)
	pageURL, cleanup := appFixture(t)
	defer cleanup()

	res, err := Run(context.Background(), Options{URL: pageURL, Viewport: "laptop", WaitMs: 100, Selector: "#main", MaxBytes: 400000})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !strings.Contains(res.HTML, "Fixture App") {
		t.Error("snapshot missing selector content (Fixture App)")
	}
	if strings.Contains(res.HTML, "Outside the main") {
		t.Error("snapshot contains content outside the selector")
	}
}

func TestCapture_MaxBytesTrims(t *testing.T) {
	requireBrowser(t)
	pageURL, cleanup := appFixture(t)
	defer cleanup()

	res, err := Run(context.Background(), Options{URL: pageURL, Viewport: "laptop", WaitMs: 100, MaxBytes: 800})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	// An 800-byte budget is far below the full snapshot: fonts dropped, images
	// downscaled, then trimmed to the largest visible section.
	if !res.Trimmed {
		t.Error("trimmed should be true with an 800-byte budget")
	}
	if !strings.Contains(res.HTML, "Injected badge") {
		t.Error("trimmed snapshot should keep the largest visible section (main)")
	}
	if strings.Contains(res.HTML, "Outside the main") {
		t.Error("trimmed snapshot should not keep the small footer section")
	}
}

func TestCapture_ErrorJSONShape(t *testing.T) {
	// Navigation failure: 127.0.0.1:1 is not listening.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := Run(ctx, Options{URL: "http://127.0.0.1:1/nope", Viewport: "laptop", WaitMs: 100, MaxBytes: 400000})
	if err == nil {
		t.Skip("expected navigation failure; environment answered the request")
	}
	// The one-line reason comes through the error string.
	if strings.TrimSpace(err.Error()) == "" {
		t.Error("error should carry a one-line reason")
	}
}

// --- cmd-level checks (not browser-dependent) ─────────────────────────────

func TestCaptureJSONEncoding(t *testing.T) {
	res := &Result{
		URL:                 "http://example.com",
		FinalURL:            "http://example.com/",
		Title:               "Example",
		Viewport:            "laptop",
		HTML:                "<html></html>",
		HTMLBytes:           14,
		ScreenshotPNGBase64: "aGVsbG8=",
		DurationMs:          12,
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"url", "final_url", "title", "viewport", "html", "html_bytes", "trimmed", "screenshot_png_base64", "duration_ms"} {
		if _, ok := back[key]; !ok {
			t.Errorf("missing key %q in JSON output", key)
		}
	}
}
