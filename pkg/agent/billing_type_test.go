package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// newBillingTestAgent wires a mock client reporting the given provider name
// with the given client type, for resolveBillingType tests.
func newBillingTestAgent(t *testing.T, provider string, clientType api.ClientType) *Agent {
	t.Helper()
	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.setClient(&strictSyntaxClient{
		TestClient: &factory.TestClient{},
		provider:   provider,
		model:      "test-model",
	}, clientType)
	return a
}

// Local model-serving clients have zero marginal cost regardless of
// endpoint — the endpoint heuristic alone misses LAN-hosted servers,
// which showed a misleading "$0.0000" in the status footer instead of
// "free".
func TestResolveBillingTypeLocalClientsAreFree(t *testing.T) {
	cases := []struct {
		name       string
		clientType api.ClientType
	}{
		{"ollama-local", api.OllamaLocalClientType},
		{"lmstudio", api.LMStudioClientType},
		{"sprout-local", api.SproutLocalClientType},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Unknown provider name: the config lookup misses, so the
			// client-type heuristic (not the endpoint heuristic) resolves.
			a := newBillingTestAgent(t, "billing-test-unknown", tc.clientType)
			if got := a.resolveBillingType(); got != BillingFree {
				t.Errorf("resolveBillingType(%s) = %q, want %q", tc.name, got, BillingFree)
			}
		})
	}
}

func TestResolveBillingTypeNonLocalClientFallsThrough(t *testing.T) {
	a := newBillingTestAgent(t, "billing-test-unknown", api.OpenAIClientType)
	if got := a.resolveBillingType(); got != BillingPayPerToken {
		t.Errorf("resolveBillingType(openai client, unknown provider) = %q, want %q", got, BillingPayPerToken)
	}
}

// Explicit config billing_type wins over the local-client heuristic —
// an operator who configured billing on a self-hosted stack is making a
// deliberate choice.
func TestResolveBillingTypeExplicitConfigWinsOverLocalClient(t *testing.T) {
	cfgJSON := []byte(`{
		"name": "billing-test-sub-provider",
		"billing_type": "subscription",
		"endpoint": "http://10.0.0.5:8000/v1",
		"backend": "auto",
		"auth": {"type": "api_key"}
	}`)
	if err := providers.GlobalFactory().LoadConfigFromBytes(cfgJSON); err != nil {
		t.Fatalf("LoadConfigFromBytes: %v", err)
	}
	a := newBillingTestAgent(t, "billing-test-sub-provider", api.OllamaLocalClientType)
	if got := a.resolveBillingType(); got != BillingSubscription {
		t.Errorf("resolveBillingType(explicit subscription config, ollama-local client) = %q, want %q", got, BillingSubscription)
	}
}
