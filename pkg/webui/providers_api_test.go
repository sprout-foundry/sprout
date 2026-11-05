//go:build !js

package webui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
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
	ws.providerModels.fetch = func(context.Context, api.ClientType) ([]api.ModelInfo, error) {
		return nil, errors.New("no network in unit tests")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIProviders(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
