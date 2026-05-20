//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleAPISettingsSkillsMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPut, "/api/settings/skills", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsSkills(rec, req)

	// PUT goes to handleAPISettingsSkillsPut which requires config manager, but DELETE should be 405
	req2 := httptest.NewRequest(http.MethodDelete, "/api/settings/skills", nil)
	rec2 := httptest.NewRecorder()
	ws.handleAPISettingsSkills(rec2, req2)

	if rec2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for DELETE, got %d", rec2.Code)
	}
}

func TestHandleAPISettingsSkillsGetNoConfigManager(t *testing.T) {
	ws := &ReactWebServer{agent: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/settings/skills", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsSkills(rec, req)

	// When agent is nil, getConfigManager returns nil and the handler
	// falls back to a layered config manager, which may succeed (200)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (best-effort via layered config), got %d", rec.Code)
	}
}
