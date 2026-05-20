//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestHandleAPISettingsProvidersMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPatch, "/api/settings/providers", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsProviders(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPISettingsProvidersGetNoConfigManager(t *testing.T) {
	ws := &ReactWebServer{agent: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/settings/providers", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsProviders(rec, req)

	// Best-effort via layered config manager
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (best-effort via layered config), got %d", rec.Code)
	}
}

func TestHandleAPISettingsProvidersPostNoConfigManager(t *testing.T) {
	ws := &ReactWebServer{agent: nil}
	req := httptest.NewRequest(http.MethodPost, "/api/settings/providers", strings.NewReader(`{"name":"test"}`))
	rec := httptest.NewRecorder()
	ws.handleAPISettingsProviders(rec, req)

	// With nil agent, we may get a 400 for validation or 200/503 via layered config
	// The important thing is it doesn't panic
	if rec.Code == http.StatusInternalServerError {
		t.Fatalf("unexpected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestValidateCustomProvider(t *testing.T) {
	t.Run("valid provider", func(t *testing.T) {
		p := configuration.CustomProviderConfig{
			Name:     "my-provider",
			Endpoint: "https://api.example.com",
		}
		if err := validateCustomProvider(p); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		p := configuration.CustomProviderConfig{
			Endpoint: "https://api.example.com",
		}
		err := validateCustomProvider(p)
		if err == nil {
			t.Fatal("expected error for missing name")
		}
		if !strings.Contains(err.Error(), "name") {
			t.Fatalf("expected error about name, got %v", err)
		}
	})

	t.Run("missing endpoint", func(t *testing.T) {
		p := configuration.CustomProviderConfig{
			Name: "my-provider",
		}
		err := validateCustomProvider(p)
		if err == nil {
			t.Fatal("expected error for missing endpoint")
		}
		if !strings.Contains(err.Error(), "endpoint") {
			t.Fatalf("expected error about endpoint, got %v", err)
		}
	})

	t.Run("negative context size", func(t *testing.T) {
		p := configuration.CustomProviderConfig{
			Name:        "my-provider",
			Endpoint:    "https://api.example.com",
			ContextSize: -1,
		}
		err := validateCustomProvider(p)
		if err == nil {
			t.Fatal("expected error for negative context size")
		}
		if !strings.Contains(err.Error(), "context_size") {
			t.Fatalf("expected error about context_size, got %v", err)
		}
	})

	t.Run("zero context size is valid", func(t *testing.T) {
		p := configuration.CustomProviderConfig{
			Name:        "my-provider",
			Endpoint:    "https://api.example.com",
			ContextSize: 0,
		}
		if err := validateCustomProvider(p); err != nil {
			t.Fatalf("expected nil for zero context size, got %v", err)
		}
	})

	t.Run("post_missing_name", func(t *testing.T) {
		ws := &ReactWebServer{agent: nil}
		req := httptest.NewRequest(http.MethodPost, "/api/settings/providers", strings.NewReader(`{"endpoint":"https://example.com"}`))
		rec := httptest.NewRecorder()
		ws.handleAPISettingsProviders(rec, req)
		// May return 400 for validation or best-effort response - just verify no panic
		if rec.Code == http.StatusInternalServerError {
			t.Fatalf("unexpected 500, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestHandleAPISettingsProvidersPutMissingName(t *testing.T) {
	ws := &ReactWebServer{agent: nil}
	req := httptest.NewRequest(http.MethodPut, "/api/settings/providers/", strings.NewReader(`{"name":"test","endpoint":"https://example.com"}`))
	rec := httptest.NewRecorder()
	ws.handleAPISettingsProviders(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", rec.Code)
	}
}

func TestHandleAPISettingsProvidersDeleteMissingName(t *testing.T) {
	ws := &ReactWebServer{agent: nil}
	req := httptest.NewRequest(http.MethodDelete, "/api/settings/providers/", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsProviders(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", rec.Code)
	}
}
