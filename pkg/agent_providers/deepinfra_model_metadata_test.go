package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// deepInfraModels is the shape of DeepInfra's OpenAI-compatible model list:
// the context length and prices (USD per million tokens) sit inside each
// entry's metadata, not at its top level.
const deepInfraModels = `{"object":"list","data":[{"id":"Qwen/Qwen3.8-27B","object":"model","owned_by":"deepinfra",` +
	`"metadata":{"context_length":262144,"max_tokens":262144,"pricing":{"input_tokens":0.2,"output_tokens":2.5,"cache_read_tokens":0.05}}}]}`

func deepInfraModelServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(deepInfraModels))
	}))
	t.Cleanup(server.Close)
	return server
}

// A model DeepInfra lists only through its metadata gets its real context
// length and prices, instead of a small default that makes the agent compact
// its conversation almost at once.
func TestListModelsReadsDeepInfraMetadata(t *testing.T) {
	server := deepInfraModelServer(t)
	provider, err := NewGenericProvider(&ProviderConfig{
		Name:     "deepinfra-shape",
		Endpoint: server.URL + "/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Models:   ModelConfig{DefaultContextLimit: 32768, DefaultModel: "Qwen/Qwen3.8-27B"},
	})
	if err != nil {
		t.Fatal(err)
	}
	models, err := provider.listModelsOpenAI(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	m := models[0]
	if m.ContextLength != 262144 {
		t.Errorf("context length = %d, want 262144 from metadata", m.ContextLength)
	}
	if m.InputCost != 0.2 || m.OutputCost != 2.5 {
		t.Errorf("costs = %v/%v, want 0.2/2.5 per million tokens from metadata", m.InputCost, m.OutputCost)
	}
}

func TestRawModelListReadsDeepInfraMetadata(t *testing.T) {
	server := deepInfraModelServer(t)
	entries, ok := fetchRawModelList(context.Background(), server.Client(), server.URL+"/v1/chat/completions", "")
	if !ok || len(entries) != 1 {
		t.Fatalf("entries = %v, ok = %v", entries, ok)
	}
	if got := parseContextLengthFromRaw(entries[0]); got != 262144 {
		t.Errorf("context length = %d, want 262144 from metadata", got)
	}
}
