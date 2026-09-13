package agent

import (
	"testing"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

// mockReplayingFlagClient implements ReasoningHistoryReplayer with a settable flag.
type mockReplayingFlagClient struct {
	MockClient
	replays bool
}

func (m *mockReplayingFlagClient) ReplaysReasoningHistory() bool { return m.replays }

// TestEstimateTokensFallbackExcludesReasoningForNonReplayingProviders is the
// regression test for the context-meter oscillation: when the token anchor
// misses (first call / prefix rewritten), the heuristic fallback must NOT
// count ReasoningContent for providers that never replay reasoning on the
// wire. Counting it inflated the estimate by the full reasoning mass
// (~80K tokens observed), which flipped the displayed context usage between
// two values depending on whether the anchor hit.
func TestEstimateTokensFallbackExcludesReasoningForNonReplayingProviders(t *testing.T) {
	messages := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ReasoningContent: "reasoning " + string(make([]byte, 4000))},
		{Role: "assistant", Content: "answer"},
	}
	req := &core.ChatRequest{Messages: messages}

	// Provider that reports replaying (or is unknown) → reasoning counted.
	replaying := &sproutProvider{client: &mockReplayingFlagClient{replays: true}}
	if got := replaying.EstimateTokens(req); got <= api.EstimateInputTokens(stripReasoning(messages), nil) {
		t.Errorf("replaying provider estimate = %d, expected reasoning mass included (> %d)",
			got, api.EstimateInputTokens(stripReasoning(messages), nil))
	}

	// Provider that does not replay → wire-view estimate, reasoning excluded.
	nonReplaying := &sproutProvider{client: &mockReplayingFlagClient{replays: false}}
	want := api.EstimateInputTokensWireView(messages, nil)
	if got := nonReplaying.EstimateTokens(req); got != want {
		t.Errorf("non-replaying provider estimate = %d, want wire-view estimate %d", got, want)
	}
}

// TestEstimateMessagesTokensWireViewExcludesReasoning verifies the estimator
// itself: identical to EstimateMessagesTokens except ReasoningContent is
// skipped.
func TestEstimateMessagesTokensWireViewExcludesReasoning(t *testing.T) {
	with := []core.Message{
		{Role: "assistant", Content: "abc", ReasoningContent: "0123456789"},
	}
	without := []core.Message{
		{Role: "assistant", Content: "abc"},
	}
	if got := api.EstimateMessagesTokensWireView(with); got != api.EstimateMessagesTokensWireView(without) {
		t.Errorf("wire-view estimate must ignore ReasoningContent: with=%d without=%d",
			got, api.EstimateMessagesTokensWireView(without))
	}
	// Full view still counts it.
	if api.EstimateMessagesTokens(with) == api.EstimateMessagesTokens(without) {
		t.Error("full-view estimate should differ when ReasoningContent is present")
	}
}

// TestTokenAnchorDeltaUsesWireView verifies the anchored delta estimator
// selection: for non-replaying providers the appended tail's reasoning is
// excluded from the heuristic portion.
func TestTokenAnchorDeltaUsesWireView(t *testing.T) {
	var anchor tokenAnchor
	prefix := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
	}
	anchor.update("m", prefix, 1, 5000)

	tail := append(append([]core.Message{}, prefix...),
		core.Message{Role: "assistant", Content: "", ReasoningContent: "r " + string(make([]byte, 8000))},
	)

	_, heuristicDefault, ok := anchor.estimate("m", tail, 1, nil)
	if !ok {
		t.Fatal("expected anchor hit")
	}
	_, heuristicWire, ok := anchor.estimate("m", tail, 1, api.EstimateMessagesTokensWireView)
	if !ok {
		t.Fatal("expected anchor hit")
	}
	if heuristicWire >= heuristicDefault {
		t.Errorf("wire-view heuristic delta (%d) must be < default (%d) when tail carries reasoning",
			heuristicWire, heuristicDefault)
	}
}

// TestGenericProviderReplaysReasoningHistory pins the provider-config-driven
// answer: empty reasoning_content_field and no PreserveReasoningDetails →
// false; either set → true.
func TestGenericProviderReplaysReasoningHistory(t *testing.T) {
	build := func(field string, preserveDetails bool) providers.ReasoningHistoryReplayer {
		p, err := providers.NewGenericProvider(&providers.ProviderConfig{
			Name:     "test-prov",
			Endpoint: "http://localhost:1",
			Auth:     providers.AuthConfig{Type: "bearer", EnvVar: "TEST_KEY"},
			Models:   providers.ModelConfig{DefaultContextLimit: 32768},
			Conversion: providers.MessageConversion{
				ReasoningContentField:    field,
				PreserveReasoningDetails: preserveDetails,
			},
		})
		if err != nil {
			t.Fatalf("NewGenericProvider: %v", err)
		}
		return p
	}
	if build("", false).ReplaysReasoningHistory() {
		t.Error("empty reasoning field + no details replay must report false")
	}
	if !build("reasoning_content", false).ReplaysReasoningHistory() {
		t.Error("set reasoning field must report true")
	}
	if !build("", true).ReplaysReasoningHistory() {
		t.Error("PreserveReasoningDetails must report true")
	}
}

func stripReasoning(msgs []core.Message) []core.Message {
	out := make([]core.Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		out[i].ReasoningContent = ""
	}
	return out
}
