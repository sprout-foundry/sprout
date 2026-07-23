//go:build !js

package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestSettingsProvidersPutLoadsProviderFromDisk(t *testing.T) {
	configDir := setupProviderHandlerEnv(t)
	writeProviderFile(t, configDir, configuration.CustomProviderConfig{Name: "disk-only", Endpoint: "https://old.example.com"})

	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPut, "/api/settings/providers/disk-only", strings.NewReader(`{"endpoint":"https://new.example.com"}`))
	rec := httptest.NewRecorder()
	ws.handleAPISettingsProvidersPut(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	provider := readProviderFile(t, configDir, "disk-only")
	if provider.Endpoint != "https://new.example.com/v1/chat/completions" {
		t.Fatalf("persisted endpoint = %q, want %q", provider.Endpoint, "https://new.example.com/v1/chat/completions")
	}
}

func TestSettingsProvidersDeleteLoadsProviderFromDisk(t *testing.T) {
	configDir := setupProviderHandlerEnv(t)
	writeProviderFile(t, configDir, configuration.CustomProviderConfig{Name: "disk-only", Endpoint: "https://example.com"})

	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodDelete, "/api/settings/providers/disk-only", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsProvidersDelete(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(configDir, configuration.ProvidersDirName, "disk-only.json")); !os.IsNotExist(err) {
		t.Fatalf("provider file still exists after DELETE: %v", err)
	}
}

func TestSettingsProvidersPutPersistenceFailureReturns500(t *testing.T) {
	// SaveCustomProvider resolves its write path via getDefaultProvidersDir()
	// (HOME-based, ignoring SPROUT_CONFIG). Manager.EnrichCustomProviders
	// reads from the manager's configDir (SPROUT_CONFIG-resolved). To
	// force a Save failure while keeping the Enrich path working, we
	// point HOME at a dir where the providers path is blocked by a
	// regular file, and SPROUT_CONFIG at a separate dir that holds a
	// normal providers directory + provider JSON.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	scopedDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", scopedDir)

	// Write provider JSON under the SPROUT_CONFIG providers dir — the
	// manager's Enrich path will find it.
	writeProviderFile(t, scopedDir, configuration.CustomProviderConfig{Name: "disk-only", Endpoint: "https://old.example.com"})

	// Block SaveCustomProvider by making HOME/.config/sprout/providers
	// be a regular file. MkdirAll inside GetCustomProviderPath will
	// refuse to create the directory because the path already exists
	// as a non-directory.
	blockedProviders := filepath.Join(homeDir, ".config", "sprout", configuration.ProvidersDirName)
	if err := os.MkdirAll(filepath.Join(homeDir, ".config", "sprout"), 0700); err != nil {
		t.Fatalf("create HOME config dir: %v", err)
	}
	if err := os.WriteFile(blockedProviders, []byte("not-a-directory"), 0600); err != nil {
		t.Fatalf("create blocking file at providers path: %v", err)
	}

	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPut, "/api/settings/providers/disk-only", strings.NewReader(`{"endpoint":"https://new.example.com"}`))
	rec := httptest.NewRecorder()
	ws.handleAPISettingsProvidersPut(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("PUT status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	// Stable user-facing message: details logged server-side, not surfaced.
	if !strings.Contains(rec.Body.String(), "failed to persist provider") {
		t.Fatalf("PUT error body does not report persistence failure: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "not-a-directory") {
		t.Fatalf("PUT error body leaked filesystem detail: %s", rec.Body.String())
	}
}

func TestSettingsProvidersGetLoadsProvidersFromDisk(t *testing.T) {
	configDir := setupProviderHandlerEnv(t)
	writeProviderFile(t, configDir, configuration.CustomProviderConfig{Name: "disk-only", Endpoint: "https://example.com"})

	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/settings/providers", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsProvidersGet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response struct {
		CustomProviders map[string]configuration.CustomProviderConfig `json:"custom_providers"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if _, ok := response.CustomProviders["disk-only"]; !ok {
		t.Fatalf("GET response missing disk-only provider: %#v", response.CustomProviders)
	}
}

// TestManagerEnrichCustomProvidersLoadsFromDisk exercises the contract that
// Manager.EnrichCustomProviders() populates an empty in-memory
// CustomProviders map from the manager's effective providers directory.
// The handler-integration tests above already cover the layered-manager
// path (whose CustomProviders is pre-populated by LoadConfigWithLayers);
// this test catches regressions where Enrich stops doing the disk read
// for managers constructed via NewManagerWithConfig.
func TestManagerEnrichCustomProvidersLoadsFromDisk(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", configDir)

	// NewManagerWithConfig skips the layered load entirely: the
	// returned manager's CustomProviders starts nil, so any data the
	// test sees afterwards came from Enrich itself.
	mgr := configuration.NewManagerWithConfig(&configuration.Config{}, nil)

	// Sanity: pre-condition holds.
	if got := mgr.GetConfig().CustomProviders; len(got) != 0 {
		t.Fatalf("pre-condition: expected empty CustomProviders, got %#v", got)
	}

	writeProviderFile(t, configDir, configuration.CustomProviderConfig{
		Name:     "disk-only",
		Endpoint: "https://example.com/v1",
	})

	mgr.EnrichCustomProviders()

	after := mgr.GetConfig().CustomProviders
	provider, ok := after["disk-only"]
	if !ok {
		t.Fatalf("Enrich did not populate CustomProviders: %#v", after)
	}
	if provider.Endpoint != "https://example.com/v1/chat/completions" {
		t.Fatalf("provider.Endpoint = %q, want normalized chat-completions URL", provider.Endpoint)
	}
}

// TestManagerEnrichCustomProvidersIsIdempotent verifies that calling Enrich
// twice doesn't duplicate state or wipe in-memory entries the caller
// added between calls. This guards against future refactors that
// (for example) replace the merge-with-loop with a wholesale reset.
func TestManagerEnrichCustomProvidersIsIdempotent(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", configDir)

	mgr := configuration.NewManagerWithConfig(&configuration.Config{}, nil)
	writeProviderFile(t, configDir, configuration.CustomProviderConfig{
		Name:     "from-disk",
		Endpoint: "https://disk.example.com",
	})

	mgr.EnrichCustomProviders()

	// Inject an in-memory-only entry; the second Enrich must keep it.
	injected := configuration.CustomProviderConfig{
		Name:     "memory-only",
		Endpoint: "https://memory.example.com",
	}
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			cfg.CustomProviders = make(map[string]configuration.CustomProviderConfig)
		}
		cfg.CustomProviders["memory-only"] = injected
		return nil
	}); err != nil {
		t.Fatalf("inject memory-only: %v", err)
	}

	mgr.EnrichCustomProviders()

	both := mgr.GetConfig().CustomProviders
	if _, ok := both["from-disk"]; !ok {
		t.Fatalf("second Enrich lost disk entry: %#v", both)
	}
	mem, ok := both["memory-only"]
	if !ok {
		t.Fatalf("second Enrich wiped in-memory entry: %#v", both)
	}
	if mem.Endpoint != injected.Endpoint {
		t.Fatalf("memory-only.Endpoint = %q, want %q", mem.Endpoint, injected.Endpoint)
	}
}

// setupProviderHandlerEnv scopes the global config directory and HOME to
// temp dirs so handlers under test never touch the caller's real
// ~/.config/sprout. It returns the SPROUT_CONFIG-resolved directory so
// tests can read/write provider JSON files at the expected path.
//
// Unlike an earlier setupProviderHandlerTest helper, this no longer
// constructs a Manager: the handlers under test resolve their own via
// ws.getConfigManager(), which falls back to NewManagerWithLayers and
// (per fix) calls Enrich internally — so callers don't need a manager
// passed in.
func setupProviderHandlerEnv(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	configDir := filepath.Join(homeDir, ".config", "sprout")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("create global config directory: %v", err)
	}
	t.Setenv("SPROUT_CONFIG", configDir)
	return configDir
}

func writeProviderFile(t *testing.T, configDir string, provider configuration.CustomProviderConfig) {
	t.Helper()
	providersDir := filepath.Join(configDir, configuration.ProvidersDirName)
	if err := os.MkdirAll(providersDir, 0700); err != nil {
		t.Fatalf("create providers directory: %v", err)
	}
	data, err := json.Marshal(provider)
	if err != nil {
		t.Fatalf("marshal provider: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providersDir, provider.Name+".json"), data, 0600); err != nil {
		t.Fatalf("write provider file: %v", err)
	}
}

func readProviderFile(t *testing.T, configDir, name string) configuration.CustomProviderConfig {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(configDir, configuration.ProvidersDirName, name+".json"))
	if err != nil {
		t.Fatalf("read provider file: %v", err)
	}
	var provider configuration.CustomProviderConfig
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&provider); err != nil {
		t.Fatalf("decode provider file: %v", err)
	}
	return provider
}
