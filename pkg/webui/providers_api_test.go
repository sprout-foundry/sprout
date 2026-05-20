//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleAPIProvidersMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/providers", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIProviders(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIProvidersNoAgent(t *testing.T) {
	ws := &ReactWebServer{agent: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIProviders(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestModelsForProviderFromAPI(t *testing.T) {
	// This function makes real API calls. With no config it returns nil
	// or falls back to catalog. We test that it doesn't panic.
	t.Skip("requires network access to provider APIs - skipped to avoid flaky CI")
	result := modelsForProviderFromAPI("")
	_ = result
}
