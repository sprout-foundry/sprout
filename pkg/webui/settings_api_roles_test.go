//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// newRolesTestServer builds an isolated ReactWebServer with a temp home so
// the global settings layer round-trips against a throwaway config file, the
// same harness the risk_profile round-trip tests use.
func newRolesTestServer(t *testing.T) *ReactWebServer {
	t.Helper()
	isolatedHome := t.TempDir()
	setTestHome(t, isolatedHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolatedHome, ".config"))
	t.Setenv("USERPROFILE", isolatedHome)
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.getOrCreateClientContext("test-client")
	return ws
}

func getRolesFromResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]configuration.RoleConfig {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET failed: %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not valid JSON: %v\n%s", err, rec.Body.String())
	}
	raw, ok := got["roles"]
	if !ok || raw == nil {
		return nil
	}
	// Round-trip through JSON so we compare against typed RoleConfig rather
	// than a raw map[string]interface{} of float64s/strings.
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("re-encode roles: %v", err)
	}
	var roles map[string]configuration.RoleConfig
	if err := json.Unmarshal(b, &roles); err != nil {
		t.Fatalf("roles not a map of {provider,model}: %v\n%s", err, raw)
	}
	return roles
}

// TestSettingsAPI_RolesRoundTrip exercises the GET→PUT→GET loop the settings
// panel uses for the roles section. Without a dedicated applier the
// "roles" key would be silently dropped with an "Unknown fields ignored"
// warning; this pins down that it now persists and survives the GET.
func TestSettingsAPI_RolesRoundTrip(t *testing.T) {
	ws := newRolesTestServer(t)

	body := `{"roles": {"coder": {"provider": "anthropic", "model": "claude-sonnet-4-6"}, "planner": {"model": "gpt-5"}}}`
	rec := makeSettingsRequest(ws, http.MethodPut, "/api/settings?layer=global", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT failed: %d: %s", rec.Code, rec.Body.String())
	}
	var putResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &putResp); err != nil {
		t.Fatalf("PUT response not valid JSON: %v\n%s", err, rec.Body.String())
	}
	if w, ok := putResp["warnings"]; ok {
		t.Errorf("PUT reported warnings (roles should be a known key): %v", w)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/settings?layer=global", nil)
	req.Header.Set("X-Sprout-Client-ID", "test-client")
	rec = httptest.NewRecorder()
	ws.handleAPISettings(rec, req)
	roles := getRolesFromResponse(t, rec)
	if roles == nil {
		t.Fatalf("GET omitted roles section after PUT")
	}
	if c := roles["coder"]; c.Provider != "anthropic" || c.Model != "claude-sonnet-4-6" {
		t.Errorf("coder role = %+v, want provider=anthropic model=claude-sonnet-4-6", c)
	}
	if p := roles["planner"]; p.Provider != "" || p.Model != "gpt-5" {
		t.Errorf("planner role = %+v, want provider=\"\" model=gpt-5", p)
	}
}

// TestSettingsAPI_RolesNilClearsSection confirms a null patch clears the
// section (parity with risk_profiles) so the UI can remove all role overrides.
func TestSettingsAPI_RolesNilClearsSection(t *testing.T) {
	ws := newRolesTestServer(t)

	// Seed a role first.
	if rec := makeSettingsRequest(ws, http.MethodPut, "/api/settings?layer=global",
		`{"roles": {"coder": {"provider": "anthropic", "model": "claude-sonnet-4-6"}}}`); rec.Code != http.StatusOK {
		t.Fatalf("setup PUT failed: %d: %s", rec.Code, rec.Body.String())
	}

	// Then clear with an explicit null.
	if rec := makeSettingsRequest(ws, http.MethodPut, "/api/settings?layer=global", `{"roles": null}`); rec.Code != http.StatusOK {
		t.Fatalf("clear PUT failed: %d: %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/settings?layer=global", nil)
	req.Header.Set("X-Sprout-Client-ID", "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISettings(rec, req)
	roles := getRolesFromResponse(t, rec)
	if len(roles) != 0 {
		t.Errorf("after null patch: roles should be empty/absent, got %v", roles)
	}
}

// TestSettingsAPI_RolesDropsTestProviderAndEmpty asserts the per-entry
// defenses: a literal "test" provider is dropped, and a fully-empty entry is
// dropped, so an all-empty patch stores nothing.
func TestSettingsAPI_RolesDropsTestProviderAndEmpty(t *testing.T) {
	ws := newRolesTestServer(t)

	body := `{
		"roles": {
			"coder":    {"provider": "test", "model": "should-be-dropped"},
			"summarizer": {"provider": "", "model": ""},
			"reviewer":  {"provider": "openai", "model": "gpt-5"}
		}
	}`
	if rec := makeSettingsRequest(ws, http.MethodPut, "/api/settings?layer=global", body); rec.Code != http.StatusOK {
		t.Fatalf("PUT failed: %d: %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/settings?layer=global", nil)
	req.Header.Set("X-Sprout-Client-ID", "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISettings(rec, req)
	roles := getRolesFromResponse(t, rec)
	if _, ok := roles["coder"]; ok {
		t.Errorf("coder with test provider should be dropped, got %v", roles["coder"])
	}
	if _, ok := roles["summarizer"]; ok {
		t.Errorf("empty summarizer entry should be dropped, got %v", roles["summarizer"])
	}
	if r := roles["reviewer"]; r.Provider != "openai" || r.Model != "gpt-5" {
		t.Errorf("reviewer role = %+v, want provider=openai model=gpt-5", r)
	}
}

// TestApplyRolesSettings_Unit pins the applier contract directly (no HTTP):
// value plumbing, the known-key marking, nil clearing, and the drop rules.
func TestApplyRolesSettings_Unit(t *testing.T) {
	t.Run("persists and marks known", func(t *testing.T) {
		cfg := configuration.NewConfig()
		patch := map[string]interface{}{
			"roles": map[string]interface{}{
				"coder":  map[string]interface{}{"provider": "anthropic", "model": "claude-sonnet-4-6"},
				"commit": map[string]interface{}{"model": "gpt-5-mini"},
			},
			"totally_unknown_key": "x",
		}
		unknown, err := applyPartialSettings(cfg, patch)
		if err != nil {
			t.Fatalf("applyPartialSettings: %v", err)
		}
		for _, u := range unknown {
			if u == "roles" {
				t.Error("roles should not be reported as an unknown key")
			}
		}
		if len(unknown) != 1 || unknown[0] != "totally_unknown_key" {
			t.Errorf("unknown = %v, want [totally_unknown_key]", unknown)
		}
		if got := cfg.Roles["coder"]; got.Provider != "anthropic" || got.Model != "claude-sonnet-4-6" {
			t.Errorf("coder = %+v, want anthropic/claude-sonnet-4-6", got)
		}
		if got := cfg.Roles["commit"]; got.Provider != "" || got.Model != "gpt-5-mini" {
			t.Errorf("commit = %+v, want \"\"/gpt-5-mini", got)
		}
	})

	t.Run("nil clears the section", func(t *testing.T) {
		cfg := configuration.NewConfig()
		cfg.Roles = map[string]configuration.RoleConfig{"coder": {Provider: "anthropic", Model: "m"}}
		unknown, err := applyPartialSettings(cfg, map[string]interface{}{"roles": nil})
		if err != nil {
			t.Fatalf("applyPartialSettings: %v", err)
		}
		if len(unknown) != 0 {
			t.Errorf("unexpected unknown keys: %v", unknown)
		}
		if cfg.Roles != nil {
			t.Errorf("Roles should be nil after null patch, got %v", cfg.Roles)
		}
	})

	t.Run("all-empty map stores nothing", func(t *testing.T) {
		cfg := configuration.NewConfig()
		if _, err := applyPartialSettings(cfg, map[string]interface{}{
			"roles": map[string]interface{}{
				"coder":  map[string]interface{}{},
				"commit": map[string]interface{}{"provider": "test", "model": "x"},
			},
		}); err != nil {
			t.Fatalf("applyPartialSettings: %v", err)
		}
		if cfg.Roles != nil {
			t.Errorf("Roles should be nil when every entry is dropped, got %v", cfg.Roles)
		}
	})

	t.Run("absent roles is a no-op", func(t *testing.T) {
		cfg := configuration.NewConfig()
		cfg.Roles = map[string]configuration.RoleConfig{"coder": {Model: "kept"}}
		unknown, err := applyPartialSettings(cfg, map[string]interface{}{"skip_prompt": true})
		if err != nil {
			t.Fatalf("applyPartialSettings: %v", err)
		}
		if len(unknown) != 0 {
			t.Errorf("unexpected unknown keys: %v", unknown)
		}
		if got := cfg.Roles["coder"]; got.Model != "kept" {
			t.Errorf("Roles mutated by an unrelated patch: %+v", got)
		}
	})
}

// TestSanitizedConfig_IncludesRoles confirms the GET payload passes the
// roles section through sanitization (it carries no secrets).
func TestSanitizedConfig_IncludesRoles(t *testing.T) {
	cfg := configuration.NewConfig()
	cfg.Roles = map[string]configuration.RoleConfig{"coder": {Provider: "anthropic", Model: "claude-sonnet-4-6"}}
	out := sanitizedConfig(cfg)
	roles, ok := out["roles"]
	if !ok {
		t.Fatal("sanitizedConfig omitted the roles section")
	}
	m, ok := roles.(map[string]configuration.RoleConfig)
	if !ok {
		t.Fatalf("roles has unexpected type %T", roles)
	}
	if c := m["coder"]; c.Provider != "anthropic" || c.Model != "claude-sonnet-4-6" {
		t.Errorf("sanitized roles[coder] = %+v, want anthropic/claude-sonnet-4-6", c)
	}
}
