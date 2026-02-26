package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/events"
)

func TestRootAssetHandlersServeEmbeddedFiles(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

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
	server := NewReactWebServer(nil, events.NewEventBus(), 0)

	type manifest struct {
		Files map[string]string `json:"files"`
	}

	rawManifest, err := staticFiles.ReadFile("static/asset-manifest.json")
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
