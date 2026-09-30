package configuration

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// discoveryServer serves an OpenAI-style /models list and records the
// Authorization header of the last request.
func discoveryServer(t *testing.T) (*httptest.Server, func() string) {
	t.Helper()
	var mu sync.Mutex
	var lastAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		lastAuth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() string {
		mu.Lock()
		defer mu.Unlock()
		return lastAuth
	}
}

// isolateCredentialStore pins the file backend inside a temp config dir so
// no test touches the developer's OS keyring.
func isolateCredentialStore(t *testing.T) {
	t.Helper()
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	credentials.ResetStorageBackend()
	if err := credentials.SetStorageMode("file"); err != nil {
		t.Fatalf("SetStorageMode: %v", err)
	}
	t.Cleanup(credentials.ResetStorageBackend)
}

func TestDiscoverCustomProviderModelsWithKey_UsesPastedKey(t *testing.T) {
	isolateCredentialStore(t)
	srv, lastAuth := discoveryServer(t)

	models, err := DiscoverCustomProviderModelsWithKey(CustomProviderConfig{
		Name:           "pasted-key-provider",
		Endpoint:       srv.URL + "/v1",
		RequiresAPIKey: true,
	}, "sk-pasted")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(models) != 1 || models[0].ID != "model-a" {
		t.Fatalf("models = %+v", models)
	}
	if got := lastAuth(); got != "Bearer sk-pasted" {
		t.Fatalf("Authorization = %q, want the pasted key", got)
	}
}

func TestDiscoverCustomProviderModels_FallsBackToStoredKey(t *testing.T) {
	isolateCredentialStore(t)
	srv, lastAuth := discoveryServer(t)
	if err := credentials.SetToActiveBackend("stored-key-provider", "sk-stored"); err != nil {
		t.Fatalf("store key: %v", err)
	}

	if _, err := DiscoverCustomProviderModels(CustomProviderConfig{
		Name:           "stored-key-provider",
		Endpoint:       srv.URL + "/v1",
		RequiresAPIKey: true,
	}); err != nil {
		t.Fatalf("discover: %v", err)
	}
	if got := lastAuth(); got != "Bearer sk-stored" {
		t.Fatalf("Authorization = %q, want the stored key", got)
	}
}

func TestDiscoverCustomProviderModels_EnvVarBeatsStoredKey(t *testing.T) {
	isolateCredentialStore(t)
	srv, lastAuth := discoveryServer(t)
	t.Setenv("SPROUT_TEST_DISCOVERY_KEY", "sk-env")
	if err := credentials.SetToActiveBackend("env-provider", "sk-stored"); err != nil {
		t.Fatalf("store key: %v", err)
	}

	if _, err := DiscoverCustomProviderModels(CustomProviderConfig{
		Name:           "env-provider",
		Endpoint:       srv.URL + "/v1",
		EnvVar:         "SPROUT_TEST_DISCOVERY_KEY",
		RequiresAPIKey: true,
	}); err != nil {
		t.Fatalf("discover: %v", err)
	}
	if got := lastAuth(); got != "Bearer sk-env" {
		t.Fatalf("Authorization = %q, want the env var key (runtime precedence)", got)
	}
}

func TestDiscoverCustomProviderModels_NoAuthForKeylessProvider(t *testing.T) {
	isolateCredentialStore(t)
	srv, lastAuth := discoveryServer(t)

	if _, err := DiscoverCustomProviderModels(CustomProviderConfig{
		Name:     "local-provider",
		Endpoint: srv.URL + "/v1",
	}); err != nil {
		t.Fatalf("discover: %v", err)
	}
	if got := lastAuth(); got != "" {
		t.Fatalf("Authorization = %q, want none for a keyless provider", got)
	}
}
