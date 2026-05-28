//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestSettingsAPI_RiskProfileRoundTrip exercises the GET→PUT→GET loop the
// webui settings panel uses for `risk_profile`. Without the dedicated PUT
// handler the value would silently round-trip through Other (string fall-
// through) and may or may not appear in the GET response; this test pins
// down the contract.
func TestSettingsAPI_RiskProfileRoundTrip(t *testing.T) {
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolatedHome, ".config"))
	t.Setenv("USERPROFILE", isolatedHome)

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.getOrCreateClientContext("test-client")

	// PUT a non-default profile at the global layer.
	body := `{"risk_profile": "readonly"}`
	rec := makeSettingsRequest(ws, http.MethodPut, "/api/settings?layer=global", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT failed: %d: %s", rec.Code, rec.Body.String())
	}

	// GET the global layer and confirm the value survives.
	req := httptest.NewRequest(http.MethodGet, "/api/settings?layer=global", nil)
	req.Header.Set("X-Sprout-Client-ID", "test-client")
	rec = httptest.NewRecorder()
	ws.handleAPISettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET failed: %d: %s", rec.Code, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not valid JSON: %v\n%s", err, rec.Body.String())
	}
	if got["risk_profile"] != "readonly" {
		t.Errorf("round-trip lost risk_profile: got %v, want %q", got["risk_profile"], "readonly")
	}
}

// TestSettingsAPI_RiskProfileClearRoundTrip mirrors the dropdown's "use the
// built-in default" choice: PUT an empty string and confirm the GET reflects
// it (or omits the key) — the user must be able to clear an override.
func TestSettingsAPI_RiskProfileClearRoundTrip(t *testing.T) {
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolatedHome, ".config"))
	t.Setenv("USERPROFILE", isolatedHome)

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.getOrCreateClientContext("test-client")

	// First set a profile.
	if rec := makeSettingsRequest(ws, http.MethodPut, "/api/settings?layer=global", `{"risk_profile": "cautious"}`); rec.Code != http.StatusOK {
		t.Fatalf("setup PUT failed: %d: %s", rec.Code, rec.Body.String())
	}
	// Then clear by sending an empty string.
	if rec := makeSettingsRequest(ws, http.MethodPut, "/api/settings?layer=global", `{"risk_profile": ""}`); rec.Code != http.StatusOK {
		t.Fatalf("clear PUT failed: %d: %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/settings?layer=global", nil)
	req.Header.Set("X-Sprout-Client-ID", "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET failed: %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if v, present := got["risk_profile"]; present && v != "" {
		t.Errorf("after clear: risk_profile should be empty or absent, got %v", v)
	}
}

// TestSettingsAPI_RiskProfileRejectsUnknown locks down the PUT validation
// added in settings_api_put.go — the dropdown ships valid names, but a
// hand-rolled API call with a typo must not silently persist a bogus value
// that would then fall back to "default" at runtime.
func TestSettingsAPI_RiskProfileRejectsUnknown(t *testing.T) {
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolatedHome, ".config"))
	t.Setenv("USERPROFILE", isolatedHome)

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.getOrCreateClientContext("test-client")

	rec := makeSettingsRequest(ws, http.MethodPut, "/api/settings?layer=global", `{"risk_profile": "yolo"}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected rejection for unknown profile, got 200: %s", rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "risk_profile") {
		t.Errorf("error body should mention risk_profile, got: %s", rec.Body.String())
	}
}
