package core

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// dynamicProvider returns different responses per call, allowing tests to
// simulate multi-iteration flows (tool calls → results → final response).
type dynamicProvider struct {
	mu        sync.Mutex
	callIdx   int
	responses []*ChatResponse
	info      ProviderInfo
}

func (d *dynamicProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.callIdx >= len(d.responses) {
		return d.responses[len(d.responses)-1], nil // repeat last
	}
	resp := d.responses[d.callIdx]
	d.callIdx++
	return resp, nil
}
func (d *dynamicProvider) ChatStream(_ context.Context, _ *ChatRequest, _ StreamHandler) error {
	return nil
}
func (d *dynamicProvider) Info() ProviderInfo                { return d.info }
func (d *dynamicProvider) EstimateTokens(_ *ChatRequest) int { return 100 }

// ---------------------------------------------------------------------------
// TestThreadingRecovery_SynthesizesMissingResults
//
// Scenario: assistant returns 2 tool calls, but executor only returns 1 result.
// The recovery code must synthesize a synthetic error result for the missing
// call so the message list stays well-formed.
// ---------------------------------------------------------------------------

func TestThreadingRecovery_SynthesizesMissingResults(t *testing.T) {
	// First provider response: assistant with 2 tool calls.
	// Second provider response: assistant with plain text (turn completes).
	provider := &dynamicProvider{
		info: ProviderInfo{ContextSize: 10000},
		responses: []*ChatResponse{
			{
				Choices: []ChatChoice{{
					Message: Message{
						Role:    "assistant",
						Content: "I'll do both.",
						ToolCalls: []ToolCall{
							{ID: "call_a", Function: ToolCallFunction{Name: "toolA", Arguments: "{}"}},
							{ID: "call_b", Function: ToolCallFunction{Name: "toolB", Arguments: "{}"}},
						},
					},
				}},
				Usage: ChatUsage{TotalTokens: 10},
			},
			{
				Choices: []ChatChoice{{
					Message: Message{Role: "assistant", Content: "Done."},
				}},
				Usage: ChatUsage{TotalTokens: 5},
			},
		},
	}

	// Executor returns only 1 result (for call_a), missing call_b.
	executor := &mockExecutor{
		results: []Message{
			{Role: "tool", Content: "result A", ToolCallID: "call_a"},
		},
	}

	a, err := NewAgent(Options{
		Provider:      provider,
		Executor:      executor,
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "Do both things")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := a.State().Messages()

	// Find the synthetic result for call_b
	var foundSynthetic bool
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == "call_b" {
			foundSynthetic = true
			if m.Status != ToolStatusError {
				t.Errorf("expected synthetic result status=%q, got %q", ToolStatusError, m.Status)
			}
			if !strings.Contains(m.Content, "synthetic-result") {
				t.Errorf("expected synthetic content, got %q", m.Content)
			}
			break
		}
	}
	if !foundSynthetic {
		t.Fatal("expected synthetic tool result for call_b, not found in message list")
	}

	// Validate that the message list is well-formed for the next iteration.
	violations := ValidateToolThreading(msgs)
	var missingResults []ToolThreadingViolation
	for _, v := range violations {
		if v.Kind == ToolThreadingViolationMissingResult {
			missingResults = append(missingResults, v)
		}
	}
	if len(missingResults) > 0 {
		t.Errorf("expected 0 missing_result violations after recovery, got %d: %v", len(missingResults), missingResults)
	}
}

// ---------------------------------------------------------------------------
// TestThreadingRecovery_DedupesDuplicateResults
//
// Scenario: executor returns the same ToolCallID twice. Only one should be
// added to state.
// ---------------------------------------------------------------------------

func TestThreadingRecovery_DedupesDuplicateResults(t *testing.T) {
	provider := &dynamicProvider{
		info: ProviderInfo{ContextSize: 10000},
		responses: []*ChatResponse{
			{
				Choices: []ChatChoice{{
					Message: Message{
						Role:    "assistant",
						Content: "Running tool.",
						ToolCalls: []ToolCall{
							{ID: "call_1", Function: ToolCallFunction{Name: "toolX", Arguments: "{}"}},
						},
					},
				}},
				Usage: ChatUsage{TotalTokens: 10},
			},
			{
				Choices: []ChatChoice{{
					Message: Message{Role: "assistant", Content: "Done."},
				}},
				Usage: ChatUsage{TotalTokens: 5},
			},
		},
	}

	// Executor returns the same result twice (simulating a bug).
	executor := &mockExecutor{
		results: []Message{
			{Role: "tool", Content: "result 1", ToolCallID: "call_1"},
			{Role: "tool", Content: "result 1 dup", ToolCallID: "call_1"},
		},
	}

	a, err := NewAgent(Options{
		Provider:      provider,
		Executor:      executor,
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "Run tool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := a.State().Messages()

	// Count tool results for call_1 — should be exactly 1.
	count := 0
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == "call_1" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 tool result for call_1 after dedup, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// TestThreadingRecovery_NoChangeWhenBalanced
//
// Scenario: N tool calls, N results — normal path. No synthetic results
// should be added; no dedup should happen.
// ---------------------------------------------------------------------------

func TestThreadingRecovery_NoChangeWhenBalanced(t *testing.T) {
	provider := &dynamicProvider{
		info: ProviderInfo{ContextSize: 10000},
		responses: []*ChatResponse{
			{
				Choices: []ChatChoice{{
					Message: Message{
						Role:    "assistant",
						Content: "Running tools.",
						ToolCalls: []ToolCall{
							{ID: "call_1", Function: ToolCallFunction{Name: "toolA", Arguments: "{}"}},
							{ID: "call_2", Function: ToolCallFunction{Name: "toolB", Arguments: "{}"}},
						},
					},
				}},
				Usage: ChatUsage{TotalTokens: 10},
			},
			{
				Choices: []ChatChoice{{
					Message: Message{Role: "assistant", Content: "Done."},
				}},
				Usage: ChatUsage{TotalTokens: 5},
			},
		},
	}

	// Executor returns exactly one result per call.
	executor := &mockExecutor{
		results: []Message{
			{Role: "tool", Content: "result A", ToolCallID: "call_1"},
			{Role: "tool", Content: "result B", ToolCallID: "call_2"},
		},
	}

	a, err := NewAgent(Options{
		Provider:      provider,
		Executor:      executor,
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "Run tools")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := a.State().Messages()

	// Count tool results — should be exactly 2, no synthetics.
	toolResults := 0
	for _, m := range msgs {
		if m.Role == "tool" {
			toolResults++
			if strings.Contains(m.Content, "synthetic-result") {
				t.Error("unexpected synthetic result in balanced scenario")
			}
		}
	}
	if toolResults != 2 {
		t.Errorf("expected 2 tool results, got %d", toolResults)
	}

	// Validate the full message list — should have zero violations.
	violations := ValidateToolThreading(msgs)
	if len(violations) > 0 {
		t.Errorf("expected 0 violations in balanced scenario, got %d: %v", len(violations), violations)
	}
}

// ---------------------------------------------------------------------------
// TestThreadingRecovery_AllResultsMissingFromNilExecutor
//
// Scenario: executor.Execute returns nil (e.g. noopExecutor) but the
// assistant message contains tool calls. The recovery code must synthesize
// error results for ALL tool calls so the message list stays well-formed.
// ---------------------------------------------------------------------------

func TestThreadingRecovery_AllResultsMissingFromNilExecutor(t *testing.T) {
	provider := &dynamicProvider{
		info: ProviderInfo{ContextSize: 10000},
		responses: []*ChatResponse{
			{
				Choices: []ChatChoice{{
					Message: Message{
						Role:    "assistant",
						Content: "Running tools.",
						ToolCalls: []ToolCall{
							{ID: "call_x", Function: ToolCallFunction{Name: "toolX", Arguments: "{}"}},
							{ID: "call_y", Function: ToolCallFunction{Name: "toolY", Arguments: "{}"}},
						},
					},
				}},
				Usage: ChatUsage{TotalTokens: 10},
			},
			{
				Choices: []ChatChoice{{
					Message: Message{Role: "assistant", Content: "Done."},
				}},
				Usage: ChatUsage{TotalTokens: 5},
			},
		},
	}

	// Executor returns nil — simulates a noop or broken executor.
	executor := &mockExecutor{results: nil}

	a, err := NewAgent(Options{
		Provider:      provider,
		Executor:      executor,
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "Run tools")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := a.State().Messages()

	// Both tool calls must have synthetic error results.
	syntheticCount := 0
	for _, m := range msgs {
		if m.Role == "tool" && strings.Contains(m.Content, "synthetic-result") {
			syntheticCount++
			if m.Status != ToolStatusError {
				t.Errorf("expected synthetic result status=%q for tool call %q, got %q", ToolStatusError, m.ToolCallID, m.Status)
			}
		}
	}
	if syntheticCount != 2 {
		t.Errorf("expected 2 synthetic tool results, got %d", syntheticCount)
	}

	// Validate the full message list — should have zero violations.
	violations := ValidateToolThreading(msgs)
	if len(violations) > 0 {
		t.Errorf("expected 0 violations after nil-executor recovery, got %d: %v", len(violations), violations)
	}
}
