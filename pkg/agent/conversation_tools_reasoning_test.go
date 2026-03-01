package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/factory"
)

func TestApplyRuntimeReasoningDownshift_HighStaysHighBeforeThreeToolCalls(t *testing.T) {
	agent := &Agent{
		client: &reasoningProviderClient{
			TestClient: &factory.TestClient{},
			provider:   "openai",
			model:      "gpt-oss:20b",
		},
		currentIteration: 1,
		messages: []api.Message{
			{Role: "tool", Content: "ok"},
			{Role: "tool", Content: "ok"},
		},
	}
	ch := NewConversationHandler(agent)

	if got := ch.applyRuntimeReasoningDownshift("high"); got != "high" {
		t.Fatalf("expected high to remain high before 3 tool calls, got %q", got)
	}
}

func TestApplyRuntimeReasoningDownshift_HighToMediumAfterThreeToolCalls(t *testing.T) {
	agent := &Agent{
		client: &reasoningProviderClient{
			TestClient: &factory.TestClient{},
			provider:   "openai",
			model:      "gpt-oss:20b",
		},
		currentIteration: 1,
		messages: []api.Message{
			{Role: "tool", Content: "ok"},
			{Role: "tool", Content: "ok"},
			{Role: "tool", Content: "ok"},
		},
	}
	ch := NewConversationHandler(agent)

	if got := ch.applyRuntimeReasoningDownshift("high"); got != "medium" {
		t.Fatalf("expected high to downshift to medium after 3 tool calls, got %q", got)
	}
}

func TestConversationDetermineReasoningEffort_StartsHigh(t *testing.T) {
	agent := &Agent{
		client: &reasoningProviderClient{
			TestClient: &factory.TestClient{},
			provider:   "openai",
			model:      "gpt-oss:20b",
		},
		currentIteration: 0,
		messages: []api.Message{{Role: "user", Content: "do task"}},
	}
	ch := NewConversationHandler(agent)

	if got := ch.determineReasoningEffort(); got != "high" {
		t.Fatalf("expected reasoning to start at high, got %q", got)
	}
}

func TestConversationDetermineReasoningEffort_DynamicDownshiftApplied(t *testing.T) {
	agent := &Agent{
		client: &reasoningProviderClient{
			TestClient: &factory.TestClient{},
			provider:   "openai",
			model:      "gpt-oss:20b",
		},
		currentIteration: 1,
		messages: []api.Message{
			{
				Role: "user",
				Content: "Please analyze and debug this complex extraction workflow, compare approaches, evaluate trade-offs, " +
					"and implement a robust fix for repeated tool-call failures with detailed validation.",
			},
			{Role: "tool", Content: "ok"},
			{Role: "tool", Content: "ok"},
			{Role: "tool", Content: "ok"},
		},
	}
	ch := NewConversationHandler(agent)

	if got := ch.determineReasoningEffort(); got != "medium" {
		t.Fatalf("expected runtime downshift to produce medium, got %q", got)
	}
}

func TestExecutedToolCallCount_IncludesAllToolResults(t *testing.T) {
	agent := &Agent{
		client: &reasoningProviderClient{
			TestClient: &factory.TestClient{},
			provider:   "openai",
			model:      "gpt-oss:20b",
		},
		messages: []api.Message{
			{Role: "tool", Content: "ok"},
			{Role: "tool", Content: "Error: failed"},
			{Role: "tool", Content: "ok"},
		},
	}
	ch := NewConversationHandler(agent)

	if got := ch.executedToolCallCount(); got != 3 {
		t.Fatalf("expected 3 tool result messages, got %d", got)
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

	if got := ch.determineReasoningEffort(); got != "high" {
		t.Fatalf("expected model-based policy for gpt-oss to start at high regardless of provider, got %q", got)
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
