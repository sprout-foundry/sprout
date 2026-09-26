package agent

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// A 402 (out of credits) cannot succeed on retry; the whole turn must make
// exactly one request and fail.
func TestPaymentRequiredIsNotRetried(t *testing.T) {
	manager, _, cleanup := cliPathTestEnv(t, 200_000)
	defer cleanup()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"message":"You're out of platform credits.","code":"credits_exhausted"}}`))
	}))
	defer srv.Close()
	for _, f := range []interface {
		GetProviderConfig(string) (*providers.ProviderConfig, error)
		UpsertConfig(string, *providers.ProviderConfig) error
	}{providers.GlobalFactory(), factory.GlobalFactory()} {
		cfg, err := f.GetProviderConfig(cliTestProviderName)
		if err != nil {
			t.Fatalf("get provider config: %v", err)
		}
		c := *cfg
		c.Endpoint = srv.URL + "/v1/chat/completions"
		if err := f.UpsertConfig(cliTestProviderName, &c); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	ag, err := newAgentWithConfigManagerInner(manager, t.TempDir(), "")
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	defer ag.Shutdown()
	ag.SetStreamingEnabled(true)

	if _, err := ag.ProcessQuery("hello"); err == nil {
		t.Fatal("expected the 402 to fail the turn")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("expected exactly 1 request for a 402, got %d", got)
	}
}
