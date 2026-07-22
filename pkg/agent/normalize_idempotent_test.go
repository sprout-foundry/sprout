package agent

import (
	"reflect"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// TestNormalize_NoOpOnSameModel verifies that normalizeConversationForCurrentModelSyntax
// is a complete no-op when the user re-selects the same provider/model pair.
//
// Re-confirming the current model is a common UI flow (open picker, pick the
// same option). Without this guard the function still calls
// normalizeConversationForStrictToolSyntax and buildSwitchContextRefreshMessage,
// both of which produce real output that pollutes the next prompt with stale
// context. The spec guard short-circuits before any of that work runs.
func TestNormalize_NoOpOnSameModel(t *testing.T) {
	client := &strictSyntaxClient{
		TestClient: &factory.TestClient{},
		provider:   "openai",
		model:      "gpt-4o",
	}
	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.setClient(client, api.TestClientType)

	// Pre-populate messages with content that would be touched by the strict-syntax
	// normalization path. If the function accidentally runs, the tool message will
	// be stripped and an assistant summary block appended — both observable.
	original := []api.Message{
		{Role: "system", Content: "system"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []api.ToolCall{
				{ID: "call-1"},
			},
		},
		{Role: "tool", Content: "Tool call result for read_file: a.go\nx", ToolCallID: "call-1"},
		{Role: "assistant", Content: "final answer"},
	}
	a.state.SetMessages(original)

	// Same provider/model as the agent — should be a no-op.
	a.normalizeConversationForCurrentModelSyntax("openai", "gpt-4o")

	// Messages must be untouched.
	got := a.state.GetMessages()
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("messages mutated on no-op call:\nbefore=%+v\nafter =%+v", original, got)
	}

	// No pending refresh, no pending notice, no pending system supplement
	// should have been set on the state manager.
	if refresh := a.consumePendingSwitchContextRefresh(); refresh != "" {
		t.Errorf("expected no pending switch context refresh, got %q", refresh)
	}
	if notice := a.ConsumePendingStrictSwitchNotice(); notice != "" {
		t.Errorf("expected no pending strict switch notice, got %q", notice)
	}
	if supplement := a.consumePendingSystemSupplement(); supplement != "" {
		t.Errorf("expected no pending system supplement, got %q", supplement)
	}
}

// TestNormalize_StillRunsOnDifferentModel verifies that the new same-pair guard
// does NOT regress the existing strict-syntax normalization path. When the
// (fromProvider, fromModel) differs from the current agent model AND the new
// model is a strict-syntax model, the function must reach
// normalizeConversationForStrictToolSyntax and apply the expected mutations.
func TestNormalize_StillRunsOnDifferentModel(t *testing.T) {
	client := &strictSyntaxClient{
		TestClient: &factory.TestClient{},
		provider:   "minimax",
		model:      "minimax-chat",
	}
	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.setClient(client, api.TestClientType)

	// Seed messages with a tool result and an assistant tool-call block —
	// these are the inputs the strict-syntax normalizer is designed to compress.
	seed := []api.Message{
		{Role: "system", Content: "system"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []api.ToolCall{
				{ID: "call-1"},
			},
		},
		{Role: "tool", Content: "Tool call result for read_file: a.go\nx", ToolCallID: "call-1"},
		{Role: "assistant", Content: "final answer"},
	}
	a.state.SetMessages(seed)

	// from != current — normalization should run.
	a.normalizeConversationForCurrentModelSyntax("openai", "gpt-4o")

	// The strict-syntax path strips tool role messages and assistant tool_calls,
	// then appends a compressed summary assistant message. We assert the
	// observable side-effects:
	after := a.state.GetMessages()
	if len(after) == 0 {
		t.Fatal("expected non-empty messages after normalization")
	}
	for _, msg := range after {
		if msg.Role == "tool" {
			t.Errorf("expected tool messages to be removed by strict syntax normalization, found role=%s", msg.Role)
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			t.Errorf("expected assistant ToolCalls to be stripped, found %d", len(msg.ToolCalls))
		}
	}

	// The refresh message is always set when the strict-syntax path runs.
	if refresh := a.consumePendingSwitchContextRefresh(); refresh == "" {
		t.Error("expected pending switch context refresh to be set after a real model switch")
	}
}
