package agent

import (
	"testing"

	core "github.com/sprout-foundry/seed/core"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestFingerprintMessages(t *testing.T) {
	a := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
	}
	b := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
	}
	c := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello!"}, // different content
	}

	if fingerprintMessages(a) != fingerprintMessages(b) {
		t.Error("identical message slices produced different fingerprints")
	}
	if fingerprintMessages(a) == fingerprintMessages(c) {
		t.Error("differing message content produced the same fingerprint")
	}

	// Field-boundary collision: "ab"+"" vs "a"+"b" must not hash the same.
	split := []core.Message{{Role: "user", Content: "ab", ReasoningContent: ""}}
	joined := []core.Message{{Role: "user", Content: "a", ReasoningContent: "b"}}
	if fingerprintMessages(split) == fingerprintMessages(joined) {
		t.Error("field-boundary shift produced a colliding fingerprint")
	}

	// Tool call payloads must participate in the fingerprint.
	withToolCall := []core.Message{
		{Role: "assistant", ToolCalls: []core.ToolCall{{ID: "1", Function: core.ToolCallFunction{Name: "read_file", Arguments: `{"path":"a"}`}}}},
	}
	withDifferentArgs := []core.Message{
		{Role: "assistant", ToolCalls: []core.ToolCall{{ID: "1", Function: core.ToolCallFunction{Name: "read_file", Arguments: `{"path":"b"}`}}}},
	}
	if fingerprintMessages(withToolCall) == fingerprintMessages(withDifferentArgs) {
		t.Error("differing tool-call arguments produced the same fingerprint")
	}
}

func TestTokenAnchor_NoAnchorYet(t *testing.T) {
	var anchor tokenAnchor
	if _, _, ok := anchor.estimate([]core.Message{{Role: "user", Content: "hi"}}, 0); ok {
		t.Error("expected no usable anchor before any update()")
	}
}

func TestTokenAnchor_ExactPrefixReturnsActualTokens(t *testing.T) {
	var anchor tokenAnchor
	messages := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
	}
	anchor.update(messages, 2, 5000)

	got, heuristic, ok := anchor.estimate(messages, 2)
	if !ok {
		t.Fatal("expected anchor to apply for the exact same messages/tools")
	}
	if got != 5000 {
		t.Errorf("estimate() total = %d, want 5000 (no new messages appended)", got)
	}
	if heuristic != 0 {
		t.Errorf("estimate() heuristic = %d, want 0 (no appended messages)", heuristic)
	}
}

func TestTokenAnchor_AppendedMessagesAddHeuristicDelta(t *testing.T) {
	var anchor tokenAnchor
	base := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
	}
	anchor.update(base, 2, 5000)

	newMsg := core.Message{Role: "assistant", Content: "a fairly long assistant reply with several words in it"}
	extended := append(append([]core.Message{}, base...), newMsg)

	got, heuristic, ok := anchor.estimate(extended, 2)
	if !ok {
		t.Fatal("expected anchor to apply when messages are purely appended to")
	}
	wantTotal := 5000 + api.EstimateMessagesTokens([]core.Message{newMsg})
	wantHeuristic := api.EstimateMessagesTokens([]core.Message{newMsg})
	if got != wantTotal {
		t.Errorf("estimate() total = %d, want %d (actual anchor + heuristic delta only)", got, wantTotal)
	}
	if heuristic != wantHeuristic {
		t.Errorf("estimate() heuristic = %d, want %d (delta only)", heuristic, wantHeuristic)
	}
}

func TestTokenAnchor_InvalidatedByToolCountChange(t *testing.T) {
	var anchor tokenAnchor
	messages := []core.Message{{Role: "user", Content: "hi"}}
	anchor.update(messages, 2, 5000)

	if _, _, ok := anchor.estimate(messages, 3); ok {
		t.Error("expected anchor to be invalidated when tool count changes")
	}
}

func TestTokenAnchor_InvalidatedByPrefixEdit(t *testing.T) {
	var anchor tokenAnchor
	messages := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
	}
	anchor.update(messages, 0, 5000)

	// Simulate checkpoint substitution: the first message's content changes
	// even though the slice length is the same.
	edited := []core.Message{
		{Role: "system", Content: "sys (summarized)"},
		{Role: "user", Content: "hello"},
	}
	if _, _, ok := anchor.estimate(edited, 0); ok {
		t.Error("expected anchor to be invalidated when prefix content changed (e.g. checkpoint substitution)")
	}
}

func TestTokenAnchor_InvalidatedByShrink(t *testing.T) {
	var anchor tokenAnchor
	messages := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	anchor.update(messages, 0, 5000)

	// Simulate rollup/drop compaction: fewer messages than the anchor saw.
	shrunk := messages[:1]
	if _, _, ok := anchor.estimate(shrunk, 0); ok {
		t.Error("expected anchor to be invalidated when the message count shrinks below the anchor")
	}
}

func TestTokenAnchor_IgnoresNonPositiveActualTokens(t *testing.T) {
	var anchor tokenAnchor
	messages := []core.Message{{Role: "user", Content: "hi"}}
	anchor.update(messages, 0, 0) // provider didn't report usage

	if _, _, ok := anchor.estimate(messages, 0); ok {
		t.Error("expected update() with actualTokens<=0 to leave the anchor unset")
	}
}

// TestSproutProviderEstimateTokens_FallsBackWithoutAnchor is a regression
// guard: with no prior real response, EstimateTokens must behave exactly as
// before this change — a full heuristic estimate via api.EstimateInputTokens.
func TestSproutProviderEstimateTokens_FallsBackWithoutAnchor(t *testing.T) {
	provider, err := NewSproutProvider(nil, &MockClient{model: "test-model"})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	sp := provider.(*sproutProvider)

	req := &core.ChatRequest{
		Messages: []core.Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "hello world"},
		},
	}

	want := api.EstimateInputTokens(req.Messages, req.Tools)
	got := sp.EstimateTokens(req)
	if got != want {
		t.Errorf("EstimateTokens() = %d, want %d (full heuristic fallback)", got, want)
	}
}

// TestSproutProviderEstimateTokens_UsesAnchorAfterUpdate verifies the
// end-to-end wiring: once the anchor has a real measurement for a message
// prefix, EstimateTokens for an extended request anchors to it instead of
// re-estimating everything from scratch.
func TestSproutProviderEstimateTokens_UsesAnchorAfterUpdate(t *testing.T) {
	provider, err := NewSproutProvider(nil, &MockClient{model: "test-model"})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	sp := provider.(*sproutProvider)

	base := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello world"},
	}
	sp.tokenAnchor.update(base, 0, 42000) // simulate a real Usage.PromptTokens from a prior response

	newMsg := core.Message{Role: "assistant", Content: "a reply"}
	req := &core.ChatRequest{Messages: append(append([]core.Message{}, base...), newMsg)}

	want := 42000 + api.EstimateMessagesTokens([]core.Message{newMsg})
	got := sp.EstimateTokens(req)
	if got != want {
		t.Errorf("EstimateTokens() = %d, want %d (anchored estimate)", got, want)
	}

	// Sanity: the anchored estimate must differ from a blind full re-estimate
	// for this to be a meaningful improvement, not a no-op.
	blind := api.EstimateInputTokens(req.Messages, req.Tools)
	if got == blind {
		t.Skip("anchored and blind estimates coincidentally match for this fixture; not a failure, just uninformative")
	}
}
