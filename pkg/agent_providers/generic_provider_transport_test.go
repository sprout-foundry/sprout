package providers

import (
	"net/http"
	"testing"
)

// The provider transport must be sized for the concurrency the agent actually
// runs at. A parallel subagent burst issues up to 16 requests to the same host;
// with the http.Transport zero value (an idle pool of 2) the excess connections
// are dialed, used once and closed, and that churn is what surfaces as a
// mid-stream "connection reset" against an otherwise healthy provider.
func TestProviderTransportSizesTheIdlePoolForConcurrentStreams(t *testing.T) {
	tr := newProviderTransport()

	const maxParallelSubagents = 16
	if tr.MaxIdleConnsPerHost < maxParallelSubagents {
		t.Errorf("MaxIdleConnsPerHost = %d, want >= %d (a parallel subagent burst must not churn connections)",
			tr.MaxIdleConnsPerHost, maxParallelSubagents)
	}
	if tr.MaxConnsPerHost < tr.MaxIdleConnsPerHost {
		t.Errorf("MaxConnsPerHost = %d, want >= MaxIdleConnsPerHost (%d): a cap below the idle pool forces churn",
			tr.MaxConnsPerHost, tr.MaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout <= 0 {
		t.Error("IdleConnTimeout must be set: pooled connections must not outlive the upstream's idle limit")
	}
}

// Each client needs its own transport: a shared one would let a long-lived
// stream's connection evict the pooled connections of the other client.
func TestProviderClientsDoNotShareATransport(t *testing.T) {
	p, err := NewGenericProvider(&ProviderConfig{
		Name:     "test",
		Endpoint: "https://example.invalid/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer", Key: "test-key"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models:   ModelConfig{DefaultContextLimit: 8192},
	})
	if err != nil {
		t.Fatalf("NewGenericProvider: %v", err)
	}

	if p.httpClient.Transport == p.streamingClient.Transport {
		t.Error("the streaming and non-streaming clients share a transport; they must be independent")
	}
	if p.httpClient.Transport == http.DefaultTransport {
		t.Error("the client uses http.DefaultTransport; it must use the tuned provider transport")
	}
}
