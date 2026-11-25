package core

import (
	"context"
	"strings"
	"testing"

	"github.com/sprout-foundry/seed/events"
)

// continuationProvider answers the summary instruction with summary and every
// other request with a final reply, recording each request.
type continuationProvider struct {
	summary   Message
	window    int
	requests  []*ChatRequest
	reasoning []string
}

func (p *continuationProvider) Chat(_ context.Context, req *ChatRequest) (*ChatResponse, error) {
	p.requests = append(p.requests, req)
	p.reasoning = append(p.reasoning, req.Reasoning)
	last := req.Messages[len(req.Messages)-1]
	if last.Role == "user" && strings.HasPrefix(last.Content, "The conversation so far is about to be compacted") {
		return &ChatResponse{
			Choices: []ChatChoice{{Message: p.summary, FinishReason: "stop"}},
			Usage:   ChatUsage{PromptTokens: 5000, CachedTokens: 4900, CompletionTokens: 120},
		}, nil
	}
	return &ChatResponse{
		Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "done"}, FinishReason: "stop"}},
		Usage:   ChatUsage{PromptTokens: 100, CompletionTokens: 5},
	}, nil
}

func (p *continuationProvider) ChatStream(ctx context.Context, req *ChatRequest, h StreamHandler) error {
	resp, _ := p.Chat(ctx, req)
	h.OnContent(resp.Choices[0].Message.Content)
	h.OnDone(resp)
	return nil
}

func (p *continuationProvider) Info() ProviderInfo {
	if p.window > 0 {
		return ProviderInfo{Model: "test", ContextSize: p.window}
	}
	return ProviderInfo{Model: "test", ContextSize: 8000}
}

func (p *continuationProvider) EstimateTokens(req *ChatRequest) int {
	return roughTokens(req.Messages)
}

func runContinuationAgent(t *testing.T, p *continuationProvider, cc ContinuationCompaction) []events.UIEvent {
	t.Helper()
	bus := events.NewEventBus()
	a, err := NewAgent(Options{
		Provider:                  p,
		Executor:                  NoopExecutor,
		EventPublisher:            bus,
		Pruner:                    NewConversationPruner(PrunerOptions{Strategy: PruneStrategyAdaptive}),
		InitialMessages:           settleMessages(38), // ~5.8K tokens, over the 5600 trigger
		CompactionTriggerFraction: 0.70,
		ContinuationCompaction:    &cc,
	})
	if err != nil {
		t.Fatal(err)
	}
	sub := bus.Subscribe("continuation")
	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var out []events.UIEvent
	for {
		select {
		case ev := <-sub:
			if ev.Type == events.EventTypeCompaction {
				out = append(out, ev)
			}
		default:
			return out
		}
	}
}

// The summary call repeats the request the loop was about to send, with the
// instruction as its only new message, so its whole prefix is the cached one;
// the next request carries the summary and settles near the target.
func TestContinuationCompactionSummarizesOverTheLiveRequest(t *testing.T) {
	p := &continuationProvider{summary: Message{Role: "assistant", Content: "Task: build it.\nDone: most of it.\nNext: finish."}}
	evs := runContinuationAgent(t, p, ContinuationCompaction{TargetTokens: 2000, SummaryTokens: 400, Reasoning: "low"})

	if len(p.requests) != 2 {
		t.Fatalf("got %d requests, want the summary call and one turn", len(p.requests))
	}
	summaryReq, next := p.requests[0], p.requests[1]
	// The request the loop sends for the same turn when nothing is compacted.
	uncompacted := &continuationProvider{window: 1_000_000}
	runContinuationAgent(t, uncompacted, ContinuationCompaction{})
	live := uncompacted.requests[0].Messages
	if n := len(summaryReq.Messages); n != len(live)+1 {
		t.Fatalf("summary request has %d messages, want the live %d and the instruction", n, len(live))
	}
	for i, m := range live {
		got := summaryReq.Messages[i]
		if got.Role != m.Role || got.Content != m.Content {
			t.Fatalf("summary request message %d differs from the live request: %q vs %q", i, got.Content, m.Content)
		}
	}
	if p.reasoning[0] != "low" {
		t.Errorf("summary call reasoning = %q, want the configured low", p.reasoning[0])
	}

	found := false
	for _, m := range next.Messages {
		if strings.Contains(m.Content, "Compacted earlier conversation state") && strings.Contains(m.Content, "Next: finish.") {
			found = true
		}
	}
	if !found {
		t.Error("the next request does not carry the summary")
	}
	if n := roughTokens(next.Messages); n > 2400 {
		t.Errorf("the next request is %d tokens, want near the 2000 target", n)
	}

	if len(evs) != 1 {
		t.Fatalf("got %d compaction events, want 1", len(evs))
	}
	data, _ := evs[0].Data.(map[string]interface{})
	if data["strategy"] != "continuation_summary" {
		t.Errorf("strategy = %v", data["strategy"])
	}
	if data["summary_cached_tokens"] != 4900 {
		t.Errorf("summary_cached_tokens = %v, want the summary call's 4900", data["summary_cached_tokens"])
	}
}

// A summary that calls a tool is refused and the loop falls back to its
// cascade, which still settles under the trigger.
func TestContinuationCompactionFallsBackWhenTheSummaryCallsATool(t *testing.T) {
	p := &continuationProvider{summary: Message{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "read_file", Arguments: "{}"}}}}}
	evs := runContinuationAgent(t, p, ContinuationCompaction{TargetTokens: 2000, SummaryTokens: 400})

	if len(evs) != 1 {
		t.Fatalf("got %d compaction events, want 1", len(evs))
	}
	data, _ := evs[0].Data.(map[string]interface{})
	if s, _ := data["strategy"].(string); !strings.HasPrefix(s, "pruner_") {
		t.Errorf("strategy = %v, want the pruner fallback", data["strategy"])
	}
	if n := roughTokens(p.requests[len(p.requests)-1].Messages); n > 5600 {
		t.Errorf("the fallback left %d tokens, over the 5600 trigger", n)
	}
}

// The verbatim tail never starts between a tool call and its results.
func TestContinuationTailKeepsToolCallsWithTheirResults(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: strings.Repeat("a", 400)},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "1", Type: "function", Function: ToolCallFunction{Name: "read_file"}}}},
		{Role: "tool", ToolCallID: "1", Content: strings.Repeat("r", 400)},
		{Role: "tool", ToolCallID: "1", Content: strings.Repeat("r", 400)},
		{Role: "assistant", Content: "ok"},
	}
	start := continuationTailStart(msgs, 150, roughTokens, compactionAnchorEnd(msgs))
	if msgs[start].Role == "tool" {
		t.Fatalf("tail starts at a tool result (index %d)", start)
	}
	if start > 0 && len(msgs[start-1].ToolCalls) > 0 {
		t.Fatalf("tail starts right after a tool call (index %d)", start)
	}
}
