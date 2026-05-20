//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleAPISettingsSubagentTypesMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPatch, "/api/settings/subagent-types", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsSubagentTypes(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPISettingsSubagentTypesGetNoConfigManager(t *testing.T) {
	ws := &ReactWebServer{agent: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/settings/subagent-types", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsSubagentTypes(rec, req)

	// Best-effort via layered config manager
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (best-effort via layered config), got %d", rec.Code)
	}
}

func TestHandleAPISettingsSubagentTypesPutMissingName(t *testing.T) {
	ws := &ReactWebServer{agent: nil}
	req := httptest.NewRequest(http.MethodPut, "/api/settings/subagent-types/", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPISettingsSubagentTypes(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", rec.Code)
	}
}

func TestHandleAPISettingsSubagentTypesDeleteMissingName(t *testing.T) {
	ws := &ReactWebServer{agent: nil}
	req := httptest.NewRequest(http.MethodDelete, "/api/settings/subagent-types/", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsSubagentTypes(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", rec.Code)
	}
}

func TestExtractPathSegmentSubagents(t *testing.T) {
	tests := []struct {
		path   string
		prefix string
		want   string
	}{
		{"/api/settings/subagent-types/tester", "/api/settings/subagent-types/", "tester"},
		{"/api/settings/subagent-types/", "/api/settings/subagent-types/", ""},
		{"/api/settings/subagent-types", "/api/settings/subagent-types/", ""},
		{"/api/settings/providers/my-provider", "/api/settings/providers/", "my-provider"},
		{"/api/settings/providers/", "/api/settings/providers/", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractPathSegment(tt.path, tt.prefix)
			if got != tt.want {
				t.Errorf("extractPathSegment(%q, %q) = %q, want %q", tt.path, tt.prefix, got, tt.want)
			}
		})
	}
}
