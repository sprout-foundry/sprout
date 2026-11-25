package core

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sprout-foundry/seed/events"
)

// These tests pin the run's opening brief — the first user message of a
// session — through every compaction path. The brief is the only durable
// record of what the session was asked to do; it must survive verbatim,
// never summarized, demoted, truncated or dropped, regardless of length.

// briefFixture builds a long conversation: system, a long brief as the first
// user message, and 30 filler turns plus a short tail. Returns the fixture
// and the brief.
func briefFixture(briefLen int) ([]Message, string) {
	brief := strings.Repeat("b", briefLen)
	msgs := []Message{
		{Role: "system", Content: "You are a helpful coding assistant."},
		{Role: "user", Content: brief},
		{Role: "assistant", Content: "I'll start by exploring the codebase."},
	}
	for i := 0; i < 30; i++ {
		msgs = append(msgs,
			Message{Role: "user", Content: strings.Repeat("u", 40)},
			Message{Role: "assistant", Content: strings.Repeat("a", 40)},
		)
	}
	msgs = append(msgs,
		Message{Role: "user", Content: "the live task"},
		Message{Role: "assistant", Content: "final answer"},
	)
	return msgs, brief
}

// expectBriefAtOne asserts the brief survived at the anchored position:
// system at slot 0, the exact brief at slot 1.
func expectBriefAtOne(t *testing.T, msgs []Message, brief string, path string) {
	t.Helper()
	if len(msgs) == 0 || msgs[0].Role != "system" {
		t.Fatalf("%s: expected system message at slot 0, got %+v", path, msgs)
	}
	if len(msgs) < 2 || msgs[1].Role != "user" {
		t.Fatalf("%s: expected anchored user brief at slot 1, got %+v", path, msgs[1:])
	}
	if msgs[1].Content != brief {
		t.Errorf("%s: brief lost — slot 1 content is %d chars, want exact %d-char brief",
			path, len(msgs[1].Content), len(brief))
	}
}

// The substitution path (BuildCheckpointCompactedMessages +
// IterativelySubstituteCheckpoints) must keep the opening user message
// verbatim even when a long brief is replaced by checkpointed summaries.
func TestBrief_Substitution_LongBriefPreserved(t *testing.T) {
	msgs, brief := briefFixture(4000)
	checkpoints := []TurnCheckpoint{
		{StartIndex: 3, EndIndex: 4, Summary: "- explored the codebase", ActionableSummary: "- read the plan"},
		{StartIndex: 5, EndIndex: 6, Summary: "- read the plan", ActionableSummary: "- did work"},
	}

	out, _ := BuildCheckpointCompactedMessages(msgs, checkpoints)
	expectBriefAtOne(t, out, brief, "BuildCheckpointCompactedMessages")

	// The iterative path must produce the same verdict with a target that
	// forces substitution.
	before := roughTokens(msgs)
	substituted, _, _ := IterativelySubstituteCheckpoints(msgs, checkpoints, 10, roughTokens)
	if before <= 10 {
		t.Fatalf("fixture must start over the target: got %d tokens", before)
	}
	expectBriefAtOne(t, substituted, brief, "IterativelySubstituteCheckpoints")
}

// The LLM-summarizer path (CompactWithLLMSummary) must keep the brief
// verbatim even when it is far beyond any excerpt bound.
func TestBrief_LLMSummarizer_LongBriefPreserved(t *testing.T) {
	msgs, brief := briefFixture(6000)
	res := CompactWithLLMSummary(context.Background(), msgs, stubSummarizer("- did some work"))
	if res.Strategy == "none" {
		t.Fatalf("expected compaction to fire, got Strategy=none")
	}
	expectBriefAtOne(t, res.Messages, brief, "CompactWithLLMSummary")
}

// The pruner must keep the brief verbatim under every strategy and under the
// agentic headroom pass (the task-run path that drops the oldest turns).
func TestBrief_Pruner_AllStrategies(t *testing.T) {
	const window = 3000
	for _, strategy := range []PruningStrategy{
		PruneStrategySlidingWindow,
		PruneStrategyImportance,
		PruneStrategyHybrid,
		PruneStrategyAdaptive,
	} {
		t.Run(string(strategy), func(t *testing.T) {
			msgs, brief := briefFixture(3000)
			p := NewConversationPruner(PrunerOptions{
				Strategy:             strategy,
				MinMessagesToKeep:    10,
				RecentMessagesToKeep: 24,
				SlidingWindowSize:    30,
			})
			current := roughTokens(msgs)
			pruned := p.Prune(context.Background(), msgs, current, window, PruneCallOptions{
				IsAgenticFlow: true, // force the headroom pass that drops oldest turns
			})
			if len(pruned) >= len(msgs) {
				t.Errorf("strategy %s: expected the pruner to drop messages on a long conversation, kept %d of %d",
					strategy, len(pruned), len(msgs))
			}
			expectBriefAtOne(t, pruned, brief, "Prune("+string(strategy)+")")
		})
	}
}

// The strict tool-call-aware pruner variant (used for MiniMax/DeepSeek
// providers) must also keep the brief verbatim — including a brief that
// precedes tool-calling turns.
func TestBrief_Pruner_ToolCallAware(t *testing.T) {
	const window = 3000
	brief := strings.Repeat("b", 3000)
	msgs := []Message{
		{Role: "system", Content: "You are a helpful coding assistant."},
		{Role: "user", Content: brief},
		{Role: "assistant", Content: "I'll read the file."},
	}
	for i := 0; i < 30; i++ {
		id := fmt.Sprintf("call_%d", i)
		msgs = append(msgs,
			Message{Role: "user", Content: "run tool " + id},
			Message{Role: "assistant", Content: "", ToolCalls: []ToolCall{
				{ID: id, Type: "function", Function: ToolCallFunction{Name: "read", Arguments: `{"path":"x"}`}},
			}},
			Message{Role: "tool", Content: "some result " + id, ToolCallID: id},
			Message{Role: "assistant", Content: "done " + id},
		)
	}

	p := NewConversationPruner(PrunerOptions{Strategy: PruneStrategyImportance})
	current := roughTokens(msgs)
	pruned := p.Prune(context.Background(), msgs, current, window, PruneCallOptions{
		Provider: "deepseek", // strict tool-pairing provider
	})
	expectBriefAtOne(t, pruned, brief, "Prune(tool-call-aware)")
	// The brief must be followed by a valid tool-pairing layout: every tool
	// result has its assistant parent in the output.
	for i, m := range pruned {
		if m.Role != "tool" {
			continue
		}
		found := false
		for j := 0; j < i; j++ {
			for _, tc := range pruned[j].ToolCalls {
				if tc.ID == m.ToolCallID {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("tool result at index %d lost its assistant parent", i)
		}
	}
}

// The continuation-compaction path must keep the brief verbatim through the
// whole agent loop: the request sent after continuation compaction starts
// with the exact brief.
func TestBrief_ContinuationCompaction_LongBriefPreserved(t *testing.T) {
	longBrief := strings.Repeat("B", 3000)
	msgs := []Message{
		{Role: "system", Content: "You are a helpful coding assistant."},
		{Role: "user", Content: longBrief},
		{Role: "assistant", Content: "I'll start by exploring the codebase."},
	}
	pad := strings.Repeat("xxxxxxxx", 60)
	for i := 0; i < 38; i++ {
		msgs = append(msgs,
			Message{Role: "user", Content: "Q" + pad},
			Message{Role: "assistant", Content: "A" + pad},
		)
	}

	p := &continuationProvider{summary: Message{Role: "assistant", Content: "Task: build it.\nDone: most of it.\nNext: finish."}}
	bus := events.NewEventBus()
	cc := ContinuationCompaction{TargetTokens: 2000, SummaryTokens: 400, Reasoning: "low"}
	a, err := NewAgent(Options{
		Provider:                  p,
		Executor:                  NoopExecutor,
		EventPublisher:            bus,
		Pruner:                    NewConversationPruner(PrunerOptions{Strategy: PruneStrategyAdaptive}),
		InitialMessages:           msgs,
		CompactionTriggerFraction: 0.70,
		ContinuationCompaction:    &cc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(p.requests) < 2 {
		t.Fatalf("expected the summary call and one turn, got %d requests", len(p.requests))
	}
	finalRequest := p.requests[len(p.requests)-1]
	if len(finalRequest.Messages) < 2 {
		t.Fatalf("expected at least 2 messages in the final request, got %d", len(finalRequest.Messages))
	}
	if finalRequest.Messages[0].Role != "system" {
		t.Fatalf("expected system message at slot 0, got %+v", finalRequest.Messages[0])
	}
	if finalRequest.Messages[1].Role != "user" || finalRequest.Messages[1].Content != longBrief {
		t.Errorf("continuation compaction lost the brief: slot 1 = role %q, content %d chars, want %d-char brief",
			finalRequest.Messages[1].Role, len(finalRequest.Messages[1].Content), len(longBrief))
	}
}
