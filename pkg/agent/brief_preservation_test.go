package agent

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestSeedCompactionPreservesOpeningBrief pins the task brief — the first
// user message of a run — through the full compaction cascade (substitution,
// pruner, LLM summarizer, continuation compaction). It is the only durable
// record of what the session was asked to do, so it must survive verbatim,
// never summarized, demoted or dropped, even far beyond any excerpt bound.
func TestSeedCompactionPreservesOpeningBrief(t *testing.T) {
	const brief = "Refactor the authentication module to use JWT tokens. " +
		"Scope: pkg/auth and cmd/server. Tests: make test-auth. Report the diff."
	// A long brief, far beyond any compaction excerpt bound.
	longBrief := brief + strings.Repeat(" Detail: preserve the exact original brief verbatim.", 40)

	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	client := NewScriptedClient(NewScriptedTextResponse("done"))
	a, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	// Force a tiny compaction trigger: a modest history crosses it without a
	// 90K-token fixture.
	a.contextProfile = configuration.ContextProfile{
		CompactionTriggerFraction: 0.05,
	}
	a.initSubManagers()

	// History over the forced trigger: the brief plus 50 filler turns.
	filler := strings.Repeat("x", 2000)
	history := make([]api.Message, 0, 103)
	history = append(history, api.Message{Role: "user", Content: longBrief})
	for i := 0; i < 50; i++ {
		history = append(history,
			api.Message{Role: "assistant", Content: filler},
			api.Message{Role: "user", Content: filler},
		)
	}
	a.SetMessages(history)

	before := len(a.GetMessages())
	if _, err := a.processQueryWithSeed(QuerySourceUnknown, "go"); err != nil {
		t.Fatalf("processQueryWithSeed: %v", err)
	}

	after := a.GetMessages()
	if len(after) >= before+2 {
		t.Fatalf("expected compaction to shrink state: before=%d after=%d", before, len(after))
	}

	// The brief must survive at the anchored position: system (if present) at
	// slot 0, then the exact brief.
	idx := 0
	if after[0].Role == "system" {
		idx = 1
	}
	if idx >= len(after) || after[idx].Role != "user" {
		t.Fatalf("expected anchored brief as user message (slot %d), got %+v", idx, after)
	}
	if after[idx].Content != longBrief {
		t.Errorf("brief lost after compaction: got %d chars, want exact %d-char brief",
			len(after[idx].Content), len(longBrief))
	}
}
