//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
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

func TestHandleAPISettingsSubagentTypesPutRejected(t *testing.T) {
	ws := &ReactWebServer{agent: nil}
	req := httptest.NewRequest(http.MethodPut, "/api/settings/subagent-types/", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPISettingsSubagentTypes(rec, req)

	// Personas are catalog-fixed; PUT is no longer supported.
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for PUT, got %d", rec.Code)
	}
}

func TestHandleAPISettingsSubagentTypesDeleteRejected(t *testing.T) {
	ws := &ReactWebServer{agent: nil}
	req := httptest.NewRequest(http.MethodDelete, "/api/settings/subagent-types/", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsSubagentTypes(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for DELETE, got %d", rec.Code)
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

// TestSubagentTypeSourceSerializes pins that the settings API exposes a
// persona's source (SP-175), so the WebUI picker can label a custom persona
// without a second endpoint.
func TestSubagentTypeSourceSerializes(t *testing.T) {
	data, err := json.Marshal(configuration.SubagentType{
		ID: "foundry_planner", Name: "Foundry Planner", Source: "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"source":"user"`) {
		t.Errorf("source missing from the serialized persona: %s", data)
	}

	// A core entry built before sources were tracked omits the key.
	core, err := json.Marshal(configuration.SubagentType{ID: "designer"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(core), `"source"`) {
		t.Errorf("empty source should be omitted: %s", core)
	}
}
