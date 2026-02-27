package agent

import (
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
)

func TestGetAvailablePersonaIDsSorted(t *testing.T) {
	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	ids := agent.GetAvailablePersonaIDs()
	if len(ids) < 2 {
		t.Fatalf("expected at least two personas, got %d", len(ids))
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] > ids[i] {
			t.Fatalf("persona ids are not sorted: %v", ids)
		}
	}
}

func TestGetPersonaProviderModelUsesProviderKeys(t *testing.T) {
	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	provider, _, err := agent.GetPersonaProviderModel("general")
	if err != nil {
		t.Fatalf("GetPersonaProviderModel failed: %v", err)
	}
	if provider != string(agent.GetProviderType()) {
		t.Fatalf("expected provider key %q, got %q", string(agent.GetProviderType()), provider)
	}
}

func TestGetPersonaProviderModelProviderOverrideUsesConfiguredModel(t *testing.T) {
	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	cfg := agent.GetConfigManager().GetConfig()
	cfg.SubagentTypes["tmp_provider_override"] = configuration.SubagentType{
		ID:       "tmp_provider_override",
		Name:     "Temp Provider Override",
		Provider: "deepinfra",
		Enabled:  true,
	}

	provider, model, err := agent.GetPersonaProviderModel("tmp_provider_override")
	if err != nil {
		t.Fatalf("GetPersonaProviderModel failed: %v", err)
	}
	if provider != "deepinfra" {
		t.Fatalf("expected deepinfra provider, got %q", provider)
	}
	wantModel := cfg.GetModelForProvider("deepinfra")
	if model != wantModel {
		t.Fatalf("expected model %q, got %q", wantModel, model)
	}
}

func TestApplyPersonaNotFoundIncludesAvailablePersonas(t *testing.T) {
	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	err = agent.ApplyPersona("definitely_not_real")
	if err == nil {
		t.Fatalf("expected error for unknown persona")
	}
	msg := err.Error()
	if !strings.Contains(msg, "available personas:") {
		t.Fatalf("expected available personas in error, got: %s", msg)
	}
	if !strings.Contains(msg, "orchestrator") {
		t.Fatalf("expected orchestrator in available persona list, got: %s", msg)
	}
}
