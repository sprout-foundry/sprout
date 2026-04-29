package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestRootAssetHandlersServeEmbeddedFiles(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")

	tests := []struct {
		name        string
		path        string
		handler     func(http.ResponseWriter, *http.Request)
		contentType string
	}{
		{
			name:        "manifest",
			path:        "/manifest.json",
			handler:     server.handleManifest,
			contentType: "application/manifest+json",
		},
		{
			name:        "browserconfig",
			path:        "/browserconfig.xml",
			handler:     server.handleBrowserConfig,
			contentType: "application/xml",
		},
		{
			name:        "asset manifest",
			path:        "/asset-manifest.json",
			handler:     server.handleAssetManifest,
			contentType: "application/json",
		},
		{
			name:        "icon 192",
			path:        "/icon-192.png",
			handler:     server.handleIcon192,
			contentType: "image/png",
		},
		{
			name:        "icon 512",
			path:        "/icon-512.png",
			handler:     server.handleIcon512,
			contentType: "image/png",
		},
		{
			name:        "logo mark",
			path:        "/logo-mark.svg",
			handler:     server.handleLogoMark,
			contentType: "image/svg+xml",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			tc.handler(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rec.Code)
			}
			if rec.Body.Len() == 0 {
				t.Fatalf("expected non-empty body for %s", tc.path)
			}

			gotType := rec.Header().Get("Content-Type")
			if !strings.HasPrefix(gotType, tc.contentType) {
				t.Fatalf("expected content type prefix %q, got %q", tc.contentType, gotType)
			}
		})
	}
}

func TestStaticFilesServesHashedMainBundle(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")

	type manifest struct {
		Files map[string]string `json:"files"`
	}

	rawManifest, err := readStaticFile("asset-manifest.json")
	if err != nil {
		t.Fatalf("failed to read embedded asset-manifest.json: %v", err)
	}

	var parsed manifest
	if err := json.Unmarshal(rawManifest, &parsed); err != nil {
		t.Fatalf("failed to parse asset-manifest.json: %v", err)
	}

	mainJS := parsed.Files["main.js"]
	if mainJS == "" {
		t.Fatal("asset-manifest.json missing files.main.js")
	}

	req := httptest.NewRequest(http.MethodGet, mainJS, nil)
	rec := httptest.NewRecorder()
	server.handleStaticFiles(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty JavaScript bundle body")
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") &&
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/javascript") {
		t.Fatalf("unexpected JS content type: %q", rec.Header().Get("Content-Type"))
	}
}

func TestEmbeddedIndexReferencesAvailableRootAssets(t *testing.T) {
	indexHTML, err := readStaticFile("index.html")
	if err != nil {
		t.Fatalf("failed to read embedded index.html: %v", err)
	}

	html := string(indexHTML)
	rootAssetPattern := regexp.MustCompile(`(?:href|src)="/([^"/]+\.(?:svg|png|json|xml|ico))"`)
	matches := rootAssetPattern.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		t.Fatal("expected embedded index.html to reference at least one root asset")
	}

	for _, match := range matches {
		assetName := path.Base(match[1])
		server := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
		if _, err := server.readRootAsset(assetName); err != nil {
			t.Fatalf("embedded index.html references missing root asset %q: %v", assetName, err)
		}
	}
}

func TestHandleAssetsServesEmbeddedFilesWithMIMETypes(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")

	// Discover the hashed index JS bundle name from embedded index.html
	indexHTML, err := readStaticFile("index.html")
	if err != nil {
		t.Fatalf("failed to read embedded index.html: %v", err)
	}
	re := regexp.MustCompile(`src="/assets/(index-[^"]+\.js)"`)
	matches := re.FindSubmatch(indexHTML)
	if len(matches) < 2 {
		t.Fatal("could not find index JS bundle reference in index.html")
	}
	indexJSBundle := "/assets/" + string(matches[1])

	tests := []struct {
		name        string
		path        string
		contentType string
	}{
		{
			name:        "index js bundle",
			path:        indexJSBundle,
			contentType: "text/javascript",
		},
		{
			name:        "react js bundle",
			path:        "/assets/react-CRB3T2We.js",
			contentType: "text/javascript",
		},
		{
			name:        "codemirror js bundle",
			path:        "/assets/codemirror-Cp_jJeyJ.js",
			contentType: "text/javascript",
		},
		{
			name:        "index css bundle",
			path:        "/assets/index-Bl_vRp-X.css",
			contentType: "text/css",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			server.handleAssets(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rec.Code)
			}
			if rec.Body.Len() == 0 {
				t.Fatalf("expected non-empty body for %s", tc.path)
			}

			gotType := rec.Header().Get("Content-Type")
			if !strings.HasPrefix(gotType, tc.contentType) {
				t.Fatalf("expected content type prefix %q, got %q", tc.contentType, gotType)
			}

			// Vite-hashed assets should be cached aggressively
			cacheControl := rec.Header().Get("Cache-Control")
			if !strings.Contains(cacheControl, "immutable") {
				t.Fatalf("expected immutable cache-control for hashed asset, got %q", cacheControl)
			}
		})
	}
}

func TestHandleAssetsReturns404ForNonExistentFile(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")

	req := httptest.NewRequest(http.MethodGet, "/assets/nonexistent.js", nil)
	rec := httptest.NewRecorder()

	server.handleAssets(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing asset, got %d", rec.Code)
	}
}

func TestHandleIndexDoesNotServeAPIPaths(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")

	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	rec := httptest.NewRecorder()

	server.handleIndex(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for API path fallback, got %d", rec.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "<!doctype html") {
		t.Fatal("expected API fallback to avoid serving index html")
	}
}
