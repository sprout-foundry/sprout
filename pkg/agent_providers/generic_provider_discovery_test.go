package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/modelregistry"
)

// countingServer answers every request with 404 and counts them; it stands
// in for both the provider endpoint and the model registry.
func countingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestGenericProvider_DisableDiscoveryMakesNoMetadataRequests(t *testing.T) {
	srv, hits := countingServer(t)
	original := modelregistry.BaseURLForTest()
	modelregistry.SetBaseURL(srv.URL)
	modelregistry.ClearCache()
	t.Cleanup(func() {
		modelregistry.SetBaseURL(original)
		modelregistry.ClearCache()
	})

	config := &ProviderConfig{
		Name:     "hostmodel",
		Endpoint: srv.URL + "/proxy/chat/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "managed"},
		Models:   ModelConfig{DefaultContextLimit: 123_000, DisableDiscovery: true},
	}
	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatal(err)
	}

	if err := provider.SetModel("managed"); err != nil {
		t.Fatal(err)
	}
	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "managed" || models[0].ContextLength != 123_000 {
		t.Errorf("models = %+v, want the configured model", models)
	}
	limit, err := provider.GetModelContextLimit()
	if err != nil || limit != 123_000 {
		t.Errorf("context limit = %d, %v; want 123000", limit, err)
	}

	// The background warm-up (run on a model change for remote endpoints)
	// stays off too.
	provider.warmModelsCache()
	time.Sleep(50 * time.Millisecond)
	if n := hits.Load(); n != 0 {
		t.Fatalf("made %d metadata requests, want none", n)
	}
}

func TestGenericProvider_DiscoveryStillListsModelsByDefault(t *testing.T) {
	srv, hits := countingServer(t)
	config := &ProviderConfig{
		Name:     "hostmodel",
		Endpoint: srv.URL + "/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Backend:  "openai",
		Defaults: RequestDefaults{Model: "managed"},
		Models:   ModelConfig{DefaultContextLimit: 8_000},
	}
	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits.Load() == 0 {
		t.Fatal("ListModels made no request with discovery on")
	}
}
