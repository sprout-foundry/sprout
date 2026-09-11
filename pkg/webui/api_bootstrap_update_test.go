//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/buildinfo"
	"github.com/sprout-foundry/sprout/pkg/updatecheck"
)

func TestHandleAPIBootstrap_UpdateFieldReflectsCache(t *testing.T) {
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())
	server := newSyncTestWebServer(t, t.TempDir())
	request := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)

	// Empty cache → no update field, and buildVersion mirrors buildinfo.
	rec := httptest.NewRecorder()
	server.handleAPIBootstrap(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("bootstrap body not JSON: %v", err)
	}
	if _, ok := payload["update"]; ok {
		t.Errorf("empty cache must omit 'update', got %s", payload["update"])
	}
	var config RuntimeConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if config.BuildVersion != buildinfo.Version {
		t.Errorf("buildVersion = %q, want %q", config.BuildVersion, buildinfo.Version)
	}

	// Cached newer release → update populated from the cache.
	now := time.Now()
	if err := writeBootstrapTestState(t, now, "v0.15.0"); err != nil {
		t.Fatal(err)
	}
	defer setTestBuildVersion(t, "v0.14.0")()

	rec = httptest.NewRecorder()
	server.handleAPIBootstrap(rec, request)
	if err := json.Unmarshal(rec.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if config.Update == nil {
		t.Fatal("cached newer release must populate update")
	}
	if config.Update.Current != "v0.14.0" || config.Update.Latest != "v0.15.0" {
		t.Errorf("update = %+v, want current v0.14.0 / latest v0.15.0", config.Update)
	}
}

func setTestBuildVersion(t *testing.T, v string) func() {
	t.Helper()
	old := buildinfo.Version
	buildinfo.Version = v
	return func() { buildinfo.Version = old }
}

func writeBootstrapTestState(t *testing.T, now time.Time, latest string) error {
	t.Helper()
	b, err := json.Marshal(updatecheck.State{LastCheck: now, LatestRelease: latest})
	if err != nil {
		return err
	}
	return os.WriteFile(
		filepath.Join(os.Getenv("SPROUT_STATE_DIR"), "update-check.json"),
		b, 0o600)
}
