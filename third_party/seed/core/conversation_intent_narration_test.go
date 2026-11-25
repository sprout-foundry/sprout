package core

import (
	"context"
	"strings"
	"testing"
)

// TestIntentNarrationGuard_RetriesOnDroppedToolCall mirrors the real-world
// stall: after tool results, the model narrates "let me commit…" but the
// tool call is dropped (corrupted stream). The guard nudges; the next
// response carries the tool call and the turn completes normally.
func TestIntentNarrationGuard_RetriesOnDroppedToolCall(t *testing.T) {
	provider := newFRProvider(
		// 1. Model makes a tool call
		frToolCallOnlyResponse("call_1", "git_status", "{}"),
		// 2. Dropped-tool-call stall: narration with mid-text intent and
		//    verbatim repetition, no tool calls, finish "stop"
		frTextResponse("No upstream changes. Now let me commit using the commit tool. "+
			"Let me commit the fix. Let me commit the fix.", "stop"),
		// 3. After the nudge, the model emits the tool call it described
		frToolCallOnlyResponse("call_2", "git_commit", "{}"),
		// 4. Final answer
		frTextResponse("Committed as abc123.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{
			{Role: "tool", Content: "nothing to push", ToolCallID: "call_1"},
			{Role: "tool", Content: "[main abc123] fix", ToolCallID: "call_2"},
		},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "commit and push")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "abc123") {
		t.Errorf("expected commit result, got %q", result)
	}
	if provider.idx != 4 {
		t.Errorf("expected 4 provider calls (tool + stall + nudged tool call + final), got %d", provider.idx)
	}
}

// TestIntentNarrationGuard_DonerestoresNarration verifies the DONE exit: a
// reply of exactly "DONE" to the nudge means the task was complete, and the
// original narration — not the sentinel — becomes the final message.
func TestIntentNarrationGuard_DoneRestoresNarration(t *testing.T) {
	narration := "The diff is staged and verified. Let me commit it now."
	provider := newFRProvider(
		frToolCallOnlyResponse("call_1", "git_status", "{}"),
		frTextResponse(narration, "stop"),
		frTextResponse("DONE", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "staged", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "commit if ready")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != narration {
		t.Errorf("expected restored narration, got %q", result)
	}
	// The sentinel must not leak into the transcript.
	for _, m := range agent.State().Messages() {
		if strings.TrimSpace(m.Content) == "DONE" {
			t.Errorf("DONE sentinel leaked into state: %+v", m)
		}
	}
	if provider.idx != 3 {
		t.Errorf("expected 3 provider calls, got %d", provider.idx)
	}
}

// TestIntentNarrationGuard_AcceptsAfterTwoRejections verifies the loop bound:
// two rejections then the response is accepted — no infinite nudge loop.
func TestIntentNarrationGuard_AcceptsAfterTwoRejections(t *testing.T) {
	stall := "Working tree is clean. Let me commit the fix. Let me commit the fix."
	provider := newFRProvider(
		frToolCallOnlyResponse("call_1", "git_status", "{}"),
		frTextResponse(stall, "stop"), // rejection 1
		frTextResponse(stall, "stop"), // rejection 2 → limit reached, accepted
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "clean", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "commit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != stall {
		t.Errorf("expected stalled narration accepted after limit, got %q", result)
	}
	if provider.idx != 3 {
		t.Errorf("expected 3 provider calls, got %d", provider.idx)
	}
}

// TestIntentNarrationGuard_FiresAfterBareContinue is the [307]-class bug:
// the user typed "continue" after the stall, and that bare prompt used to
// break tool-result proximity, disabling every post-tool guard for the
// retry. The guard must fire anyway.
func TestIntentNarrationGuard_FiresAfterBareContinue(t *testing.T) {
	provider := newFRProvider(
		// 1. Tool call
		frToolCallOnlyResponse("call_1", "git_fetch", "{}"),
		// 2. First stall (nudged automatically)
		frTextResponse("Up to date. Let me commit the staged fix now.", "stop"),
		// 3. Second stall (rejection limit reached on the intent guard,
		//    accepted → loop exits; the consumer sends "continue")
		frTextResponse("Up to date. Let me commit the staged fix now.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	if _, err := agent.Run(context.Background(), "commit and push"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate: append the user's bare continue to the existing state, then
	// run another query through a fresh agent seeded with that state.
	agent.State().AddMessage(Message{Role: "user", Content: "continue"})

	agent2, _ := NewAgent(Options{
		Provider: newFRProvider(
			frTextResponse("Still up to date. I'll commit the staged fix now.", "stop"),
			frToolCallOnlyResponse("call_2", "git_commit", "{}"),
			frTextResponse("Committed as def456.", "stop"),
		),
		Executor:        executor,
		InitialMessages: agent.State().Messages(),
	})

	result, err := agent2.Run(context.Background(), "continue")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "def456") {
		t.Errorf("expected recovery after nudge past bare continue, got %q", result)
	}
}
