//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleAPIBootstrap_PlatformURL pins the SP-016 P0.3 contract:
// platformURL is served from SPROUT_PLATFORM_URL and the field is omitted
// (not an empty string) when the env var is unset — the editor keeps its
// relative-exit behavior in that case.
func TestHandleAPIBootstrap_PlatformURL(t *testing.T) {
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())
	server := newSyncTestWebServer(t, t.TempDir())
	request := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)

	// Unset → the field must be omitted entirely.
	t.Setenv("SPROUT_PLATFORM_URL", "")
	rec := httptest.NewRecorder()
	server.handleAPIBootstrap(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("bootstrap body not JSON: %v", err)
	}
	if _, ok := payload["platformURL"]; ok {
		t.Errorf("empty SPROUT_PLATFORM_URL must omit the field, got %s", payload["platformURL"])
	}

	// Set → the field is present with the exact value.
	t.Setenv("SPROUT_PLATFORM_URL", "https://platform.example.dev")
	rec = httptest.NewRecorder()
	server.handleAPIBootstrap(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var config RuntimeConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if config.PlatformURL != "https://platform.example.dev" {
		t.Errorf("platformURL = %q, want %q", config.PlatformURL, "https://platform.example.dev")
	}

	// Other fields stay intact.
	if config.AppMode != "local" {
		t.Errorf("appMode = %q, want local", config.AppMode)
	}
}
