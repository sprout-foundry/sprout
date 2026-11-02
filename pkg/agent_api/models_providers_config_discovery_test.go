package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func writeCustomProvider(t *testing.T, name, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SPROUT_CONFIG_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, "providers"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "providers", name+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCustomProviderModels_DisableDiscoveryAsksNothing(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)
	writeCustomProvider(t, "hostmodel", `{"name": "hostmodel", "endpoint": "`+srv.URL+`/proxy/chat",
		"model_name": "managed", "context_size": 150000, "disable_model_discovery": true}`)

	w := &genericConfigListModelsWrapper{providerName: "hostmodel"}
	models, err := w.loadCustomProviderModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "managed" || models[0].ContextLength != 150000 {
		t.Errorf("models = %+v, want the configured model", models)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("asked the endpoint %d times, want none", n)
	}
}

func TestCustomProviderModels_DiscoveryAsksTheEndpoint(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [{"id": "listed"}]}`))
	}))
	t.Cleanup(srv.Close)
	writeCustomProvider(t, "hostmodel", `{"name": "hostmodel", "endpoint": "`+srv.URL+`/proxy/chat", "model_name": "managed"}`)

	w := &genericConfigListModelsWrapper{providerName: "hostmodel"}
	models, err := w.loadCustomProviderModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() == 0 || len(models) != 1 || models[0].ID != "listed" {
		t.Fatalf("models = %+v after %d requests, want the listed model", models, hits.Load())
	}
}
