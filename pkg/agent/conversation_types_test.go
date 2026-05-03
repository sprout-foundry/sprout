package agent

import (
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestTokenUsageFields(t *testing.T) {
	tu := TokenUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		EstimatedCost:    0.003,
	}

	if tu.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d; want 100", tu.PromptTokens)
	}
	if tu.CompletionTokens != 50 {
		t.Errorf("CompletionTokens = %d; want 50", tu.CompletionTokens)
	}
	if tu.TotalTokens != 150 {
		t.Errorf("TotalTokens = %d; want 150", tu.TotalTokens)
	}
	if tu.EstimatedCost != 0.003 {
		t.Errorf("EstimatedCost = %f; want 0.003", tu.EstimatedCost)
	}
}

func TestTurnEvaluationFields(t *testing.T) {
	toolCalls := []api.ToolCall{
		{ID: "call_1", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "shell_command", Arguments: `{"command":"ls"}`}},
	}
	toolResults := []api.Message{
		{Role: "tool", Content: "file1.txt"},
	}
	now := time.Now()

	te := TurnEvaluation{
		Iteration:         1,
		Timestamp:         now,
		UserInput:         "run ls",
		AssistantContent:  "running...",
		ToolCalls:         toolCalls,
		ToolResults:       toolResults,
		TokenUsage:        TokenUsage{TotalTokens: 200},
		CompletionReached: true,
		FinishReason:      "stop",
		ReasoningSnippet:  "reasoning",
		GuardrailTrigger:  "none",
	}

	if te.Iteration != 1 {
		t.Errorf("Iteration = %d; want 1", te.Iteration)
	}
	if !te.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v; want %v", te.Timestamp, now)
	}
	if te.UserInput != "run ls" {
		t.Errorf("UserInput = %q; want %q", te.UserInput, "run ls")
	}
	if te.AssistantContent != "running..." {
		t.Errorf("AssistantContent = %q; want %q", te.AssistantContent, "running...")
	}
	if len(te.ToolCalls) != 1 {
		t.Errorf("len(ToolCalls) = %d; want 1", len(te.ToolCalls))
	}
	if te.ToolCalls[0].Function.Name != "shell_command" {
		t.Errorf("ToolCalls[0].Function.Name = %q; want %q", te.ToolCalls[0].Function.Name, "shell_command")
	}
	if len(te.ToolResults) != 1 {
		t.Errorf("len(ToolResults) = %d; want 1", len(te.ToolResults))
	}
	if te.ToolResults[0].Role != "tool" {
		t.Errorf("ToolResults[0].Role = %q; want %q", te.ToolResults[0].Role, "tool")
	}
	if te.TokenUsage.TotalTokens != 200 {
		t.Errorf("TokenUsage.TotalTokens = %d; want 200", te.TokenUsage.TotalTokens)
	}
	if !te.CompletionReached {
		t.Error("CompletionReached should be true")
	}
	if te.FinishReason != "stop" {
		t.Errorf("FinishReason = %q; want %q", te.FinishReason, "stop")
	}
	if te.ReasoningSnippet != "reasoning" {
		t.Errorf("ReasoningSnippet = %q; want %q", te.ReasoningSnippet, "reasoning")
	}
	if te.GuardrailTrigger != "none" {
		t.Errorf("GuardrailTrigger = %q; want %q", te.GuardrailTrigger, "none")
	}
}

func TestTurnEvaluationCompletionReachedDefault(t *testing.T) {
	// Verify CompletionReached defaults to false when not set.
	te := TurnEvaluation{
		Iteration: 0,
		Timestamp: time.Now(),
	}
	if te.CompletionReached {
		t.Error("CompletionReached should be false by default")
	}
}
