package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestDeriveUsageMetricsUsesProviderUsageWhenPresent(t *testing.T) {
	resp := &api.ChatResponse{}
	resp.Usage.PromptTokens = 120
	resp.Usage.CompletionTokens = 30
	resp.Usage.TotalTokens = 150
	resp.Usage.EstimatedCost = 0.001
	resp.Usage.CachedTokens = 40

	prompt, completion, total, cost, cached, _, estimated := deriveUsageMetrics(resp, nil, nil)
	if estimated {
		t.Fatalf("expected non-estimated usage when provider metrics are present")
	}
	if prompt != 120 || completion != 30 || total != 150 {
		t.Fatalf("unexpected usage values: prompt=%d completion=%d total=%d", prompt, completion, total)
	}
	if cost != 0.001 || cached != 40 {
		t.Fatalf("unexpected cost/cached values: cost=%f cached=%d", cost, cached)
	}
}

func TestDeriveUsageMetricsEstimatesWhenProviderUsageMissing(t *testing.T) {
	choice := api.Choice{}
	choice.Message.Role = "assistant"
	choice.Message.Content = "This is a generated completion."
	resp := &api.ChatResponse{
		Choices: []api.Choice{choice},
	}
	messages := []api.Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Write a short response."},
	}

	prompt, completion, total, _, _, _, estimated := deriveUsageMetrics(resp, messages, nil)
	if !estimated {
		t.Fatalf("expected estimated usage when provider metrics are missing")
	}
	if prompt <= 0 {
		t.Fatalf("expected positive estimated prompt tokens, got %d", prompt)
	}
	if completion <= 0 {
		t.Fatalf("expected positive estimated completion tokens, got %d", completion)
	}
	if total != prompt+completion {
		t.Fatalf("expected total=%d, got %d", prompt+completion, total)
	}
}

func TestDeriveUsageMetricsUsesCentralizedEstimatorForToolCalls(t *testing.T) {
	choice := api.Choice{}
	choice.Message.Role = "assistant"
	choice.Message.Content = "I can help with that."
	resp := &api.ChatResponse{
		Choices: []api.Choice{choice},
	}

	messages := []api.Message{
		{
			Role:    "assistant",
			Content: "I can help with that.",
			ToolCalls: []api.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
				},
			},
		},
	}
	messages[0].ToolCalls[0].Function.Name = "calculator"
	messages[0].ToolCalls[0].Function.Arguments = `{"value":1}`

	prompt, _, _, _, _, _, estimated := deriveUsageMetrics(resp, messages, nil)
	if !estimated {
		t.Fatalf("expected estimated usage when provider metrics are missing")
	}

	want := api.EstimateInputTokens(messages, nil)
	if prompt != want {
		t.Fatalf("expected prompt token estimate to match centralized estimator, got %d want %d", prompt, want)
	}
}
