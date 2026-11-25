package core

import (
	"context"
	"strings"
	"testing"
)

// frToolCallOnlyResponse creates a response with tool calls and no text content.
func frToolCallOnlyResponse(callID, toolName, args string) *ChatResponse {
	return &ChatResponse{
		Choices: []ChatChoice{{
			Message: Message{
				Role:    "assistant",
				Content: "",
				ToolCalls: []ToolCall{{
					ID:   callID,
					Type: "function",
					Function: ToolCallFunction{
						Name:      toolName,
						Arguments: args,
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
		Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

// TestSubstanceGuard_RetriesOnMetaAcknowledgement verifies that when the model
// returns "stop" with a brief meta-acknowledgement ("I've reviewed the files.")
// after tool results, the substance guard rejects it and retries.
func TestSubstanceGuard_RetriesOnMetaAcknowledgement(t *testing.T) {
	provider := newFRProvider(
		// Iteration 1: model calls a tool
		frToolCallOnlyResponse("call_1", "read_file", `{"path":"foo.go"}`),
		// Iteration 2: model returns a meta-acknowledgement with no findings
		frTextResponse("I've reviewed the files.", "stop"),
		// Iteration 3: after the reminder, model provides real findings
		frTextResponse("The file foo.go contains a nil pointer dereference on line 42 in the processRequest function.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "file contents here", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "research the nil pointer issue")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The final result should be the substantive response, not the meta-ack.
	if !strings.Contains(result, "nil pointer") {
		t.Errorf("expected substantive findings about nil pointer, got: %q", result)
	}
	// Should have made 3 provider calls: tool call, meta-ack (rejected), findings.
	if provider.idx != 3 {
		t.Errorf("expected 3 provider calls (tool + rejected + real answer), got %d", provider.idx)
	}
}

// TestSubstanceGuard_AcceptsAfterOneRejection verifies the loop doesn't spin
// forever — after 1 rejection (substanceRejectionCount reaches 2), the next
// insufficient response is accepted to avoid loops. This mirrors the
// tentative-rejection pattern.
func TestSubstanceGuard_AcceptsAfterOneRejection(t *testing.T) {
	provider := newFRProvider(
		// Iteration 1: tool call
		frToolCallOnlyResponse("call_1", "read_file", `{"path":"bar.go"}`),
		// Iteration 2: first meta-ack (rejected, count=1)
		frTextResponse("I've reviewed the files.", "stop"),
		// Iteration 3: second meta-ack (accepted — count=2 reaches limit)
		frTextResponse("I've checked the code.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "contents", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "research something")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should accept the 2nd meta-ack (after 1 rejection).
	if result != "I've checked the code." {
		t.Errorf("expected accepted meta-ack after 1 rejection, got: %q", result)
	}
	// 3 calls: tool + 2 stop responses (1 rejected, 1 accepted).
	if provider.idx != 3 {
		t.Errorf("expected 3 provider calls, got %d", provider.idx)
	}
}

// TestSubstanceGuard_DisabledOption verifies the guard can be turned off.
func TestSubstanceGuard_DisabledOption(t *testing.T) {
	provider := newFRProvider(
		// Tool call
		frToolCallOnlyResponse("call_1", "read_file", `{"path":"baz.go"}`),
		// Meta-ack — should be accepted immediately when guard is disabled.
		frTextResponse("I've reviewed the files.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "contents", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider:                     provider,
		Executor:                     executor,
		DisableMinimumSubstanceGuard: true,
	})

	result, err := agent.Run(context.Background(), "research something")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Meta-ack accepted without retry.
	if result != "I've reviewed the files." {
		t.Errorf("expected meta-ack accepted (guard disabled), got: %q", result)
	}
	// Only 2 calls: tool + meta-ack (no retry).
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls (guard disabled), got %d", provider.idx)
	}
}

// TestSubstanceGuard_PassesSimpleConfirmation verifies "Done." after tool calls
// is NOT caught by the guard — it's a valid simple confirmation.
func TestSubstanceGuard_PassesSimpleConfirmation(t *testing.T) {
	provider := newFRProvider(
		frToolCallOnlyResponse("call_1", "run_tests", `{}`),
		frTextResponse("Done.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "all tests passed", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "run tests")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Done." {
		t.Errorf("expected Done. accepted, got: %q", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

// TestSubstanceGuard_PassesSubstantiveResponse verifies a short but substantive
// response (with file reference) passes through.
func TestSubstanceGuard_PassesSubstantiveResponse(t *testing.T) {
	provider := newFRProvider(
		frToolCallOnlyResponse("call_1", "read_file", `{"path":"handler.go"}`),
		frTextResponse("I've reviewed the files. handler.go:42 has the bug.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "contents", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "research the bug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "handler.go:42") {
		t.Errorf("expected substantive response with file ref, got: %q", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls (no retry needed), got %d", provider.idx)
	}
}
