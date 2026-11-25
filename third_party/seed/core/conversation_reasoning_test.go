package core

import (
	"context"
	"testing"
)

// frReasoningResponse builds a ChatResponse with reasoning content. The
// finish reason is "stop" (the model ended its turn) and the visible
// Content is set to whatever the caller wants — typically empty or a
// one-word acknowledgement, mirroring a reasoning-only model that
// decided to stop after thinking.
func frReasoningResponse(content, reasoning string) *ChatResponse {
	return &ChatResponse{
		Choices: []ChatChoice{{
			Message: Message{
				Role:             "assistant",
				Content:          content,
				ReasoningContent: reasoning,
			},
			FinishReason: "stop",
		}},
		Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

// TestReasoningOnlyGuard_RejectsEmptyContentAfterToolCalls verifies that a
// reasoning-capable model which emits a tool call, then on the following
// turn sends "stop" with empty Content but non-empty ReasoningContent, is
// rejected and asked to act or state a final answer.
func TestReasoningOnlyGuard_RejectsEmptyContentAfterToolCalls(t *testing.T) {
	provider := newFRProvider(
		// 1. Model calls a tool
		frToolCallOnlyResponse("call_1", "echo", `{"message":"test"}`),
		// 2. Reasoning-only response after tool results → rejected (#1)
		frReasoningResponse("", "The user wants me to verify the build. I should run make build-all next."),
		// 3. Concrete answer → accepted
		frTextResponse("The build passed without errors after the fix.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "echo result", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "verify the build")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "The build passed without errors after the fix." {
		t.Errorf("expected concrete answer after rejection, got: %q", result)
	}
	// 3 provider calls: tool → rejected reasoning-only → concrete answer.
	if provider.idx != 3 {
		t.Errorf("expected 3 provider calls, got %d", provider.idx)
	}
}

// TestReasoningOnlyGuard_AcceptsAfterOneRejection is the cap test: after
// one rejection, a second reasoning-only response reaches the limit and is
// accepted to break the loop.
//
// Flow:
//  1. Tool call → result
//  2. Reasoning-only #1 (empty content) → rejected, nudge
//  3. Reasoning-only #2 (punctuation only) → limit reached, accepted
func TestReasoningOnlyGuard_AcceptsAfterOneRejection(t *testing.T) {
	provider := newFRProvider(
		frToolCallOnlyResponse("call_1", "echo", `{"message":"test"}`),
		frReasoningResponse("", "I should think about whether to continue or call another tool here now."),
		frReasoningResponse("…", "I will summarize the findings now and conclude the run here."),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "result", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "research")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "…" {
		t.Errorf("expected the response accepted at the cap, got: %q", result)
	}
	if provider.idx != 3 {
		t.Errorf("expected 3 provider calls, got %d", provider.idx)
	}
}

// TestReasoningOnlyGuard_PassesShortSubstantiveResponse verifies a
// short real answer is not caught by the guard, even when
// ReasoningContent is non-empty. This is the regression case we must
// NOT introduce — the user's bug report is specifically about content
// being essentially empty, not about content being short.
func TestReasoningOnlyGuard_PassesShortSubstantiveResponse(t *testing.T) {
	provider := newFRProvider(
		// 1. Tool call
		frToolCallOnlyResponse("call_1", "echo", `{"message":"test"}`),
		// 2. Real short answer → accepted
		frReasoningResponse("Build is green.", "Verify was the main thing the user asked for."),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "result", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "verify")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Build is green." {
		t.Errorf("expected short substantive answer to pass, got: %q", result)
	}
	// Only 2 calls: tool + accepted answer — no retry.
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

// TestReasoningOnlyGuard_DoneAloneIsAccepted verifies a one-word final
// answer after tool calls ends the turn: it is the model's answer, and
// rejecting it made the model repeat itself or redo finished work.
func TestReasoningOnlyGuard_DoneAloneIsAccepted(t *testing.T) {
	provider := newFRProvider(
		frToolCallOnlyResponse("call_1", "echo", `{"message":"test"}`),
		frReasoningResponse("Done.", "I have decided to stop the run here since the user's intent is satisfied."),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "result", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Done." {
		t.Errorf("expected the one-word answer, got: %q", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls (tool + answer), got %d", provider.idx)
	}
}

// TestReasoningOnlyGuard_OneWordAnswerWithoutTools verifies a reasoning
// model's one-word answer to a plain question is accepted as-is. Rejecting it
// makes the model repeat the answer, which the user then sees twice.
func TestReasoningOnlyGuard_OneWordAnswerWithoutTools(t *testing.T) {
	provider := newFRProvider(
		frReasoningResponse("PLUM", "The user asks me to reply with just the word PLUM."),
		frTextResponse("PLUM", "stop"),
	)
	agent, _ := NewAgent(Options{Provider: provider, Executor: &mockExecutor{}})

	result, err := agent.Run(context.Background(), "Reply with just the word PLUM.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "PLUM" {
		t.Errorf("expected the one-word answer, got: %q", result)
	}
	if provider.idx != 1 {
		t.Errorf("expected 1 provider call, got %d", provider.idx)
	}
}

// TestReasoningOnlyGuard_NoReasoning_NoTrigger verifies the guard does
// NOT fire when ReasoningContent is empty. A simple empty Content after
// tool results is the blank guard's job — reasoning-only detection is
// specifically about the case where the model thought but didn't act.
func TestReasoningOnlyGuard_NoReasoning_NoTrigger(t *testing.T) {
	provider := newFRProvider(
		frToolCallOnlyResponse("call_1", "echo", `{"message":"test"}`),
		// No reasoning content, no Content → blank guard handles this,
		// reasoning-only guard does NOT.
		frReasoningResponse("", ""),
		frReasoningResponse("", ""),
		// After 2 blank responses, blank guard force-finalizes with error.
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "result", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	_, err := agent.Run(context.Background(), "do the thing")
	// The blank guard fires twice and returns BlankResponseError. The
	// reasoning-only guard should NOT have interfered.
	if err == nil {
		t.Fatal("expected blank-response error from blank guard (not reasoning-only)")
	}
	if _, ok := err.(*BlankResponseError); !ok {
		t.Errorf("expected BlankResponseError, got %T: %v", err, err)
	}
}

// contains is a tiny helper to keep test assertions readable.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
