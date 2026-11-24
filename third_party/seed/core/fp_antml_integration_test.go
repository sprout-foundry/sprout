package core

import (
	"context"
	"strings"
	"testing"

	"github.com/sprout-foundry/seed/events"
)

// TestFallback_AntmlParameterBlocks drives the full Agent.Run loop with the
// namespaced-parameter tool-call shape (tool name on a <antml:parameter
// name="TOOL"> attribute, no <function=/<tool= opener). Before the antml
// extractor existed this markup was neither detected nor parsed, so it leaked
// to the user verbatim; here it must recover into a structured call and emit a
// tool_start event.
func TestFallback_AntmlParameterBlocks(t *testing.T) {
	open := antmlTag("parameter")
	close := antmlClose("parameter")
	content := "Let me edit that file.\n\n" +
		open + ` name="search">` + "\n" +
		open + ` name="query">hello` + close + "\n" +
		close + "\n\nDone."

	p := &testMP{
		responses: []*ChatResponse{
			{Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: content}}}, Usage: ChatUsage{TotalTokens: 15}},
			{Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "Done."}}}, Usage: ChatUsage{TotalTokens: 10}},
		},
	}
	e := &mockExecutor{
		tools:   []Tool{{Function: ToolFunction{Name: "search", Description: "Search", Parameters: ToolParameters{Type: "object"}}}},
		results: []Message{{Role: "tool", Content: "Results"}},
	}
	bus := events.NewEventBus()
	ch := bus.Subscribe("t")
	a, _ := NewAgent(Options{Provider: p, Executor: e, EventPublisher: bus})
	if _, err := a.Run(context.Background(), "search hello"); err != nil {
		t.Fatal(err)
	}
	evts := drainEvents(ch)
	if !findEventType(evts, events.EventTypeToolStart) {
		t.Errorf("expected tool_start event from antml-parsed tool call; events=%v", eventTypeNames(evts))
	}
}

// TestFallback_AntmlMarkupNotLeakedToUser is the regression guard for the
// reported symptom: the raw namespaced markup must never survive into the
// user-visible assistant content once it has been recovered as a tool call.
func TestFallback_AntmlMarkupNotLeakedToUser(t *testing.T) {
	open := antmlTag("parameter")
	close := antmlClose("parameter")
	content := open + ` name="search">` + "\n" +
		open + ` name="query">hello` + close + "\n" +
		close

	fp := NewFallbackParser(FallbackParserOptions{
		KnownToolNames: func(name string) bool { return name == "search" },
	})
	res := fp.Parse(content)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 recovered call, got %d", len(res.ToolCalls))
	}
	if strings.Contains(res.CleanedContent, antmlTag("parameter")) {
		t.Errorf("namespaced markup leaked into cleaned content: %q", res.CleanedContent)
	}
}
