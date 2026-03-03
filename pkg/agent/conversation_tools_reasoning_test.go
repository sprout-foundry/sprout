package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/factory"
)

func TestConversationDetermineReasoningEffort_StartsMediumForGptOSS(t *testing.T) {
	agent := &Agent{
		client: &reasoningProviderClient{
			TestClient: &factory.TestClient{},
			provider:   "openai",
			model:      "gpt-oss:20b",
		},
		currentIteration: 0,
		messages:         []api.Message{{Role: "user", Content: "do task"}},
	}
	ch := NewConversationHandler(agent)

	if got := ch.determineReasoningEffort(); got != "medium" {
		t.Fatalf("expected reasoning to start at medium for gpt-oss, got %q", got)
	}
}

func TestConversationDetermineReasoningEffort_GptOSSModelPolicyAppliesAcrossProviders(t *testing.T) {
	agent := &Agent{
		client: &reasoningProviderClient{
			TestClient: &factory.TestClient{},
			provider:   "ollama",
			model:      "gpt-oss:20b",
		},
		messages: []api.Message{{Role: "user", Content: "do task"}},
	}
	ch := NewConversationHandler(agent)

	if got := ch.determineReasoningEffort(); got != "medium" {
		t.Fatalf("expected model-based policy for gpt-oss to start at medium regardless of provider, got %q", got)
	}
}

func TestConversationDetermineReasoningEffort_NonGptOSSUsesDefaultHeuristic(t *testing.T) {
	agent := &Agent{
		client: &reasoningProviderClient{
			TestClient: &factory.TestClient{},
			provider:   "openai",
			model:      "gpt-4o-mini",
		},
		messages: []api.Message{{Role: "user", Content: "what is this"}},
	}
	ch := NewConversationHandler(agent)

	if got := ch.determineReasoningEffort(); got != "low" {
		t.Fatalf("expected non-gpt-oss model to use default heuristic, got %q", got)
	}
}
